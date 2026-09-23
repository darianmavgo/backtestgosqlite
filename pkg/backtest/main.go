package backtest

import (
	"flag"
	"fmt"
	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"log"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/analytics"
	"github.com/darianmavgo/backtestgosqlite/pkg/cliutils"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/runner"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	_ "modernc.org/sqlite"
)

// backtestStart is the -start flag value: the default backtest window start.
var backtestStart string

// Config holds the settings of a run.
type Config struct {
	Db                  string   // -db
	Table               string   // -table
	Strategy            string   // -strategy
	SharedAccount       bool     // -shared-account
	Primary             string   // -primary
	Secondary           string   // -secondary
	OutDir              string   // -out-dir
	List                bool     // -list
	Symbol              string   // -symbol
	Capital             float64  // -capital
	MaxPositions        int      // -max-positions
	Stoploss            float64  // -stoploss
	Target              float64  // -target
	Hold                int      // -hold
	Html                string   // -html
	AutoDownload        bool     // -auto-download
	DownloadYears       int      // -download-years
	Concurrency         int      // -concurrency
	Force               bool     // -force
	GridsearchDb        string   // -gridsearch-db
	IncludeUniverse     bool     // -include-universe
	IncludeDt           bool     // -include-dt
	DtTop               int      // -dt-top
	StackDepth          int      // -stack-depth
	PersistBest         bool     // -persist-best
	Start               string   // -start
	SignalsOnly         bool     // -signals-only
	Bars                int      // -bars
	Otm                 float64  // -otm
	Commission          float64  // -commission
	OptSlip             float64  // -opt-slip
	NoReinvestDividends bool     // -no-reinvest-dividends
	Mode                string   // subcommand (empty = default)
	Args                []string // positional arguments
}

// DefaultConfig returns the CLI defaults.
func DefaultConfig() Config {
	return Config{
		Db:                  cliutils.GetDefaultMarketDB(),
		Table:               "backtest_start",
		Strategy:            "",
		SharedAccount:       false,
		Primary:             "",
		Secondary:           "",
		OutDir:              appenv.Reports(),
		List:                false,
		Symbol:              "",
		Capital:             100000.0,
		MaxPositions:        0,
		Stoploss:            0.0,
		Target:              0.0,
		Hold:                0,
		Html:                appenv.ReportFile("backtest_report.html"),
		AutoDownload:        true,
		DownloadYears:       5,
		Concurrency:         runtime.NumCPU(),
		Force:               false,
		GridsearchDb:        appenv.ReportFile("gridsearch.db"),
		IncludeUniverse:     false,
		IncludeDt:           false,
		DtTop:               15,
		StackDepth:          3,
		PersistBest:         true,
		Start:               storage.DefaultStartDate,
		SignalsOnly:         false,
		Bars:                0,
		Otm:                 2.0,
		Commission:          0.65,
		OptSlip:             0.05,
		NoReinvestDividends: false,
	}
}

// Main is the CLI entry point.
func Main() {
	// Subcommand dispatch:
	//   backtest stale      -> assess which strategies' cached reports/*.db
	//                          results are stale (unregistered strategy,
	//                          newer market data, or an edited SQL pipeline
	//                          since the result was made) and exit — no
	//                          backtests run
	//   backtest optimized  -> run every selected strategy (default: all) with
	//                          the best config a prior `gridsearch` sweep
	//                          found for it, instead of its baseline defaults
	//   backtest covered-call -> hold -symbol (default VOO) and sell a monthly
	//                          call; needs `download -source polygon-options`
	//   backtest stack-eval -> rank existing strategies as idle-cash overlays
	//                          on one primary, one shared cash ledger
	conf := DefaultConfig()
	d := conf
	flag.StringVar(&conf.Db, "db", d.Db, "Path to source SQLite DB containing historical market bars")
	flag.StringVar(&conf.Table, "table", d.Table, "Table name containing historical bars")
	flag.StringVar(&conf.Strategy, "strategy", d.Strategy, "Strategy ID to run, comma-separated list, 'all', or 'strat1+strat2' for shared account")
	flag.BoolVar(&conf.SharedAccount, "shared-account", d.SharedAccount, "Run strategies in a single shared cash account with priority preemption")
	flag.StringVar(&conf.Primary, "primary", d.Primary, "Primary strategy ID for shared-account execution (has capital priority)")
	flag.StringVar(&conf.Secondary, "secondary", d.Secondary, "Secondary strategy ID(s) for shared-account execution (comma-separated)")
	flag.StringVar(&conf.OutDir, "out-dir", d.OutDir, "Directory to write strategy SQLite database results and reports")
	flag.BoolVar(&conf.List, "list", d.List, "List all registered Go and SQL strategies")
	flag.StringVar(&conf.Symbol, "symbol", d.Symbol, "Optional: Filter backtest to a specific symbol (e.g. DFEN, SOXL)")
	flag.Float64Var(&conf.Capital, "capital", d.Capital, "Starting portfolio capital for simulation")
	flag.IntVar(&conf.MaxPositions, "max-positions", d.MaxPositions, "Optional override: Maximum concurrent open positions allowed")
	flag.Float64Var(&conf.Stoploss, "stoploss", d.Stoploss, "Optional override: Stop-loss floor multiplier (e.g. 0.93 for -7%)")
	flag.Float64Var(&conf.Target, "target", d.Target, "Optional override: Take-profit multiplier (e.g. 1.18 for +18%)")
	flag.IntVar(&conf.Hold, "hold", d.Hold, "Optional override: Max holding days window")
	flag.StringVar(&conf.Html, "html", d.Html, "Path to export interactive HTML dashboard report")
	flag.BoolVar(&conf.AutoDownload, "auto-download", d.AutoDownload, "Automatically detect missing market data and run download")
	flag.IntVar(&conf.DownloadYears, "download-years", d.DownloadYears, "Number of years of history to fetch when downloading missing data")
	flag.IntVar(&conf.Concurrency, "concurrency", d.Concurrency, "Max concurrent strategies when running more than one (defaults to all CPU cores; bounds memory use for large -strategy all runs)")
	flag.BoolVar(&conf.Force, "force", d.Force, "(multi-strategy runs only) redo every strategy even if it already has a usable result in -out-dir")
	flag.StringVar(&conf.GridsearchDb, "gridsearch-db", d.GridsearchDb, "(optimized subcommand only) SQLite DB of gridsearch results to read best configs from")
	flag.BoolVar(&conf.IncludeUniverse, "include-universe", d.IncludeUniverse, "(stack-eval) also try full-universe overlays (bb-capitulation, rsi2, ...)")
	flag.BoolVar(&conf.IncludeDt, "include-dt", d.IncludeDt, "(stack-eval) also try auto-fit dt_* ETF decision trees")
	flag.IntVar(&conf.DtTop, "dt-top", d.DtTop, "(stack-eval) how many highest-scored dt_* trees to include with -include-dt")
	flag.IntVar(&conf.StackDepth, "stack-depth", d.StackDepth, "(stack-eval) greedy complementary overlays to combine after pairwise ranking")
	flag.BoolVar(&conf.PersistBest, "persist-best", d.PersistBest, "(stack-eval) write a shared_*.db for the greedy N-way stack")
	flag.StringVar(&conf.Start, "start", d.Start, "Earliest bar date (YYYY-MM-DD) to simulate; earlier bars are only used for SMA warmup. Empty = full history")
	flag.BoolVar(&conf.SignalsOnly, "signals-only", d.SignalsOnly, "Skip portfolio simulation; run the same GenerateSignals live window as cmd/livescan (tip bar → next session)")
	flag.IntVar(&conf.Bars, "bars", d.Bars, "(with -signals-only) recent bars per symbol; 0 = strategy MinHistoryBars")
	flag.Float64Var(&conf.Otm, "otm", d.Otm, "(covered-call) target call strike as % above spot at each monthly roll")
	flag.Float64Var(&conf.Commission, "commission", d.Commission, "(covered-call) $ per option contract sold (IBKR tiered ≈ $0.65)")
	flag.Float64Var(&conf.OptSlip, "opt-slip", d.OptSlip, "(covered-call) $ per share given up vs the last-trade option price when selling")
	flag.BoolVar(&conf.NoReinvestDividends, "no-reinvest-dividends", d.NoReinvestDividends, "Total-return strategies (e.g. schd-buy-hold): take dividends as idle cash instead of reinvesting them")
	conf.Mode = cliutils.PopSubcommand(map[string]string{"covered-call": "covered-call", "coveredcall": "covered-call", "stale": "stale", "optimized": "optimized", "stack-eval": "stack-eval", "stackeval": "stack-eval"})
	flag.Parse()
	conf.Args = flag.Args()
	if err := Run(conf); err != nil {
		log.Fatal(err)
	}
}

// Run executes the command with cfg. It returns errors instead of exiting.
func Run(conf Config) error {
	backtestStart = conf.Start
	reinvestDividends := !conf.NoReinvestDividends

	if conf.Mode == "covered-call" {
		ccStart := conf.Start
		if ccStart == storage.DefaultStartDate {
			ccStart = "" // option history is only ~2y; begin at the first rollable month
		}
		if err := runCoveredCallCommand(conf.Db, conf.Table, strings.ToUpper(conf.Symbol), conf.Capital, conf.Otm, ccStart, conf.Commission, conf.OptSlip); err != nil {
			return err
		}
		return nil
	}

	// Ensure HTML reports land in reports/ directory
	conf.Html = appenv.ReportFile(conf.Html)

	// Auto-discover any SQL pipeline strategies in sql/strategies/
	strategy.AutoRegisterSQLStrategies(appenv.Folder(), conf.Db)

	if conf.Mode == "stale" {
		if err := runStaleCommand(conf.OutDir, conf.Db, conf.Concurrency); err != nil {
			return err
		}
		return nil
	}

	if conf.Mode == "optimized" {
		optArg := strings.TrimSpace(conf.Strategy)
		if optArg == "" && len(conf.Args) > 0 {
			optArg = strings.Join(conf.Args, ",")
		}
		if err := runOptimizedCommand(optArg, conf.Db, conf.Table, conf.OutDir, conf.GridsearchDb, conf.Capital, conf.Symbol, conf.AutoDownload, conf.DownloadYears, conf.Concurrency, !conf.NoReinvestDividends); err != nil {
			return err
		}
		return nil
	}

	if conf.Mode == "stack-eval" {
		primaryID := strings.TrimSpace(conf.Primary)
		if primaryID == "" {
			primaryID = strings.TrimSpace(conf.Strategy)
		}
		if primaryID == "" && len(conf.Args) > 0 {
			primaryID = strings.TrimSpace(conf.Args[0])
		}
		runStackEvalCommand(
			primaryID,
			parseSecondaryList(conf.Secondary),
			conf.IncludeUniverse, conf.IncludeDt,
			conf.DtTop, conf.StackDepth, conf.Concurrency,
			conf.Db, conf.Table, conf.OutDir,
			conf.Capital, conf.Symbol,
			conf.AutoDownload, conf.DownloadYears,
			conf.PersistBest,
		)
		return nil
	}

	if conf.List {
		runner.PrintStrategyList()
		return nil
	}

	// Resolve strategies from flags or positional arguments
	stratArg := strings.TrimSpace(conf.Strategy)
	posArgs := conf.Args
	if stratArg == "" && len(posArgs) > 0 {
		stratArg = strings.Join(posArgs, ",")
	}

	// -signals-only: same path as cmd/livescan (wrapper around RunSignalScan).
	if conf.SignalsOnly {
		if conf.SharedAccount || conf.Primary != "" || conf.Secondary != "" || strings.Contains(stratArg, "+") {
			return fmt.Errorf("-signals-only does not support shared-account / + stacks; use livescan or plain -strategy lists")
		}
		selected, err := runner.ResolveStrategies(stratArg, "bb-capitulation")
		if err != nil {
			return fmt.Errorf("%v", err)
		}
		fmt.Printf("\n📡 backtest -signals-only → runner.RunSignalScan (same as livescan)\n")
		res, err := runner.RunSignalScan(runner.SignalScanOptions{
			Out:           os.Stdout,
			MarketDB:      conf.Db,
			Table:         conf.Table,
			Strategies:    selected,
			SymbolFilter:  conf.Symbol,
			BarsLimit:     conf.Bars,
			AutoDownload:  conf.AutoDownload,
			DownloadYears: conf.DownloadYears,
			Concurrency:   conf.Concurrency,
			OutDir:        conf.OutDir,
		})
		if err != nil {
			return fmt.Errorf("signals-only: %v", err)
		}
		fmt.Printf("📅 Tip/as-of %s → next session %s (%d symbols)\n\n", res.AsOf, res.NextSession, res.SymbolsLoaded)
		entering := 0
		for _, r := range res.Rows {
			mark := "NO_SIGNAL"
			if r.Status == "ENTER" {
				mark = "ENTER"
				entering++
			}
			fmt.Printf("  %-28s %-10s %s\n", r.StrategyID, mark, r.Symbols)
		}
		fmt.Printf("\n%d/%d ENTER · wrote %s\n", entering, len(res.Rows), res.OutDBPath)
		return nil
	}

	// Detect if user requested Shared Account Mode
	isSharedAccount := conf.SharedAccount || conf.Primary != "" || conf.Secondary != "" || strings.Contains(stratArg, "+")

	if isSharedAccount {
		var primaryStrat strategy.Strategy
		var secondaryStrats []strategy.Strategy

		if conf.Primary != "" {
			s, exists := strategy.Get(conf.Primary)
			if !exists {
				return fmt.Errorf("Primary strategy '%s' not found in registry.", conf.Primary)
			}
			primaryStrat = s

			if conf.Secondary != "" {
				secTokens := strings.FieldsFunc(conf.Secondary, func(r rune) bool { return r == ',' || r == ' ' })
				for _, tok := range secTokens {
					sec, exists := strategy.Get(strings.TrimSpace(tok))
					if !exists {
						return fmt.Errorf("Secondary strategy '%s' not found in registry.", tok)
					}
					secondaryStrats = append(secondaryStrats, sec)
				}
			}
		} else if strings.Contains(stratArg, "+") {
			parts := strings.Split(stratArg, "+")
			pID := strings.TrimSpace(parts[0])
			pStrat, exists := strategy.Get(pID)
			if !exists {
				return fmt.Errorf("Primary strategy '%s' not found in registry.", pID)
			}
			primaryStrat = pStrat

			for _, p := range parts[1:] {
				sID := strings.TrimSpace(p)
				sStrat, exists := strategy.Get(sID)
				if !exists {
					return fmt.Errorf("Secondary strategy '%s' not found in registry.", sID)
				}
				secondaryStrats = append(secondaryStrats, sStrat)
			}
		} else {
			// Spliced from -strategy comma-separated list
			tokens := strings.FieldsFunc(stratArg, func(r rune) bool { return r == ',' || r == ' ' })
			if len(tokens) < 2 {
				return fmt.Errorf("Shared account mode requires at least 2 strategies (primary + secondary).")
			}
			pStrat, exists := strategy.Get(strings.TrimSpace(tokens[0]))
			if !exists {
				return fmt.Errorf("Primary strategy '%s' not found in registry.", tokens[0])
			}
			primaryStrat = pStrat

			for _, tok := range tokens[1:] {
				sStrat, exists := strategy.Get(strings.TrimSpace(tok))
				if !exists {
					return fmt.Errorf("Secondary strategy '%s' not found in registry.", tok)
				}
				secondaryStrats = append(secondaryStrats, sStrat)
			}
		}

		allStrats := append([]strategy.Strategy{primaryStrat}, secondaryStrats...)

		// Detect missing market data and download
		if err := runner.DetectAndDownloadMissingData(conf.Db, conf.Table, allStrats, conf.Symbol, conf.AutoDownload, conf.DownloadYears); err != nil {
			return fmt.Errorf("Market data resolution error: %v", err)
		}

		db, err := storage.OpenSQLite(conf.Db)
		if err != nil {
			return fmt.Errorf("Failed to open source DB %s: %v", conf.Db, err)
		}
		defer db.Close()

		// Always include SPY: ExecuteSharedAccount falls back to it as the
		// benchmark when a strategy's own DefaultConfig().Benchmark is empty,
		// and RequiredSymbolsFor only picks up non-empty benchmarks.
		reqSymbols := append(runner.RequiredSymbolsFor(allStrats, conf.Symbol), "SPY")
		fmt.Printf("\n⚙️ Loading bars for %v from table '%s' for Shared-Account Simulation (Starting Capital: $%.2f)...\n", reqSymbols, conf.Table, conf.Capital)
		barsBySymbol, sortedDates, err := storage.FetchBars(db, conf.Table, reqSymbols, backtestStart, "")
		if err != nil {
			return fmt.Errorf("Error loading historical bars for simulation: %v", err)
		}

		sharedRes := runner.ExecuteSharedAccount(
			primaryStrat, secondaryStrats, barsBySymbol, sortedDates, conf.Capital, conf.Symbol, conf.OutDir, conf.Db,
		)
		if sharedRes.Err != nil {
			return fmt.Errorf("Shared account backtest failed: %v", sharedRes.Err)
		}

		runner.PrintSharedAccountTearSheet(sharedRes)

		// Export HTML Report for Shared Account
		if conf.Html != "" {
			symUpper := strings.ToUpper(conf.Symbol)
			reportTitle := fmt.Sprintf("Shared Account Multi-Strategy Report: %s + %s", primaryStrat.Name(), secondaryStrats[0].Name())

			var stratReports []analytics.StrategyReportData
			eqCurves := make(map[string][]float64)
			ddCurves := make(map[string][]float64)

			// 1. Shared Account Combined
			stratReports = append(stratReports, analytics.StrategyReportData{
				ID:     sharedRes.CombinedID,
				Name:   fmt.Sprintf("Consolidated Shared Account (%s + %s)", primaryStrat.Name(), secondaryStrats[0].Name()),
				Type:   "Portfolio",
				Report: sharedRes.CombinedReport,
				Trades: sharedRes.Trades,
			})
			var eqSeries, ddSeries []float64
			for _, pt := range sharedRes.EquityCurve {
				eqSeries = append(eqSeries, pt.TotalEquity)
				ddSeries = append(ddSeries, pt.DrawdownPct)
			}
			eqCurves[sharedRes.CombinedID] = eqSeries
			ddCurves[sharedRes.CombinedID] = ddSeries

			// 2. Primary Strategy Attribution
			pRep := sharedRes.PerStrategyReports[primaryStrat.ID()]
			var pTrades []models.Trade
			for _, t := range sharedRes.Trades {
				if t.StrategyID == primaryStrat.ID() {
					pTrades = append(pTrades, t)
				}
			}
			stratReports = append(stratReports, analytics.StrategyReportData{
				ID:     primaryStrat.ID(),
				Name:   primaryStrat.Name() + " (Primary)",
				Type:   "Primary",
				Report: pRep,
				Trades: pTrades,
			})

			// 3. Secondary Strategies Attribution
			for _, sec := range secondaryStrats {
				sRep := sharedRes.PerStrategyReports[sec.ID()]
				var sTrades []models.Trade
				for _, t := range sharedRes.Trades {
					if t.StrategyID == sec.ID() {
						sTrades = append(sTrades, t)
					}
				}
				stratReports = append(stratReports, analytics.StrategyReportData{
					ID:     sec.ID(),
					Name:   sec.Name() + " (Secondary)",
					Type:   "Secondary",
					Report: sRep,
					Trades: sTrades,
				})
			}

			htmlData := analytics.MultiStrategyHTMLData{
				Title:          reportTitle,
				GeneratedAt:    time.Now().Format("2006-01-02 15:04:05 MST"),
				Symbol:         symUpper,
				StartDate:      sharedRes.CombinedReport.StartDate,
				EndDate:        sharedRes.CombinedReport.EndDate,
				TotalDays:      sharedRes.CombinedReport.TotalTradingDays,
				TotalYears:     sharedRes.CombinedReport.TotalCalendarYears,
				InitialCap:     conf.Capital,
				Strategies:     stratReports,
				AllDates:       sortedDates,
				EquityCurves:   eqCurves,
				DrawdownCurves: ddCurves,
			}

			if err := analytics.GenerateComparisonHTML(conf.Html, htmlData); err != nil {
				log.Printf("Warning: Failed to generate HTML report %s: %v", conf.Html, err)
			} else {
				fmt.Printf("\n✨ Interactive HTML Report generated: %s\n\n", conf.Html)
			}
		}
		return nil
	}

	selectedStrategies, err := runner.ResolveStrategies(stratArg, "bb-capitulation")
	if err != nil {
		return fmt.Errorf("%v. Run with -list to view available strategies.", err)
	}

	// For bulk runs (-strategy all, or a multi-symbol comma list), avoid duplicate
	// work by default: skip any strategy that already has a usable result in
	// -out-dir (same highest-increment-first, skip-if-compromised check used by
	// cmd/scoreboard). A single explicitly-named strategy always runs — that's
	// direct intent, not a bulk sweep. Pass -force to redo everything anyway.
	existing := map[string]runner.CompiledResult{}
	toRun := selectedStrategies
	if len(selectedStrategies) > 1 && !conf.Force {
		fmt.Println("🔎 Checking", conf.OutDir, "for strategies that already have a usable result...")
		existing, _, _, _, _ = runner.ScanAndValidate(conf.OutDir, conf.Concurrency)
		toRun = nil
		for _, s := range selectedStrategies {
			if _, ok := existing[s.ID()]; !ok {
				toRun = append(toRun, s)
			}
		}
		fmt.Printf("   %d/%d strategies already have a usable result and will be skipped; %d need backtesting.\n\n",
			len(selectedStrategies)-len(toRun), len(selectedStrategies), len(toRun))
	}

	var results []runner.RunResult

	if len(toRun) == 0 {
		// Every selected strategy already has a usable result — nothing to
		// simulate, just report what's already there.
		fmt.Println("✅ Nothing to backtest — every selected strategy already has a usable result. (Use -force to redo everything.)")
		for _, s := range selectedStrategies {
			if c, ok := existing[s.ID()]; ok {
				results = append(results, runner.RunResult{Strat: s, Report: c.Report, DbPath: c.DbPath})
			}
		}
		runner.PrintComparisonTable(results)
		return nil
	}

	// Detect missing market data and download before running backtest (scoped to
	// what we're actually about to run).
	if err := runner.DetectAndDownloadMissingData(conf.Db, conf.Table, toRun, conf.Symbol, conf.AutoDownload, conf.DownloadYears); err != nil {
		return fmt.Errorf("Market data resolution error: %v", err)
	}

	// Open read-only historical bars from source DB
	db, err := storage.OpenSQLite(conf.Db)
	if err != nil {
		return fmt.Errorf("Failed to open source DB %s: %v", conf.Db, err)
	}
	defer db.Close()

	var barsBySymbol map[string][]models.Bar
	var sortedDates []string
	if len(selectedStrategies) == 1 {
		// Single strategy: fetch only the symbols it actually needs instead of
		// the entire multi-thousand-symbol database. Loading everything here
		// was a fixed ~20-30s tax paid on every single-strategy invocation
		// regardless of that strategy's own data footprint — e.g. `dt_fxr`
		// looked slow/wasteful because of its 19-year history, but the real
		// cost was this unconditional full-DB load, not FXR's own (correctly
		// scoped, 4866-row) simulation.
		reqSymbols := runner.RequiredSymbolsFor([]strategy.Strategy{toRun[0]}, conf.Symbol)
		fmt.Printf("\n⚙️ Loading bars for %v from table '%s' for Portfolio Simulation (Starting Capital: $%.2f)...\n", reqSymbols, conf.Table, conf.Capital)
		var fetchErr error
		barsBySymbol, sortedDates, fetchErr = storage.FetchBars(db, conf.Table, reqSymbols, backtestStart, "")
		if fetchErr != nil {
			return fmt.Errorf("Error loading historical bars for simulation: %v", fetchErr)
		}
	} else {
		fmt.Printf("\n⚙️ Loading chronological bars from table '%s' for Portfolio Simulation (Starting Capital: $%.2f)...\n", conf.Table, conf.Capital)
		var fetchErr error
		barsBySymbol, sortedDates, fetchErr = storage.FetchBars(db, conf.Table, nil, backtestStart, "")
		if fetchErr != nil {
			return fmt.Errorf("Error loading historical bars for simulation: %v", fetchErr)
		}
	}

	if len(selectedStrategies) == 1 {
		// A single explicitly-named strategy always takes this detailed path
		// (toRun == selectedStrategies here since skip-filtering only applies
		// when more than one strategy was selected).
		strat := toRun[0]
		cfg := runner.BuildConfig(strat, conf.Stoploss, conf.Target, conf.Hold, conf.MaxPositions)

		fmt.Printf("\n========================================================================================\n")
		fmt.Printf("🎯 STRATEGY SELECTED: %s (ID: %s)\n", strat.Name(), strat.ID())
		fmt.Printf("   Description:  %s\n", strat.Description())
		tpPct := cfg.TakeProfitPct * 100
		if tpPct == 0 && cfg.TargetPct > 1.0 {
			tpPct = (cfg.TargetPct - 1.0) * 100
		}
		slPct := cfg.StopLossPct * 100
		if slPct > 50 {
			slPct = (1.0 - cfg.StopLossPct) * 100
		}
		fmt.Printf("   Target:       +%.1f%% | Stop-Loss: -%.1f%% | Max Hold: %d days | Max Positions: %d\n",
			tpPct, slPct, cfg.HoldingWindow, cfg.PositionCap)
		fmt.Printf("========================================================================================\n")

		res := runner.ExecuteStrategyWithDividends(strat, cfg, barsBySymbol, sortedDates, conf.Capital, conf.Symbol, conf.OutDir, conf.Db, reinvestDividends)
		if res.Err != nil {
			return fmt.Errorf("Backtest failed: %v", res.Err)
		}
		results = []runner.RunResult{res}

		fmt.Printf("Generated %d entry signals.\n", res.SignalCount)
		fmt.Printf("💾 Persisted %d generated entry signals to SQLite 'signals' table.\n", res.SignalCount)
		fmt.Printf("💾 Persisted %d completed trades to SQLite 'trades' table.\n", len(res.Trades))
		fmt.Printf("💾 Persisted %d daily equity points to SQLite 'equity_curve' table.\n", len(res.EquityCurve))
		fmt.Printf("💾 Persisted quantitative performance summary to SQLite 'performance_summary' table.\n")

		runner.PrintPerformanceTearSheet(strat.Name(), res.Report)
		runner.PrintReturnBreakdown(res)
		runner.PrintNotes(res)
		runner.PrintTradesTable(res.Trades, conf.Symbol)

		fmt.Printf("\n💾 Dedicated Strategy SQLite Database: %s\n", res.DbPath)
	} else {
		fmt.Printf("\n========================================================================================\n")
		fmt.Printf("🚀 CONCURRENT STRATEGY BACKTESTING (%d STRATEGIES TO RUN, %d TOTAL SELECTED)\n", len(toRun), len(selectedStrategies))
		fmt.Printf("   Market Data:  %d symbols across %d dates loaded from %s\n", len(barsBySymbol), len(sortedDates), conf.Db)
		fmt.Printf("   Output Dir:   %s/ (each strategy writes to an isolated, uniquely suffixed SQLite DB)\n", conf.OutDir)
		fmt.Printf("========================================================================================\n\n")

		freshResults := make([]runner.RunResult, len(toRun))

		// Bounded worker pool — each worker holds a full copy of the shared bar map's
		// working set in-flight (PortfolioSimulator + signals + equity curve), and some
		// strategies (e.g. genetic-momentum) shell out to Python. Unbounded goroutines
		// here (one per strategy) can OOM-kill the process when there are hundreds of
		// strategies, so cap concurrency instead.
		workers := conf.Concurrency
		if workers < 1 {
			workers = 1
		}
		if workers > len(toRun) {
			workers = len(toRun)
		}
		fmt.Printf("   Concurrency:  %d workers\n\n", workers)

		jobs := make(chan int, len(toRun))
		for i := range toRun {
			jobs <- i
		}
		close(jobs)

		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for idx := range jobs {
					s := toRun[idx]
					cfg := runner.BuildConfig(s, conf.Stoploss, conf.Target, conf.Hold, conf.MaxPositions)
					res := runner.ExecuteStrategyWithDividends(s, cfg, barsBySymbol, sortedDates, conf.Capital, conf.Symbol, conf.OutDir, conf.Db, reinvestDividends)
					freshResults[idx] = res
					if res.Err != nil {
						log.Printf("❌ [%s] Error: %v\n", s.ID(), res.Err)
					} else {
						fmt.Printf("✅ [%s] Completed: %d signals, %d trades, Return: %+.2f%%, Sharpe: %.2f ➔ %s\n",
							s.ID(), res.SignalCount, len(res.Trades), res.Report.TotalReturnPct*100, res.Report.SharpeRatio, res.DbPath)
					}
				}
			}()
		}

		wg.Wait()

		// Merge freshly-run results with whatever already had a usable result so
		// the printed table/HTML export covers every selected strategy, not just
		// the ones we actually had to run.
		freshByID := make(map[string]runner.RunResult, len(freshResults))
		for _, r := range freshResults {
			freshByID[r.Strat.ID()] = r
		}
		for _, s := range selectedStrategies {
			if res, ok := freshByID[s.ID()]; ok {
				results = append(results, res)
				continue
			}
			if c, ok := existing[s.ID()]; ok {
				results = append(results, runner.RunResult{Strat: s, Report: c.Report, DbPath: c.DbPath})
			}
		}

		runner.PrintComparisonTable(results)
	}

	// Export HTML Report for standalone/concurrent mode
	if conf.Html != "" {
		symUpper := strings.ToUpper(conf.Symbol)
		reportTitle := "Multi-Strategy Quantitative Comparison Report"
		if len(selectedStrategies) == 1 {
			reportTitle = fmt.Sprintf("%s Performance Report", selectedStrategies[0].Name())
			if symUpper != "" {
				reportTitle = fmt.Sprintf("%s Performance Report for %s", selectedStrategies[0].Name(), symUpper)
			}
		}

		var stratReports []analytics.StrategyReportData
		eqCurves := make(map[string][]float64)
		ddCurves := make(map[string][]float64)

		var startDate, endDate string
		var totalDays int
		var totalYears float64

		for _, r := range results {
			if r.Err != nil {
				continue
			}
			sType := "Go"
			if strings.HasSuffix(r.Strat.ID(), "-sql") {
				sType = "SQL"
			}
			stratReports = append(stratReports, analytics.StrategyReportData{
				ID:     r.Strat.ID(),
				Name:   r.Strat.Name(),
				Type:   sType,
				Report: r.Report,
				Trades: r.Trades,
			})
			var eqSeries, ddSeries []float64
			for _, pt := range r.EquityCurve {
				eqSeries = append(eqSeries, pt.TotalEquity)
				ddSeries = append(ddSeries, pt.DrawdownPct)
			}
			eqCurves[r.Strat.ID()] = eqSeries
			ddCurves[r.Strat.ID()] = ddSeries

			if startDate == "" {
				startDate = r.Report.StartDate
				endDate = r.Report.EndDate
				totalDays = r.Report.TotalTradingDays
				totalYears = r.Report.TotalCalendarYears
			}
		}

		htmlData := analytics.MultiStrategyHTMLData{
			Title:          reportTitle,
			GeneratedAt:    time.Now().Format("2006-01-02 15:04:05 MST"),
			Symbol:         symUpper,
			StartDate:      startDate,
			EndDate:        endDate,
			TotalDays:      totalDays,
			TotalYears:     totalYears,
			InitialCap:     conf.Capital,
			Strategies:     stratReports,
			AllDates:       sortedDates,
			EquityCurves:   eqCurves,
			DrawdownCurves: ddCurves,
		}

		err := analytics.GenerateComparisonHTML(conf.Html, htmlData)
		if err != nil {
			log.Printf("Warning: Failed to generate HTML report %s: %v", conf.Html, err)
		} else {
			fmt.Printf("\n✨ Interactive HTML Report generated: %s\n\n", conf.Html)
		}
	}
	return nil
}
