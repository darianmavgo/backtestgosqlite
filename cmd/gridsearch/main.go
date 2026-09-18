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
package main

import (
	"flag"
	"fmt"
	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/charting"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
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
	// formatted string) by recordRun, so `backtest optimized` has something
	// structured to read back instead of parsing Label.
	Symbol     string
	SignalDays int
	HoldDays   int
	TakeProfit float64 // fractional offset, e.g. 0.05 for +5% (0 = tree_bounce/no-TP)
	StopLoss   float64 // fractional offset, e.g. 0.05 for -5% (0 = no-SL)
	Regime     string
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

func main() {
	startFlag := flag.String("start", storage.DefaultStartDate, "Earliest bar date (YYYY-MM-DD) to sweep; earlier bars only warm up SMAs. Empty = full history")
	dbPath := flag.String("db", appenv.MarketDB(), "Path to SQLite database")
	stratFlag := flag.String("strategy", "", "Strategy ID (comma-separated list, or 'all') to assess and optimize")
	stratShort := flag.String("strat", "", "Alias for -strategy")
	modeFlag := flag.String("mode", "", "Legacy compatibility alias for -strategy")
	listFlag := flag.Bool("list", false, "List registered strategies with baked-in parameters")
	signalSym := flag.String("signal", "", "Signal generation symbol override (single-strategy mode only)")
	customTradeSym := flag.String("symbol", "", "Trade symbol override; comma-separated list allowed (single-strategy mode only)")
	fromReport := flag.String("symbols-from", "", "Study report DB (e.g. reports/voo_up3_etf.db) whose etf_compare view supplies the trade symbols, ranked by rank_cagr (use with -top-cagr)")
	topCAGR := flag.Int("top-cagr", 10, "With -symbols-from: how many of the best rank_cagr symbols to sweep")
	capital := flag.Float64("capital", 100000.0, "Starting cash ($)")
	allocPct := flag.Float64("alloc", 0.65, "Allocation percentage override (single-strategy mode only)")
	cashYield := flag.Float64("yield", 0.045, "Cash yield on idle reserves (4.5% = 0.045)")
	minTrades := flag.Int("min-trades", 5, "Minimum trade count filter")
	topN := flag.Int("top", 10, "Top N results to display per strategy")
	htmlOutput := flag.String("html", "", "Path to export HTML comparison report (single-strategy mode; defaults to reports/<strategy>_gridsearch.html)")
	noHTML := flag.Bool("no-html", false, "Skip per-strategy HTML export (batch mode; speeds up large sweeps)")
	concurrency := flag.Int("concurrency", runtime.NumCPU(), "Worker goroutines. Single-strategy mode: workers within that one sweep. Multi-strategy mode: total workers shared across every strategy's tasks combined (not per-strategy — a few expensive strategies get proportionally more of the pool once cheap ones finish). Defaults to all CPU cores.")
	force := flag.Bool("force", false, "Redo strategies that already have a completed sweep in reports/gridsearch.db")
	includeDT := flag.Bool("include-dt", false, "Include dt_* (auto-generated per-ETF decision tree) strategies in -strategy all — they already have their own dedicated sweep via cmd/etf_decision_trees, so excluded by default")
	gridDBPath := flag.String("gridsearch-db", appenv.ReportFile("gridsearch.db"), "SQLite DB for the pipeline controller (gridsearch_runs) and results (gridsearch_results) tables")
	maxPerms := flag.Int("max-perms", 20000, "Multi-strategy mode: skip a strategy whose generic parameter grid exceeds this many permutations (e.g. genetic-momentum's 50-symbol RequiredSymbols list balloons its generic grid to 210,000+ combos, none of which even exercise its real Python-driven signal logic). 0 disables the cap. Single-strategy mode ignores this.")

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
	paramsMode := false
	staleMode := false
	if len(os.Args) > 1 && os.Args[1] == "params" {
		paramsMode = true
		os.Args = append(os.Args[:1], os.Args[2:]...) // drop the subcommand so flag.Parse still works
	} else if len(os.Args) > 1 && os.Args[1] == "stale" {
		staleMode = true
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}
	flag.Parse()

	// Track which flags were explicitly set by the user
	userPassedFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		userPassedFlags[f.Name] = true
	})

	strategy.AutoRegisterSQLStrategies(appenv.Folder(), *dbPath)

	if staleMode {
		runStaleCommand(*gridDBPath, *dbPath)
		return
	}

	if *listFlag {
		fmt.Println("\n=======================================================================================================================")
		fmt.Println("📋 REGISTERED STRATEGIES AVAILABLE FOR GRID SEARCH OPTIMIZATION:")
		fmt.Println("=======================================================================================================================")
		for _, s := range strategy.List() {
			space := strategy.AssessParameterSpace(s)
			fmt.Printf("  • %-20s %s\n", s.ID(), s.Name())
			fmt.Printf("    Baked-in: Asset=%s | Hold=%dd | TP=%.1f%% | SL=%.1f%% | Alloc=%.0f%%\n",
				space.SignalSymbol, space.Baseline.HoldDays, space.Baseline.TakeProfit*100, space.Baseline.StopLoss*100, space.Baseline.Allocation*100)
		}
		fmt.Println("=======================================================================================================================")
		fmt.Println()
		return
	}

	// Resolve strategy selection from flag, positional argument, or legacy mode
	stratArg := strings.TrimSpace(*stratFlag)
	if stratArg == "" {
		stratArg = strings.TrimSpace(*stratShort)
	}
	if stratArg == "" && len(flag.Args()) > 0 {
		stratArg = strings.TrimSpace(flag.Args()[0])
	}
	if stratArg == "" && *modeFlag != "" {
		switch strings.ToLower(*modeFlag) {
		case "gld", "gld_decline", "gld-decline":
			stratArg = "gld_decline"
		case "bull", "voo", "sig_voo_buy_tecl":
			stratArg = "sig-voo-buy-tecl"
		case "bear":
			stratArg = "sig-voo-buy-tecl"
		default:
			stratArg = *modeFlag
		}
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
		os.Exit(1)
	}

	var targets []strategy.Strategy
	if strings.EqualFold(stratArg, "all") {
		for _, s := range strategy.List() {
			if !*includeDT && strings.HasPrefix(s.ID(), "dt_") {
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
				log.Fatalf("Unknown strategy '%s'. Run with -list to see available strategies.", tok)
			}
			targets = append(targets, s)
		}
	}
	if len(targets) == 0 {
		log.Fatalf("No valid strategies selected.")
	}

	sweepOpts := sweepOptions{
		Capital:   *capital,
		MinTrades: *minTrades,
		TopN:      *topN,
		StartDate: *startFlag,
	}
	if userPassedFlags["alloc"] {
		v := *allocPct
		sweepOpts.AllocOverride = &v
	}
	if userPassedFlags["yield"] {
		v := *cashYield
		sweepOpts.CashYieldOverride = &v
	} else {
		sweepOpts.CashYieldOverride = nil
	}
	if userPassedFlags["symbol"] && *customTradeSym != "" {
		for _, sym := range strings.Split(*customTradeSym, ",") {
			if sym = strings.ToUpper(strings.TrimSpace(sym)); sym != "" {
				sweepOpts.SymbolOverride = append(sweepOpts.SymbolOverride, sym)
			}
		}
	}
	if *fromReport != "" {
		syms, err := symbolsFromReport(*fromReport, *topCAGR)
		if err != nil {
			log.Fatalf("-symbols-from %s: %v", *fromReport, err)
		}
		fmt.Printf("📄 Sweeping top %d rank_cagr symbols from %s: %v\n", len(syms), *fromReport, syms)
		sweepOpts.SymbolOverride = syms
	}
	if userPassedFlags["signal"] && *signalSym != "" {
		sweepOpts.SignalOverride = *signalSym
	}

	// `params` subcommand: just print the resolved parameter grid for each
	// target strategy and exit — no DB access, no backtests.
	if paramsMode {
		printParamsCommand(targets, sweepOpts)
		return
	}

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	gdb, err := storage.OpenSQLite(*gridDBPath)
	if err != nil {
		log.Fatalf("Failed to open gridsearch pipeline DB %s: %v", *gridDBPath, err)
	}
	defer gdb.Close()
	if err := ensureGridSearchSchema(gdb); err != nil {
		log.Fatalf("Failed to initialize gridsearch pipeline schema: %v", err)
	}
	// All runs and results (gridsearch_runs, gridsearch_results) land in this
	// file; print it up front and again at exit so it's easy to find.
	gridDBURL := fileURL(*gridDBPath)
	fmt.Printf("🗄️  Gridsearch DB (calculations + results): %s\n", gridDBURL)
	defer fmt.Printf("\n🗄️  Gridsearch DB (calculations + results): %s\n", gridDBURL)

	// --- Single strategy: preserve the original rich, single-target CLI experience. ---
	if len(targets) == 1 {
		strat := targets[0]
		if !*force && isStrategyDone(gdb, strat.ID()) {
			fmt.Printf("✅ %s already has a completed sweep in %s — skipping. Use -force to redo.\n", strat.ID(), *gridDBPath)
			printCachedResults(gdb, strat)
			return
		}

		sweepOpts.InnerWorkers = *concurrency
		printSweepHeader(strat, sweepOpts)

		outcome, err := runSweep(db, strat, sweepOpts)
		recordRun(gdb, strat, outcome, err)
		if err != nil {
			log.Fatalf("Grid search failed for %s: %v", strat.ID(), err)
		}
		if len(outcome.Results) == 0 {
			fmt.Println("No configurations met the minimum trade count filter.")
			return
		}

		printSweepReport(strat, outcome)

		if !*noHTML {
			reportFile := defaultReportPath(strat, *htmlOutput)
			if err := exportSweepHTML(strat, outcome, reportFile, *capital); err != nil {
				log.Printf("Warning: Failed to save HTML report: %v", err)
			} else {
				fmt.Printf("\n✨ Interactive Grid Search Chart saved to: %s\n\n", reportFile)
			}
		}
		return
	}

	// --- Multiple strategies: outer bounded pool across strategies, persisted. ---
	runBatchSweep(db, gdb, targets, sweepOpts, *concurrency, *force, *noHTML, *gridDBPath, *maxPerms)
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
		fmt.Printf("   %-50s  Net Profit=+$%.2f  CAGR=%.2f%%  MaxDD=%.2f%%  Calmar=%.2f  WR=%.1f%%  Trades=%d\n\n",
			outcome.BaselineRes.Label, outcome.BaselineRes.Report.NetProfit, outcome.BaselineRes.Report.CAGR*100, outcome.BaselineRes.Report.MaxDrawdownPct*100,
			outcome.BaselineRes.Report.CalmarRatio, outcome.BaselineRes.Report.WinRate*100, outcome.BaselineRes.Report.TotalTrades)
	}

	fmt.Printf("⭐ TOP %d BY CALMAR RATIO (Risk-Adjusted):\n", len(outcome.TopCalmar))
	for i, r := range outcome.TopCalmar {
		comp := ""
		if outcome.BaselineRes != nil && outcome.BaselineRes.Report.CalmarRatio > 0 {
			diff := (r.Report.CalmarRatio - outcome.BaselineRes.Report.CalmarRatio) / outcome.BaselineRes.Report.CalmarRatio * 100.0
			comp = fmt.Sprintf(" (%+.0f%% vs base)", diff)
		}
		fmt.Printf("  #%d  %-50s  CAGR=%.2f%%  DD=%.2f%%  Calmar=%.2f  WR=%.1f%%  Trades=%d%s\n",
			i+1, r.Label, r.Report.CAGR*100, r.Report.MaxDrawdownPct*100, r.Report.CalmarRatio, r.Report.WinRate*100, r.Report.TotalTrades, comp)
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
		fmt.Printf("  #%d  %-50s  CAGR=%.2f%%  DD=%.2f%%  DDdays=%d  Score=%.4f  Trades=%d%s\n",
			i+1, r.Label, r.Report.CAGR*100, r.Report.MaxDrawdownPct*100, r.Report.MaxDrawdownDuration, score, r.Report.TotalTrades, comp)
	}

	fmt.Printf("\n💰 TOP %d BY NET PROFIT:\n", len(outcome.TopProfit))
	for i, r := range outcome.TopProfit {
		comp := ""
		if outcome.BaselineRes != nil && outcome.BaselineRes.Report.NetProfit > 0 {
			diff := (r.Report.NetProfit - outcome.BaselineRes.Report.NetProfit) / outcome.BaselineRes.Report.NetProfit * 100.0
			comp = fmt.Sprintf(" (%+.0f%% vs base)", diff)
		}
		fmt.Printf("  #%d  %-50s  Profit=+$%.2f  CAGR=%.2f%%%s\n", i+1, r.Label, r.Report.NetProfit, r.Report.CAGR*100, comp)
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

// buildSignals generates signals for a single grid-search task using consecutive-streak detection.
func buildSignals(signalBars, tradeBars []models.Bar, consecutiveDays int, direction, regime string, tpPct, slPct float64, holdDays int, tradeSym string) []models.Signal {
	tradeByDate := make(map[string]models.Bar, len(tradeBars))
	for _, b := range tradeBars {
		tradeByDate[b.Date] = b
	}
	isLong := direction != "rally"
	var signals []models.Signal

	for i := consecutiveDays; i < len(signalBars); i++ {
		voo := signalBars[i]
		var detected bool
		if isLong {
			detected = true
			for s := 0; s < consecutiveDays; s++ {
				if signalBars[i-s].Close >= signalBars[i-s-1].Close {
					detected = false
					break
				}
			}
		} else {
			detected = true
			for s := 0; s < consecutiveDays; s++ {
				if signalBars[i-s].Close <= signalBars[i-s-1].Close {
					detected = false
					break
				}
			}
		}
		if !detected {
			continue
		}
		// Regime gate
		switch {
		case strings.HasSuffix(regime, "<SMA200"):
			if voo.SMA200 > 0 && voo.Close >= voo.SMA200 {
				continue
			}
		case strings.HasSuffix(regime, "<SMA50"):
			if voo.SMA50 > 0 && voo.Close >= voo.SMA50 {
				continue
			}
		case strings.HasSuffix(regime, ">=SMA200"):
			if voo.SMA200 > 0 && voo.Close < voo.SMA200 {
				continue
			}
		case strings.HasSuffix(regime, ">=SMA50"):
			if voo.SMA50 > 0 && voo.Close < voo.SMA50 {
				continue
			}
		}
		tradeBar, ok := tradeByDate[voo.Date]
		if !ok || tradeBar.Close <= 0 {
			continue
		}
		entryPrice := tradeBar.Close
		// isLong only selects the streak type (decline vs rally in the signal
		// symbol). The trade itself is always a long buy of tradeSym (the
		// simulator has no short side; inverse ETFs are bought long), so
		// direction, TP and SL are always long-style.
		dir := "LONG"
		sig := models.Signal{
			Symbol:           tradeSym,
			Date:             voo.Date,
			Open:             tradeBar.Open,
			High:             tradeBar.High,
			Low:              tradeBar.Low,
			Close:            entryPrice,
			Volume:           tradeBar.Volume,
			Entry:            1,
			Direction:        dir,
			OrderType:        "limit",
			BuyLimit:         entryPrice,
			Regime:           regime,
			HoldDaysOverride: holdDays,
			AssetClass:       "equity",
			StrategyID:       tradeSym + "-opt",
			Priority:         0,
		}
		if tpPct > 0 {
			sig.TakeProfit = entryPrice * (1.0 + tpPct)
		}
		if slPct > 0 {
			sig.StopLoss = entryPrice * (1.0 - slPct)
		}
		signals = append(signals, sig)
	}
	return signals
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
