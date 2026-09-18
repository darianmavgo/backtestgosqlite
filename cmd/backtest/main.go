package main

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

func main() {
	defaultMarketDb := cliutils.GetDefaultMarketDB()

	// Subcommand dispatch:
	//   backtest stale      -> assess which strategies' cached reports/*.db
	//                          results are stale (unregistered strategy,
	//                          newer market data, or an edited SQL pipeline
	//                          since the result was made) and exit — no
	//                          backtests run
	//   backtest optimized  -> run every selected strategy (default: all) with
	//                          the best config a prior `gridsearch` sweep
	//                          found for it, instead of its baseline defaults
	//   backtest stack-eval -> rank existing strategies as idle-cash overlays
	//                          on one primary, one shared cash ledger
	staleMode := false
	optimizedMode := false
	stackEvalMode := false
	if len(os.Args) > 1 && os.Args[1] == "stale" {
		staleMode = true
		os.Args = append(os.Args[:1], os.Args[2:]...) // drop the subcommand so flag.Parse still works
	} else if len(os.Args) > 1 && os.Args[1] == "optimized" {
		optimizedMode = true
		os.Args = append(os.Args[:1], os.Args[2:]...)
	} else if len(os.Args) > 1 && (os.Args[1] == "stack-eval" || os.Args[1] == "stackeval") {
		stackEvalMode = true
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}

	targetDb := flag.String("db", defaultMarketDb, "Path to source SQLite DB containing historical market bars")
	tableName := flag.String("table", "backtest_start", "Table name containing historical bars")
	strategyType := flag.String("strategy", "", "Strategy ID to run, comma-separated list, 'all', or 'strat1+strat2' for shared account")
	sharedAccountFlag := flag.Bool("shared-account", false, "Run strategies in a single shared cash account with priority preemption")
	primaryFlag := flag.String("primary", "", "Primary strategy ID for shared-account execution (has capital priority)")
	secondaryFlag := flag.String("secondary", "", "Secondary strategy ID(s) for shared-account execution (comma-separated)")
	outDir := flag.String("out-dir", appenv.Reports(), "Directory to write strategy SQLite database results and reports")
	listFlag := flag.Bool("list", false, "List all registered Go and SQL strategies")
	symbolFilter := flag.String("symbol", "", "Optional: Filter backtest to a specific symbol (e.g. DFEN, SOXL)")
	capital := flag.Float64("capital", 100000.0, "Starting portfolio capital for simulation")
	maxPositions := flag.Int("max-positions", 0, "Optional override: Maximum concurrent open positions allowed")
	stopLoss := flag.Float64("stoploss", 0.0, "Optional override: Stop-loss floor multiplier (e.g. 0.93 for -7%)")
	profitTarget := flag.Float64("target", 0.0, "Optional override: Take-profit multiplier (e.g. 1.18 for +18%)")
	holdWindow := flag.Int("hold", 0, "Optional override: Max holding days window")
	htmlOutput := flag.String("html", appenv.ReportFile("backtest_report.html"), "Path to export interactive HTML dashboard report")
	autoDownload := flag.Bool("auto-download", true, "Automatically detect missing market data and run download")
	downloadYears := flag.Int("download-years", 5, "Number of years of history to fetch when downloading missing data")
	concurrency := flag.Int("concurrency", runtime.NumCPU(), "Max concurrent strategies when running more than one (defaults to all CPU cores; bounds memory use for large -strategy all runs)")
	force := flag.Bool("force", false, "(multi-strategy runs only) redo every strategy even if it already has a usable result in -out-dir")
	gridDBPath := flag.String("gridsearch-db", appenv.ReportFile("gridsearch.db"), "(optimized subcommand only) SQLite DB of gridsearch results to read best configs from")
	includeUniverse := flag.Bool("include-universe", false, "(stack-eval) also try full-universe overlays (bb-capitulation, rsi2, ...)")
	includeDT := flag.Bool("include-dt", false, "(stack-eval) also try auto-fit dt_* ETF decision trees")
	dtTop := flag.Int("dt-top", 15, "(stack-eval) how many highest-scored dt_* trees to include with -include-dt")
	stackDepth := flag.Int("stack-depth", 3, "(stack-eval) greedy complementary overlays to combine after pairwise ranking")
	persistBest := flag.Bool("persist-best", true, "(stack-eval) write a shared_*.db for the greedy N-way stack")
	startFlag := flag.String("start", storage.DefaultStartDate, "Earliest bar date (YYYY-MM-DD) to simulate; earlier bars are only used for SMA warmup. Empty = full history")
	flag.Parse()
	backtestStart = *startFlag

	// Ensure HTML reports land in reports/ directory
	*htmlOutput = appenv.ReportFile(*htmlOutput)

	// Auto-discover any SQL pipeline strategies in sql/strategies/
	strategy.AutoRegisterSQLStrategies(appenv.Folder(), *targetDb)

	if staleMode {
		runStaleCommand(*outDir, *targetDb, *concurrency)
		return
	}

	if optimizedMode {
		optArg := strings.TrimSpace(*strategyType)
		if optArg == "" && len(flag.Args()) > 0 {
			optArg = strings.Join(flag.Args(), ",")
		}
		runOptimizedCommand(optArg, *targetDb, *tableName, *outDir, *gridDBPath, *capital, *symbolFilter, *autoDownload, *downloadYears, *concurrency)
		return
	}

	if stackEvalMode {
		primaryID := strings.TrimSpace(*primaryFlag)
		if primaryID == "" {
			primaryID = strings.TrimSpace(*strategyType)
		}
		if primaryID == "" && len(flag.Args()) > 0 {
			primaryID = strings.TrimSpace(flag.Args()[0])
		}
		runStackEvalCommand(
			primaryID,
			parseSecondaryList(*secondaryFlag),
			*includeUniverse, *includeDT,
			*dtTop, *stackDepth, *concurrency,
			*targetDb, *tableName, *outDir,
			*capital, *symbolFilter,
			*autoDownload, *downloadYears,
			*persistBest,
		)
		return
	}

	if *listFlag {
		runner.PrintStrategyList()
		return
	}

	// Resolve strategies from flags or positional arguments
	stratArg := strings.TrimSpace(*strategyType)
	posArgs := flag.Args()
	if stratArg == "" && len(posArgs) > 0 {
		stratArg = strings.Join(posArgs, ",")
	}

	// Detect if user requested Shared Account Mode
	isSharedAccount := *sharedAccountFlag || *primaryFlag != "" || *secondaryFlag != "" || strings.Contains(stratArg, "+")

	if isSharedAccount {
		var primaryStrat strategy.Strategy
		var secondaryStrats []strategy.Strategy

		if *primaryFlag != "" {
			s, exists := strategy.Get(*primaryFlag)
			if !exists {
				log.Fatalf("Primary strategy '%s' not found in registry.", *primaryFlag)
			}
			primaryStrat = s

			if *secondaryFlag != "" {
				secTokens := strings.FieldsFunc(*secondaryFlag, func(r rune) bool { return r == ',' || r == ' ' })
				for _, tok := range secTokens {
					sec, exists := strategy.Get(strings.TrimSpace(tok))
					if !exists {
						log.Fatalf("Secondary strategy '%s' not found in registry.", tok)
					}
					secondaryStrats = append(secondaryStrats, sec)
				}
			}
		} else if strings.Contains(stratArg, "+") {
			parts := strings.Split(stratArg, "+")
			pID := strings.TrimSpace(parts[0])
			pStrat, exists := strategy.Get(pID)
			if !exists {
				log.Fatalf("Primary strategy '%s' not found in registry.", pID)
			}
			primaryStrat = pStrat

			for _, p := range parts[1:] {
				sID := strings.TrimSpace(p)
				sStrat, exists := strategy.Get(sID)
				if !exists {
					log.Fatalf("Secondary strategy '%s' not found in registry.", sID)
				}
				secondaryStrats = append(secondaryStrats, sStrat)
			}
		} else {
			// Spliced from -strategy comma-separated list
			tokens := strings.FieldsFunc(stratArg, func(r rune) bool { return r == ',' || r == ' ' })
			if len(tokens) < 2 {
				log.Fatalf("Shared account mode requires at least 2 strategies (primary + secondary).")
			}
			pStrat, exists := strategy.Get(strings.TrimSpace(tokens[0]))
			if !exists {
				log.Fatalf("Primary strategy '%s' not found in registry.", tokens[0])
			}
			primaryStrat = pStrat

			for _, tok := range tokens[1:] {
				sStrat, exists := strategy.Get(strings.TrimSpace(tok))
				if !exists {
					log.Fatalf("Secondary strategy '%s' not found in registry.", tok)
				}
				secondaryStrats = append(secondaryStrats, sStrat)
			}
		}

		allStrats := append([]strategy.Strategy{primaryStrat}, secondaryStrats...)

		// Detect missing market data and download
		if err := runner.DetectAndDownloadMissingData(*targetDb, *tableName, allStrats, *symbolFilter, *autoDownload, *downloadYears); err != nil {
			log.Fatalf("Market data resolution error: %v", err)
		}

		db, err := storage.OpenSQLite(*targetDb)
		if err != nil {
			log.Fatalf("Failed to open source DB %s: %v", *targetDb, err)
		}
		defer db.Close()

		// Always include SPY: ExecuteSharedAccount falls back to it as the
		// benchmark when a strategy's own DefaultConfig().Benchmark is empty,
		// and RequiredSymbolsFor only picks up non-empty benchmarks.
		reqSymbols := append(runner.RequiredSymbolsFor(allStrats, *symbolFilter), "SPY")
		fmt.Printf("\n⚙️ Loading bars for %v from table '%s' for Shared-Account Simulation (Starting Capital: $%.2f)...\n", reqSymbols, *tableName, *capital)
		barsBySymbol, sortedDates, err := storage.FetchBars(db, *tableName, reqSymbols, backtestStart, "")
		if err != nil {
			log.Fatalf("Error loading historical bars for simulation: %v", err)
		}

		sharedRes := runner.ExecuteSharedAccount(
			primaryStrat, secondaryStrats, barsBySymbol, sortedDates, *capital, *symbolFilter, *outDir, *targetDb,
		)
		if sharedRes.Err != nil {
			log.Fatalf("Shared account backtest failed: %v", sharedRes.Err)
		}

		runner.PrintSharedAccountTearSheet(sharedRes)

		// Export HTML Report for Shared Account
		if *htmlOutput != "" {
			symUpper := strings.ToUpper(*symbolFilter)
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
				InitialCap:     *capital,
				Strategies:     stratReports,
				AllDates:       sortedDates,
				EquityCurves:   eqCurves,
				DrawdownCurves: ddCurves,
			}

			if err := analytics.GenerateComparisonHTML(*htmlOutput, htmlData); err != nil {
				log.Printf("Warning: Failed to generate HTML report %s: %v", *htmlOutput, err)
			} else {
				fmt.Printf("\n✨ Interactive HTML Report generated: %s\n\n", *htmlOutput)
			}
		}
		return
	}

	if stratArg == "" {
		stratArg = "bb-capitulation"
	}

	var selectedStrategies []strategy.Strategy
	if strings.ToLower(stratArg) == "all" {
		selectedStrategies = strategy.List()
	} else {
		tokens := strings.FieldsFunc(stratArg, func(r rune) bool {
			return r == ',' || r == ' '
		})
		for _, token := range tokens {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			s, exists := strategy.Get(token)
			if !exists {
				log.Fatalf("Strategy '%s' not found in registry. Run with -list to view available strategies.", token)
			}
			selectedStrategies = append(selectedStrategies, s)
		}
	}

	if len(selectedStrategies) == 0 {
		log.Fatalf("No valid strategies selected. Run with -list to view available strategies.")
	}

	// For bulk runs (-strategy all, or a multi-symbol comma list), avoid duplicate
	// work by default: skip any strategy that already has a usable result in
	// -out-dir (same highest-increment-first, skip-if-compromised check used by
	// cmd/scoreboard). A single explicitly-named strategy always runs — that's
	// direct intent, not a bulk sweep. Pass -force to redo everything anyway.
	existing := map[string]runner.CompiledResult{}
	toRun := selectedStrategies
	if len(selectedStrategies) > 1 && !*force {
		fmt.Println("🔎 Checking", *outDir, "for strategies that already have a usable result...")
		existing, _, _, _, _ = runner.ScanAndValidate(*outDir, *concurrency)
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
		return
	}

	// Detect missing market data and download before running backtest (scoped to
	// what we're actually about to run).
	if err := runner.DetectAndDownloadMissingData(*targetDb, *tableName, toRun, *symbolFilter, *autoDownload, *downloadYears); err != nil {
		log.Fatalf("Market data resolution error: %v", err)
	}

	// Open read-only historical bars from source DB
	db, err := storage.OpenSQLite(*targetDb)
	if err != nil {
		log.Fatalf("Failed to open source DB %s: %v", *targetDb, err)
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
		reqSymbols := runner.RequiredSymbolsFor([]strategy.Strategy{toRun[0]}, *symbolFilter)
		fmt.Printf("\n⚙️ Loading bars for %v from table '%s' for Portfolio Simulation (Starting Capital: $%.2f)...\n", reqSymbols, *tableName, *capital)
		var fetchErr error
		barsBySymbol, sortedDates, fetchErr = storage.FetchBars(db, *tableName, reqSymbols, backtestStart, "")
		if fetchErr != nil {
			log.Fatalf("Error loading historical bars for simulation: %v", fetchErr)
		}
	} else {
		fmt.Printf("\n⚙️ Loading chronological bars from table '%s' for Portfolio Simulation (Starting Capital: $%.2f)...\n", *tableName, *capital)
		var fetchErr error
		barsBySymbol, sortedDates, fetchErr = storage.FetchBars(db, *tableName, nil, backtestStart, "")
		if fetchErr != nil {
			log.Fatalf("Error loading historical bars for simulation: %v", fetchErr)
		}
	}

	if len(selectedStrategies) == 1 {
		// A single explicitly-named strategy always takes this detailed path
		// (toRun == selectedStrategies here since skip-filtering only applies
		// when more than one strategy was selected).
		strat := toRun[0]
		cfg := runner.BuildConfig(strat, *stopLoss, *profitTarget, *holdWindow, *maxPositions)

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

		res := runner.ExecuteStrategy(strat, cfg, barsBySymbol, sortedDates, *capital, *symbolFilter, *outDir, *targetDb)
		if res.Err != nil {
			log.Fatalf("Backtest failed: %v", res.Err)
		}
		results = []runner.RunResult{res}

		fmt.Printf("Generated %d entry signals.\n", res.SignalCount)
		fmt.Printf("💾 Persisted %d generated entry signals to SQLite 'signals' table.\n", res.SignalCount)
		fmt.Printf("💾 Persisted %d completed trades to SQLite 'trades' table.\n", len(res.Trades))
		fmt.Printf("💾 Persisted %d daily equity points to SQLite 'equity_curve' table.\n", len(res.EquityCurve))
		fmt.Printf("💾 Persisted quantitative performance summary to SQLite 'performance_summary' table.\n")

		runner.PrintPerformanceTearSheet(strat.Name(), res.Report)
		runner.PrintTradesTable(res.Trades, *symbolFilter)

		fmt.Printf("\n💾 Dedicated Strategy SQLite Database: %s\n", res.DbPath)
	} else {
		fmt.Printf("\n========================================================================================\n")
		fmt.Printf("🚀 CONCURRENT STRATEGY BACKTESTING (%d STRATEGIES TO RUN, %d TOTAL SELECTED)\n", len(toRun), len(selectedStrategies))
		fmt.Printf("   Market Data:  %d symbols across %d dates loaded from %s\n", len(barsBySymbol), len(sortedDates), *targetDb)
		fmt.Printf("   Output Dir:   %s/ (each strategy writes to an isolated, uniquely suffixed SQLite DB)\n", *outDir)
		fmt.Printf("========================================================================================\n\n")

		freshResults := make([]runner.RunResult, len(toRun))

		// Bounded worker pool — each worker holds a full copy of the shared bar map's
		// working set in-flight (PortfolioSimulator + signals + equity curve), and some
		// strategies (e.g. genetic-momentum) shell out to Python. Unbounded goroutines
		// here (one per strategy) can OOM-kill the process when there are hundreds of
		// strategies, so cap concurrency instead.
		workers := *concurrency
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
					cfg := runner.BuildConfig(s, *stopLoss, *profitTarget, *holdWindow, *maxPositions)
					res := runner.ExecuteStrategy(s, cfg, barsBySymbol, sortedDates, *capital, *symbolFilter, *outDir, *targetDb)
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
	if *htmlOutput != "" {
		symUpper := strings.ToUpper(*symbolFilter)
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
			InitialCap:     *capital,
			Strategies:     stratReports,
			AllDates:       sortedDates,
			EquityCurves:   eqCurves,
			DrawdownCurves: ddCurves,
		}

		err := analytics.GenerateComparisonHTML(*htmlOutput, htmlData)
		if err != nil {
			log.Printf("Warning: Failed to generate HTML report %s: %v", *htmlOutput, err)
		} else {
			fmt.Printf("\n✨ Interactive HTML Report generated: %s\n\n", *htmlOutput)
		}
	}
}
