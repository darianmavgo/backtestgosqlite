// cmd/gridsearch — Parallel parameter sweep assessing baked-in strategy parameters.
//
// Usage:
//
//	# Assess and optimize a single strategy:
//	go run cmd/gridsearch/main.go gld_decline
//	go run cmd/gridsearch/main.go -strategy voo_tecl_combo
//
//	# Assess and optimize many strategies at once (outer concurrency across
//	# strategies, persisted to reports/gridsearch.db, skips strategies already
//	# swept unless -force):
//	go run cmd/gridsearch/main.go -strategy mara_tree,pdd_tree,gld_decline
//	go run cmd/gridsearch/main.go -strategy all
//
//	# List all optimizable strategies:
//	go run cmd/gridsearch/main.go -list
package main

import (
	"flag"
	"fmt"
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
	_ "github.com/mattn/go-sqlite3"
)

type gridResult struct {
	Label      string
	Report     models.PerformanceReport
	Trades     []models.Trade
	Curve      []models.DailyEquityPoint
	IsBaseline bool
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
	dbPath := flag.String("db", "data/market_history.db", "Path to SQLite database")
	stratFlag := flag.String("strategy", "", "Strategy ID (comma-separated list, or 'all') to assess and optimize")
	stratShort := flag.String("strat", "", "Alias for -strategy")
	modeFlag := flag.String("mode", "", "Legacy compatibility alias for -strategy")
	listFlag := flag.Bool("list", false, "List registered strategies with baked-in parameters")
	signalSym := flag.String("signal", "", "Signal generation symbol override (single-strategy mode only)")
	customTradeSym := flag.String("symbol", "", "Specific trade symbol override (single-strategy mode only)")
	capital := flag.Float64("capital", 100000.0, "Starting cash ($)")
	allocPct := flag.Float64("alloc", 0.65, "Allocation percentage override (single-strategy mode only)")
	cashYield := flag.Float64("yield", 0.045, "Cash yield on idle reserves (4.5% = 0.045)")
	minTrades := flag.Int("min-trades", 5, "Minimum trade count filter")
	topN := flag.Int("top", 10, "Top N results to display per strategy")
	htmlOutput := flag.String("html", "", "Path to export HTML comparison report (single-strategy mode; defaults to reports/<strategy>_gridsearch.html)")
	noHTML := flag.Bool("no-html", false, "Skip per-strategy HTML export (batch mode; speeds up large sweeps)")
	concurrency := flag.Int("concurrency", runtime.NumCPU(), "Single-strategy mode: worker goroutines within the sweep. Multi-strategy mode: strategies swept concurrently. Defaults to all CPU cores.")
	force := flag.Bool("force", false, "Redo strategies that already have a completed sweep in reports/gridsearch.db")
	includeDT := flag.Bool("include-dt", false, "Include dt_* (auto-generated per-ETF decision tree) strategies in -strategy all — they already have their own dedicated sweep via cmd/etf_decision_trees, so excluded by default")
	gridDBPath := flag.String("gridsearch-db", "reports/gridsearch.db", "SQLite DB for the pipeline controller (gridsearch_runs) and results (gridsearch_results) tables")
	flag.Parse()

	// Track which flags were explicitly set by the user
	userPassedFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		userPassedFlags[f.Name] = true
	})

	strategy.AutoRegisterSQLStrategies(".", *dbPath)

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
		case "bull", "voo", "voo_tecl_combo":
			stratArg = "voo-tecl-combo"
		case "bear":
			stratArg = "voo-tecl-combo"
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

	sweepOpts := sweepOptions{
		Capital:   *capital,
		MinTrades: *minTrades,
		TopN:      *topN,
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
		sweepOpts.SymbolOverride = *customTradeSym
	}
	if userPassedFlags["signal"] && *signalSym != "" {
		sweepOpts.SignalOverride = *signalSym
	}

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
	runBatchSweep(db, gdb, targets, sweepOpts, *concurrency, *force, *noHTML, *gridDBPath)
}

func defaultReportPath(strat strategy.Strategy, override string) string {
	if override != "" {
		reportFile := override
		if !filepath.IsAbs(reportFile) && !strings.HasPrefix(reportFile, "reports/") && !strings.HasPrefix(reportFile, "reports"+string(filepath.Separator)) {
			reportFile = filepath.Join("reports", reportFile)
		}
		return reportFile
	}
	cleanID := strings.ReplaceAll(strat.ID(), "-", "_")
	return fmt.Sprintf("reports/%s_gridsearch.html", cleanID)
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
		dir := "LONG"
		if !isLong {
			dir = "SHORT"
		}
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
			if isLong {
				sig.TakeProfit = entryPrice * (1.0 + tpPct)
			} else {
				sig.TakeProfit = entryPrice * (1.0 - tpPct)
			}
		}
		if slPct > 0 {
			if isLong {
				sig.StopLoss = entryPrice * (1.0 - slPct)
			} else {
				sig.StopLoss = entryPrice * (1.0 + slPct)
			}
		}
		signals = append(signals, sig)
	}
	return signals
}
