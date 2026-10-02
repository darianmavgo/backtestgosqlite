// cmd/gridsearch — Parallel parameter sweep assessing baked-in strategy parameters.
//
// Usage:
//
//	# Assess and optimize a single strategy:
//	go run cmd/gridsearch/main.go gld_decline
//	go run cmd/gridsearch/main.go -strategy sig_voo_buy_tecl
//
//	# Assess and optimize many strategies at once (outer concurrency across
//	# strategies, persisted to reports/gridsearch.db, skips strategies already
//	# swept unless -force):
//	go run cmd/gridsearch/main.go -strategy mara_tree,pdd_tree,gld_decline
//	go run cmd/gridsearch/main.go -strategy all
//
//	# List all optimizable strategies:
//	go run cmd/gridsearch/main.go -list
//
//	# Print the resolved parameter grid for a strategy (or 'all') without
//	# running any backtests — no DB access required:
//	go run cmd/gridsearch/main.go params gld_decline
//	go run cmd/gridsearch/main.go params all
//
//	# Assess every strategy with a completed sweep for staleness (renamed/
//	# removed strategy, newer market data, or an edited SQL pipeline since
//	# the sweep ran) — no sweeps run:
//	go run cmd/gridsearch/main.go stale
package gridsearch

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/cliutils"

	"github.com/darianmavgo/backtestgosqlite/pkg/charting"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/stratreg"
	_ "modernc.org/sqlite"
)

type gridResult struct {
	task       sweepTask // the permutation that produced this result (for re-simulation)
	Label      string
	Report     models.PerformanceReport
	Trades     []models.Trade
	Curve      []models.DailyEquityPoint
	IsBaseline bool

	// Raw params behind Label, persisted as real columns (not just a
	// formatted string) by recordRun, so `backtest optimized` and
	// `gridsearch promote` have something structured to read back.
	Symbol       string
	SignalSymbol string
	SignalDays   int
	HoldDays     int
	TakeProfit   float64 // fractional offset, e.g. 0.05 for +5% (0 = tree_bounce/no-TP)
	StopLoss     float64 // fractional offset, e.g. 0.05 for -5% (0 = no-SL)
	Regime       string
	Allocation   float64
}

// sweepOutcome is everything produced by running a full parameter sweep for one
// strategy — shared between single-strategy CLI output and batch/persisted mode.
type sweepOutcome struct {
	Strat         strategy.Strategy
	ParamSpace    strategy.ParameterSpace
	SignalBars    []models.Bar
	TotalPerms    int
	Results       []gridResult
	BaselineRes   *gridResult
	TopCalmar     []gridResult
	TopResilience []gridResult
	TopProfit     []gridResult
	Elapsed       time.Duration
}

// resilienceScore rewards high CAGR while penalizing both the depth (MaxDrawdownPct)
// and duration (MaxDrawdownDuration, in trading days) of the worst drawdown. A strategy
// with the same CAGR but a shallower or briefer drawdown scores higher.
func resilienceScore(r models.PerformanceReport) float64 {
	durationYears := float64(r.MaxDrawdownDuration) / 365.0
	return r.CAGR / ((1.0 + r.MaxDrawdownPct) * (1.0 + durationYears))
}

func formatPercents(vals []float64) []string {
	res := make([]string, len(vals))
	for i, v := range vals {
		if v == 0 {
			res[i] = "None (0%)"
		} else {
			res[i] = fmt.Sprintf("%.0f%%", v*100)
		}
	}
	return res
}

// Config holds the settings of a run.
type Config struct {
	Start         string          // -start
	Db            string          // -db
	Strategy      string          // -strategy
	Strat         string          // -strat
	Mode          string          // -mode
	List          bool            // -list
	Signal        string          // -signal
	Symbol        string          // -symbol
	SymbolsFrom   string          // -symbols-from
	TopCagr       int             // -top-cagr
	Capital       float64         // -capital
	Alloc         float64         // -alloc
	Yield         float64         // -yield
	MinTrades     int             // -min-trades
	Top           int             // -top
	Html          string          // -html
	NoHtml        bool            // -no-html
	Concurrency   int             // -concurrency
	Force         bool            // -force
	IncludeStreak bool            // -include-streak
	MinWinRate    float64         // -min-win-rate (promote)
	GridsearchDb  string          // -gridsearch-db
	MaxPerms      int             // -max-perms
	Subcommand    string          // "params", "stale", "promote", or empty
	Passed        map[string]bool // flags set explicitly on the command line (nil = none)
	Args          []string        // positional arguments
}

// DefaultConfig returns the CLI defaults.
func DefaultConfig() Config {
	return Config{
		Start:         storage.DefaultStartDate,
		Db:            appenv.MarketDB(),
		Strategy:      "",
		List:          false,
		Signal:        "",
		Symbol:        "",
		SymbolsFrom:   "",
		TopCagr:       10,
		Capital:       100000.0,
		Alloc:         0.65,
		Yield:         0.045,
		MinTrades:     5,
		Top:           10,
		Html:          "",
		NoHtml:        false,
		Concurrency:   runtime.NumCPU(),
		Force:         false,
		IncludeStreak: false,
		MinWinRate:    0.6,
		GridsearchDb:  appenv.ReportFile("gridsearch.db"),
		MaxPerms:      20000,
	}
}

// Main is the CLI entry point.
func Main() {
	// Subcommand dispatch:
	//   gridsearch <strategy>        -> run the sweep (default)
	//   gridsearch params <strategy> -> print the resolved parameter grid (incl.
	//                                   the consecutive decline/rally-day axis)
	//                                   for one strategy or 'all', with no DB
	//                                   access and no backtests run
	//   gridsearch stale              -> assess every strategy with a recorded
	//                                    sweep in -gridsearch-db for staleness
	//                                    (unregistered strategy, newer market
	//                                    data, or an edited SQL pipeline since
	//                                    the sweep ran) and exit — no sweeps run
	conf := DefaultConfig()
	d := conf
	flag.StringVar(&conf.Start, "start", d.Start, "Earliest bar date (YYYY-MM-DD) to sweep; earlier bars only warm up SMAs. Empty = full history")
	flag.StringVar(&conf.Db, "db", d.Db, "Path to SQLite database")
	flag.StringVar(&conf.Strategy, "strategy", d.Strategy, "Strategy ID (comma-separated list, or 'all') to assess and optimize")
	flag.BoolVar(&conf.List, "list", d.List, "List registered strategies with baked-in parameters")
	flag.StringVar(&conf.Signal, "signal", d.Signal, "Signal generation symbol override (single-strategy mode only)")
	flag.StringVar(&conf.Symbol, "symbol", d.Symbol, "Trade symbol override; comma-separated list allowed (single-strategy mode only)")
	flag.StringVar(&conf.SymbolsFrom, "symbols-from", d.SymbolsFrom, "Study report DB (e.g. data/reports/voo_up3_etf.db) whose etf_compare view supplies the trade symbols, ranked by rank_cagr (use with -top-cagr)")
	flag.IntVar(&conf.TopCagr, "top-cagr", d.TopCagr, "With -symbols-from: how many of the best rank_cagr symbols to sweep")
	flag.Float64Var(&conf.Capital, "capital", d.Capital, "Starting cash ($)")
	flag.Float64Var(&conf.Alloc, "alloc", d.Alloc, "Allocation percentage override (single-strategy mode only)")
	flag.Float64Var(&conf.Yield, "yield", d.Yield, "Cash yield on idle reserves (4.5% = 0.045)")
	flag.IntVar(&conf.MinTrades, "min-trades", d.MinTrades, "Minimum trade count filter")
	flag.IntVar(&conf.Top, "top", d.Top, "Top N results to display per strategy")
	flag.StringVar(&conf.Html, "html", d.Html, "Path to export HTML comparison report (single-strategy mode; defaults to data/reports/<strategy>_gridsearch.html)")
	flag.BoolVar(&conf.NoHtml, "no-html", d.NoHtml, "Skip per-strategy HTML export (batch mode; speeds up large sweeps)")
	flag.IntVar(&conf.Concurrency, "concurrency", d.Concurrency, "Worker goroutines. Single-strategy mode: workers within that one sweep. Multi-strategy mode: total workers shared across every strategy's tasks combined (not per-strategy — a few expensive strategies get proportionally more of the pool once cheap ones finish). Defaults to all CPU cores.")
	flag.BoolVar(&conf.Force, "force", d.Force, "Redo strategies that already have a completed sweep in data/reports/gridsearch.db")
	flag.BoolVar(&conf.IncludeStreak, "include-streak", d.IncludeStreak, "Include streak-* strategies (rows of refdata streak_strategy) in -strategy all. They are already a promoted config, so excluded by default")
	flag.Float64Var(&conf.MinWinRate, "min-win-rate", d.MinWinRate, "gridsearch promote: minimum win rate (0-1)")
	flag.StringVar(&conf.GridsearchDb, "gridsearch-db", d.GridsearchDb, "SQLite DB for the pipeline controller (gridsearch_runs) and results (gridsearch_results) tables")
	flag.IntVar(&conf.MaxPerms, "max-perms", d.MaxPerms, "Multi-strategy mode: skip a strategy whose generic parameter grid exceeds this many permutations (e.g. genetic-momentum's 50-symbol RequiredSymbols list balloons its generic grid to 210,000+ combos, none of which even exercise its real Python-driven signal logic). 0 disables the cap. Single-strategy mode ignores this.")
	conf.Subcommand = cliutils.PopSubcommand(map[string]string{"params": "params", "stale": "stale", "promote": "promote"})
	flag.Parse()
	conf.Passed = map[string]bool{}
	flag.Visit(func(f *flag.Flag) { conf.Passed[f.Name] = true })
	conf.Args = flag.Args()
	if err := Run(conf); err != nil {
		log.Fatal(err)
	}
}

// Run executes the command with cfg. It returns errors instead of exiting.
func Run(conf Config) error {
	strategy.AutoRegisterSQLStrategies(appenv.Folder(), conf.Db)
	stratreg.RegisterFamilies()

	if conf.Subcommand == "stale" {
		runStaleCommand(conf.GridsearchDb, conf.Db)
		return nil
	}

	if conf.List {
		fmt.Println("\n=======================================================================================================================")
		fmt.Println("📋 REGISTERED STRATEGIES AVAILABLE FOR GRID SEARCH OPTIMIZATION:")
		fmt.Println("=======================================================================================================================")
		strategy.PrintFamilyCounts(os.Stdout)
		for _, s := range strategy.List() {
			space := strategy.AssessParameterSpace(s)
			fmt.Printf("  • %-20s %s\n", s.ID(), s.Name())
			fmt.Printf("    Baked-in: Asset=%s | Hold=%dd | TP=%.1f%% | SL=%.1f%% | Alloc=%.0f%%\n",
				space.SignalSymbol, space.Baseline.HoldDays, space.Baseline.TakeProfit*100, space.Baseline.StopLoss*100, space.Baseline.Allocation*100)
		}
		fmt.Println("=======================================================================================================================")
		fmt.Println()
		return nil
	}

	// Resolve strategy selection from flag or positional argument
	stratArg := strings.TrimSpace(conf.Strategy)
	if stratArg == "" && len(conf.Args) > 0 {
		stratArg = strings.TrimSpace(conf.Args[0])
	}
	if stratArg == "" {
		fmt.Println()
		fmt.Println("⚠️  No strategy specified! Please specify a strategy to optimize.")
		fmt.Println("Usage:   go run cmd/gridsearch/main.go <strategy_id>")
		fmt.Println("         go run cmd/gridsearch/main.go -strategy strat1,strat2")
		fmt.Println("         go run cmd/gridsearch/main.go -strategy all")
		fmt.Println()
		fmt.Println("Run with -list to view all available strategies:")
		fmt.Println("         go run cmd/gridsearch/main.go -list")
		fmt.Println()
		fmt.Println("Run with the 'params' subcommand to print the parameter grid without sweeping:")
		fmt.Println("         go run cmd/gridsearch/main.go params <strategy_id>")
		fmt.Println("         go run cmd/gridsearch/main.go params all")
		fmt.Println()
		fmt.Println("Run with the 'stale' subcommand to see which completed sweeps are stale:")
		fmt.Println("         go run cmd/gridsearch/main.go stale")
		fmt.Println()
		fmt.Println("Run with the 'promote' subcommand to copy winning rows into streak_strategy:")
		fmt.Println("         go run cmd/gridsearch/main.go promote -strategy voo-up3 -min-win-rate 0.6 -min-trades 30 -top 5")
		fmt.Println()
		return fmt.Errorf("no strategy specified")
	}

	var targets []strategy.Strategy
	if strings.EqualFold(stratArg, "all") {
		for _, s := range strategy.ListAll() {
			if !conf.IncludeStreak && strings.HasPrefix(s.ID(), "streak-") {
				continue
			}
			targets = append(targets, s)
		}
	} else {
		for _, tok := range strings.Split(stratArg, ",") {
			tok = strings.TrimSpace(tok)
			if tok == "" {
				continue
			}
			s, found := strategy.Get(tok)
			if !found {
				return fmt.Errorf("Unknown strategy '%s'. Run with -list to see available strategies.", tok)
			}
			targets = append(targets, s)
		}
	}
	if len(targets) == 0 {
		return fmt.Errorf("No valid strategies selected.")
	}

	if conf.Subcommand == "promote" {
		minTrades := 30
		if conf.Passed["min-trades"] {
			minTrades = conf.MinTrades
		}
		top := 5
		if conf.Passed["top"] {
			top = conf.Top
		}
		rep, err := Promote(PromoteConfig{
			Parents:    targets,
			GridDB:     conf.GridsearchDb,
			RefDB:      refdb.DefaultPath,
			MinWinRate: conf.MinWinRate,
			MinTrades:  minTrades,
			Top:        top,
		})
		if err != nil {
			return err
		}
		printPromoteReport(rep)
		return nil
	}

	sweepOpts := sweepOptions{
		Capital:   conf.Capital,
		MinTrades: conf.MinTrades,
		TopN:      conf.Top,
		StartDate: conf.Start,
		MarketDB:  conf.Db,
	}
	if conf.Passed["alloc"] {
		v := conf.Alloc
		sweepOpts.AllocOverride = &v
	}
	if conf.Passed["yield"] {
		v := conf.Yield
		sweepOpts.CashYieldOverride = &v
	} else {
		sweepOpts.CashYieldOverride = nil
	}
	if conf.Passed["symbol"] && conf.Symbol != "" {
		for _, sym := range strings.Split(conf.Symbol, ",") {
			if sym = strings.ToUpper(strings.TrimSpace(sym)); sym != "" {
				sweepOpts.SymbolOverride = append(sweepOpts.SymbolOverride, sym)
			}
		}
	}
	if conf.SymbolsFrom != "" {
		syms, err := symbolsFromReport(conf.SymbolsFrom, conf.TopCagr)
		if err != nil {
			return fmt.Errorf("-symbols-from %s: %v", conf.SymbolsFrom, err)
		}
		fmt.Printf("📄 Sweeping top %d rank_cagr symbols from %s: %v\n", len(syms), conf.SymbolsFrom, syms)
		sweepOpts.SymbolOverride = syms
	}
	if conf.Passed["signal"] && conf.Signal != "" {
		sweepOpts.SignalOverride = conf.Signal
	}

	// `params` subcommand: just print the resolved parameter grid for each
	// target strategy and exit — no DB access, no backtests.
	if conf.Subcommand == "params" {
		printParamsCommand(targets, sweepOpts)
		return nil
	}

	db, err := storage.OpenSQLite(conf.Db)
	if err != nil {
		return fmt.Errorf("Failed to open DB: %v", err)
	}
	defer db.Close()

	gdb, err := storage.OpenSQLite(conf.GridsearchDb)
	if err != nil {
		return fmt.Errorf("Failed to open gridsearch pipeline DB %s: %v", conf.GridsearchDb, err)
	}
	defer gdb.Close()
	if err := ensureGridSearchSchema(gdb); err != nil {
		return fmt.Errorf("Failed to initialize gridsearch pipeline schema: %v", err)
	}
	// All runs and results (gridsearch_runs, gridsearch_results) land in this
	// file; print it up front and again at exit so it's easy to find.
	gridDBURL := fileURL(conf.GridsearchDb)
	fmt.Printf("🗄️  Gridsearch DB (calculations + results): %s\n", gridDBURL)
	defer fmt.Printf("\n🗄️  Gridsearch DB (calculations + results): %s\n", gridDBURL)

	// --- Single strategy: preserve the original rich, single-target CLI experience. ---
	if len(targets) == 1 {
		strat := targets[0]
		if !conf.Force && isStrategyDone(gdb, strat.ID()) {
			fmt.Printf("✅ %s already has a completed sweep in %s — skipping. Use -force to redo.\n", strat.ID(), conf.GridsearchDb)
			printCachedResults(gdb, strat)
			return nil
		}

		sweepOpts.InnerWorkers = conf.Concurrency
		printSweepHeader(strat, sweepOpts)

		outcome, err := runSweep(db, strat, sweepOpts)
		recordRun(gdb, strat, outcome, err)
		if err != nil {
			return fmt.Errorf("Grid search failed for %s: %v", strat.ID(), err)
		}
		if len(outcome.Results) == 0 {
			fmt.Println("No configurations met the minimum trade count filter.")
			return nil
		}

		printSweepReport(strat, outcome)

		if !conf.NoHtml {
			reportFile := defaultReportPath(strat, conf.Html)
			if err := exportSweepHTML(strat, outcome, reportFile, conf.Capital); err != nil {
				log.Printf("Warning: Failed to save HTML report: %v", err)
			} else {
				fmt.Printf("\n✨ Interactive Grid Search Chart saved to: %s\n\n", reportFile)
			}
		}
		return nil
	}

	// --- Multiple strategies: outer bounded pool across strategies, persisted. ---
	runBatchSweep(db, gdb, targets, sweepOpts, conf.Concurrency, conf.Force, conf.NoHtml, conf.GridsearchDb, conf.MaxPerms)
	return nil
}

func defaultReportPath(strat strategy.Strategy, override string) string {
	if override != "" {
		reportFile := override
		reportFile = appenv.ReportFile(reportFile)
		return reportFile
	}
	cleanID := strings.ReplaceAll(strat.ID(), "-", "_")
	return appenv.ReportFile(fmt.Sprintf("%s_gridsearch.html", cleanID))
}

func printSweepHeader(strat strategy.Strategy, opts sweepOptions) {
	paramSpace := strategy.AssessParameterSpace(strat)
	if opts.AllocOverride != nil {
		paramSpace.Allocations = []float64{*opts.AllocOverride}
	}
	totalPerms := len(paramSpace.Symbols) * len(paramSpace.SignalDays) * len(paramSpace.HoldDays) *
		len(paramSpace.TakeProfits) * len(paramSpace.StopLosses) * len(paramSpace.Regimes) * len(paramSpace.Allocations)

	fmt.Printf("\n=======================================================================================================================\n")
	fmt.Printf("⚡ STRATEGY GRID SEARCH OPTIMIZER\n")
	fmt.Printf("=======================================================================================================================\n")
	fmt.Printf("Strategy:      %s (ID: %s)\n", strat.Name(), strat.ID())
	fmt.Printf("Description:   %s\n\n", strat.Description())
	fmt.Printf("📋 Baked-In Baseline Parameters:\n")
	fmt.Printf("   • Signal / Asset:     %s (Direction: %s)\n", paramSpace.SignalSymbol, paramSpace.Direction)
	fmt.Printf("   • Consecutive Days:   %d days\n", paramSpace.Baseline.SignalDays)
	fmt.Printf("   • Max Holding Window: %d days\n", paramSpace.Baseline.HoldDays)
	fmt.Printf("   • Take-Profit Target: +%.2f%%\n", paramSpace.Baseline.TakeProfit*100)
	fmt.Printf("   • Stop-Loss Limit:    %.2f%%\n", paramSpace.Baseline.StopLoss*100)
	fmt.Printf("   • Allocation / APY:   %.1f%% capital | %.1f%% idle APY\n\n", paramSpace.Allocations[0]*100, paramSpace.CashYield*100)
	fmt.Printf("🎯 Evaluated Parameter Grid (%d Permutations):\n", totalPerms)
	fmt.Printf("   • Tradable Assets:    %v\n", paramSpace.Symbols)
	fmt.Printf("   • Consecutive Days:   %v\n", paramSpace.SignalDays)
	fmt.Printf("   • Holding Windows:    %v days\n", paramSpace.HoldDays)
	fmt.Printf("   • Take-Profit:        %v\n", formatPercents(paramSpace.TakeProfits))
	fmt.Printf("   • Stop-Loss:          %v\n", formatPercents(paramSpace.StopLosses))
	fmt.Printf("   • Regime Filters:     %v\n", paramSpace.Regimes)
	fmt.Printf("=======================================================================================================================\n\n")
	fmt.Printf("⚙️  Concurrency: %d workers\n", opts.InnerWorkers)
}

func printSweepReport(strat strategy.Strategy, outcome sweepOutcome) {
	fmt.Printf("⚡ Evaluated %d valid configurations in %v\n\n", len(outcome.Results), outcome.Elapsed)

	if outcome.BaselineRes != nil {
		fmt.Println("📌 BAKED-IN STRATEGY BASELINE:")
		fmt.Printf("   %-50s  Net Profit=+$%.2f  CAGR=%.2f%%  MaxDD=%.2f%%  Calmar=%.2f  WR=%.1f%%  Trades=%d  Idle=%s\n\n",
			outcome.BaselineRes.Label, outcome.BaselineRes.Report.NetProfit, outcome.BaselineRes.Report.CAGR*100, outcome.BaselineRes.Report.MaxDrawdownPct*100,
			outcome.BaselineRes.Report.CalmarRatio, outcome.BaselineRes.Report.WinRate*100, outcome.BaselineRes.Report.TotalTrades, formatIdle(outcome.BaselineRes.Report))
	}

	fmt.Printf("⭐ TOP %d BY CALMAR RATIO (Risk-Adjusted):\n", len(outcome.TopCalmar))
	for i, r := range outcome.TopCalmar {
		comp := ""
		if outcome.BaselineRes != nil && outcome.BaselineRes.Report.CalmarRatio > 0 {
			diff := (r.Report.CalmarRatio - outcome.BaselineRes.Report.CalmarRatio) / outcome.BaselineRes.Report.CalmarRatio * 100.0
			comp = fmt.Sprintf(" (%+.0f%% vs base)", diff)
		}
		fmt.Printf("  #%d  %-50s  CAGR=%.2f%%  DD=%.2f%%  Calmar=%.2f  WR=%.1f%%  Trades=%d  Idle=%s%s\n",
			i+1, r.Label, r.Report.CAGR*100, r.Report.MaxDrawdownPct*100, r.Report.CalmarRatio, r.Report.WinRate*100, r.Report.TotalTrades, formatIdle(r.Report), comp)
	}

	var baselineScore float64
	if outcome.BaselineRes != nil {
		baselineScore = resilienceScore(outcome.BaselineRes.Report)
	}
	fmt.Printf("\n🛡️  TOP %d BY RESILIENCE (High CAGR, Shallow & Brief Drawdowns):\n", len(outcome.TopResilience))
	for i, r := range outcome.TopResilience {
		comp := ""
		score := resilienceScore(r.Report)
		if outcome.BaselineRes != nil && baselineScore > 0 {
			diff := (score - baselineScore) / baselineScore * 100.0
			comp = fmt.Sprintf(" (%+.0f%% vs base)", diff)
		}
		fmt.Printf("  #%d  %-50s  CAGR=%.2f%%  DD=%.2f%%  DDdays=%d  Score=%.4f  Trades=%d  Idle=%s%s\n",
			i+1, r.Label, r.Report.CAGR*100, r.Report.MaxDrawdownPct*100, r.Report.MaxDrawdownDuration, score, r.Report.TotalTrades, formatIdle(r.Report), comp)
	}

	fmt.Printf("\n💰 TOP %d BY NET PROFIT:\n", len(outcome.TopProfit))
	for i, r := range outcome.TopProfit {
		comp := ""
		if outcome.BaselineRes != nil && outcome.BaselineRes.Report.NetProfit > 0 {
			diff := (r.Report.NetProfit - outcome.BaselineRes.Report.NetProfit) / outcome.BaselineRes.Report.NetProfit * 100.0
			comp = fmt.Sprintf(" (%+.0f%% vs base)", diff)
		}
		fmt.Printf("  #%d  %-50s  Profit=+$%.2f  CAGR=%.2f%%  Idle=%s%s\n", i+1, r.Label, r.Report.NetProfit, r.Report.CAGR*100, formatIdle(r.Report), comp)
	}
}

func exportSweepHTML(strat strategy.Strategy, outcome sweepOutcome, reportFile string, capital float64) error {
	var multiResults []charting.MultiResult
	if outcome.BaselineRes != nil {
		multiResults = append(multiResults, charting.MultiResult{
			Label:      fmt.Sprintf("⭐ [BASELINE] %s", outcome.BaselineRes.Label),
			Report:     outcome.BaselineRes.Report,
			DailyCurve: outcome.BaselineRes.Curve,
		})
	}
	for _, r := range outcome.TopResilience {
		multiResults = append(multiResults, charting.MultiResult{
			Label:      "🛡️ " + r.Label,
			Report:     r.Report,
			DailyCurve: r.Curve,
		})
	}
	for _, r := range outcome.TopCalmar {
		multiResults = append(multiResults, charting.MultiResult{
			Label:      r.Label,
			Report:     r.Report,
			DailyCurve: r.Curve,
		})
	}

	view := charting.FromMultiReports(
		fmt.Sprintf("Grid Search Optimization: %s", strat.Name()),
		fmt.Sprintf("Parameter sweep centered on baked-in defaults — top %d by Resilience Score, top %d by Calmar Ratio", len(outcome.TopResilience), len(outcome.TopCalmar)),
		multiResults, outcome.SignalBars, capital,
	)
	return charting.GenerateHTML(reportFile, view)
}

// fileURL returns a file:// URL for path (absolute when resolvable).
func fileURL(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return "file://" + filepath.ToSlash(path)
}

// symbolsFromReport returns the n best symbols by rank_cagr from a study
// report DB's etf_compare view.
func symbolsFromReport(path string, n int) ([]string, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := storage.OpenSQLite(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var syms []string
	if err := db.Select(&syms, `SELECT symbol FROM etf_compare ORDER BY rank_cagr, symbol LIMIT ?`, n); err != nil {
		return nil, err
	}
	if len(syms) == 0 {
		return nil, fmt.Errorf("etf_compare returned no symbols")
	}
	return syms, nil
}
