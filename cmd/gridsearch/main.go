// cmd/gridsearch — Parallel parameter sweep assessing baked-in strategy parameters.
//
// Usage:
//
//	# Assess and optimize gld_decline:
//	go run cmd/gridsearch/main.go gld_decline
//	go run cmd/gridsearch/main.go -strategy gld_decline
//
//	# Assess and optimize voo_tecl_combo:
//	go run cmd/gridsearch/main.go voo_tecl_combo
//
//	# List all optimizable strategies:
//	go run cmd/gridsearch/main.go -list
package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/charting"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
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
	stratFlag := flag.String("strategy", "", "Strategy ID to assess and optimize (e.g. 'gld_decline', 'voo_tecl_combo')")
	stratShort := flag.String("strat", "", "Alias for -strategy")
	modeFlag := flag.String("mode", "", "Legacy compatibility alias for -strategy")
	listFlag := flag.Bool("list", false, "List registered strategies with baked-in parameters")
	signalSym := flag.String("signal", "", "Signal generation symbol override")
	customTradeSym := flag.String("symbol", "", "Specific trade symbol override")
	capital := flag.Float64("capital", 100000.0, "Starting cash ($)")
	allocPct := flag.Float64("alloc", 0.65, "Allocation percentage override (e.g. 0.65 = 65%)")
	cashYield := flag.Float64("yield", 0.045, "Cash yield on idle reserves (4.5% = 0.045)")
	minTrades := flag.Int("min-trades", 5, "Minimum trade count filter")
	topN := flag.Int("top", 10, "Top N results to display")
	htmlOutput := flag.String("html", "", "Path to export HTML comparison report (defaults to reports/<strategy>_gridsearch.html)")
	flag.Parse()

	// Track which flags were explicitly set by the user
	userPassedFlags := make(map[string]bool)
	flag.Visit(func(f *flag.Flag) {
		userPassedFlags[f.Name] = true
	})

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

	// Resolve strategy ID from flag, positional argument, or legacy mode
	stratID := strings.TrimSpace(*stratFlag)
	if stratID == "" {
		stratID = strings.TrimSpace(*stratShort)
	}
	if stratID == "" && len(flag.Args()) > 0 {
		stratID = strings.TrimSpace(flag.Args()[0])
	}
	if stratID == "" && *modeFlag != "" {
		switch strings.ToLower(*modeFlag) {
		case "gld", "gld_decline", "gld-decline":
			stratID = "gld_decline"
		case "bull", "voo", "voo_tecl_combo":
			stratID = "voo-tecl-combo"
		case "bear":
			// Bear market inverse search
			stratID = "voo-tecl-combo"
		default:
			stratID = *modeFlag
		}
	}

	if stratID == "" {
		fmt.Println()
		fmt.Println("⚠️  No strategy specified! Please specify a strategy to optimize.")
		fmt.Println("Usage:   go run cmd/gridsearch/main.go <strategy_id>")
		fmt.Println("Example: go run cmd/gridsearch/main.go gld_decline")
		fmt.Println()
		fmt.Println("Run with -list to view all available strategies:")
		fmt.Println("         go run cmd/gridsearch/main.go -list")
		fmt.Println()
		os.Exit(1)
	}

	strat, found := strategy.Get(stratID)
	if !found {
		log.Fatalf("Unknown strategy '%s'. Run with -list to see available strategies.", stratID)
	}

	// Assess the baked-in strategy parameters
	paramSpace := strategy.AssessParameterSpace(strat)

	// Apply user overrides if explicitly provided
	if userPassedFlags["alloc"] {
		paramSpace.Allocations = []float64{*allocPct}
	}
	if userPassedFlags["yield"] {
		paramSpace.CashYield = *cashYield
	}
	if userPassedFlags["symbol"] && *customTradeSym != "" {
		paramSpace.Symbols = []string{*customTradeSym}
	}
	if userPassedFlags["signal"] && *signalSym != "" {
		paramSpace.SignalSymbol = *signalSym
	}

	// Default HTML report path based on strategy ID
	cleanID := strings.ReplaceAll(strat.ID(), "-", "_")
	reportFile := fmt.Sprintf("reports/%s_gridsearch.html", cleanID)
	if *htmlOutput != "" {
		reportFile = *htmlOutput
	}

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// Fetch signal bars (with SMA200/SMA50 for regime gating)
	barMap, _, err := storage.FetchBars(db, "backtest_start", []string{paramSpace.SignalSymbol}, "", "")
	if err != nil {
		log.Fatalf("Failed to fetch %s bars: %v", paramSpace.SignalSymbol, err)
	}
	signalBars := barMap[paramSpace.SignalSymbol]
	if len(signalBars) == 0 {
		log.Fatalf("No price bars found in DB for signal symbol %s", paramSpace.SignalSymbol)
	}

	// Fetch trade bars for all target symbols
	tradeBarsMap := make(map[string][]models.Bar, len(paramSpace.Symbols))
	for _, sym := range paramSpace.Symbols {
		tBarMap, _, err := storage.FetchBars(db, "backtest_start", []string{sym}, "", "")
		bars := tBarMap[sym]
		if err == nil && len(bars) > 0 {
			tradeBarsMap[sym] = bars
		}
	}

	// Build unified sorted date list
	dateSet := make(map[string]struct{})
	for _, b := range signalBars {
		dateSet[b.Date] = struct{}{}
	}
	for _, bars := range tradeBarsMap {
		for _, b := range bars {
			dateSet[b.Date] = struct{}{}
		}
	}
	sortedDates := make([]string, 0, len(dateSet))
	for d := range dateSet {
		sortedDates = append(sortedDates, d)
	}
	sort.Strings(sortedDates)

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

	start := time.Now()

	type task struct {
		sym        string
		sigDays    int
		hold       int
		tp         float64
		sl         float64
		regime     string
		alloc      float64
		tradeBars  []models.Bar
		isBaseline bool
	}

	tasks := make(chan task, 5000)
	var results []gridResult
	var baselineRes *gridResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	// 16 parallel worker goroutines
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tasks {
				barsBySymbol := map[string][]models.Bar{
					paramSpace.SignalSymbol: signalBars,
					t.sym:                   t.tradeBars,
				}

				cfg := strategy.StrategyConfig{
					AllocationPct:   t.alloc,
					TakeProfitPct:   t.tp,
					StopLossPct:     t.sl,
					HoldingWindow:   t.hold,
					PositionCap:     1,
					CashYieldAnnual: paramSpace.CashYield,
				}

				sigs := buildSignals(signalBars, t.tradeBars, t.sigDays, paramSpace.Direction, t.regime, t.tp, t.sl, t.hold, t.sym)
				if len(sigs) < *minTrades {
					continue
				}

				sim := simulator.NewPortfolioSimulator(cfg, *capital)
				report, trades, curve := sim.Run(sigs, barsBySymbol, sortedDates)

				if report.TotalTrades < *minTrades {
					continue
				}

				label := fmt.Sprintf("%s/%dd/%dd/+%.0f%%-%.0f%%/%s", t.sym, t.sigDays, t.hold, t.tp*100, t.sl*100, t.regime)
				res := gridResult{
					Label:      label,
					Report:     report,
					Trades:     trades,
					Curve:      curve,
					IsBaseline: t.isBaseline,
				}

				mu.Lock()
				results = append(results, res)
				if t.isBaseline {
					bCopy := res
					baselineRes = &bCopy
				}
				mu.Unlock()
			}
		}()
	}

	// Enqueue all permutations
	for _, sym := range paramSpace.Symbols {
		tBars, ok := tradeBarsMap[sym]
		if !ok {
			continue
		}
		for _, sigDays := range paramSpace.SignalDays {
			for _, regime := range paramSpace.Regimes {
				for _, hold := range paramSpace.HoldDays {
					for _, tp := range paramSpace.TakeProfits {
						for _, sl := range paramSpace.StopLosses {
							for _, alloc := range paramSpace.Allocations {
								isBase := (sigDays == paramSpace.Baseline.SignalDays &&
									hold == paramSpace.Baseline.HoldDays &&
									math.Abs(tp-paramSpace.Baseline.TakeProfit) < 1e-4 &&
									math.Abs(sl-paramSpace.Baseline.StopLoss) < 1e-4 &&
									regime == paramSpace.Baseline.Regime)

								tasks <- task{
									sym:        sym,
									sigDays:    sigDays,
									hold:       hold,
									tp:         tp,
									sl:         sl,
									regime:     regime,
									alloc:      alloc,
									tradeBars:  tBars,
									isBaseline: isBase,
								}
							}
						}
					}
				}
			}
		}
	}
	close(tasks)
	wg.Wait()

	elapsed := time.Since(start)
	fmt.Printf("⚡ Evaluated %d valid configurations in %v\n\n", len(results), elapsed)

	if len(results) == 0 {
		fmt.Println("No configurations met the minimum trade count filter.")
		return
	}

	// Print Baseline Performance first as benchmark
	if baselineRes != nil {
		fmt.Println("📌 BAKED-IN STRATEGY BASELINE:")
		fmt.Printf("   %-50s  Net Profit=+$%.2f  CAGR=%.2f%%  MaxDD=%.2f%%  Calmar=%.2f  WR=%.1f%%  Trades=%d\n\n",
			baselineRes.Label, baselineRes.Report.NetProfit, baselineRes.Report.CAGR*100, baselineRes.Report.MaxDrawdownPct*100,
			baselineRes.Report.CalmarRatio, baselineRes.Report.WinRate*100, baselineRes.Report.TotalTrades)
	}

	// Rank by Calmar Ratio
	sort.Slice(results, func(i, j int) bool {
		return results[i].Report.CalmarRatio > results[j].Report.CalmarRatio
	})
	topCalmar := results
	if len(topCalmar) > *topN {
		topCalmar = topCalmar[:*topN]
	}

	fmt.Printf("⭐ TOP %d BY CALMAR RATIO (Risk-Adjusted):\n", len(topCalmar))
	for i, r := range topCalmar {
		comp := ""
		if baselineRes != nil && baselineRes.Report.CalmarRatio > 0 {
			diff := (r.Report.CalmarRatio - baselineRes.Report.CalmarRatio) / baselineRes.Report.CalmarRatio * 100.0
			comp = fmt.Sprintf(" (%+.0f%% vs base)", diff)
		}
		fmt.Printf("  #%d  %-50s  CAGR=%.2f%%  DD=%.2f%%  Calmar=%.2f  WR=%.1f%%  Trades=%d%s\n",
			i+1, r.Label, r.Report.CAGR*100, r.Report.MaxDrawdownPct*100, r.Report.CalmarRatio, r.Report.WinRate*100, r.Report.TotalTrades, comp)
	}

	// Rank by Net Profit
	sort.Slice(results, func(i, j int) bool {
		return results[i].Report.NetProfit > results[j].Report.NetProfit
	})
	topProfit := results
	if len(topProfit) > *topN {
		topProfit = topProfit[:*topN]
	}
	fmt.Printf("\n💰 TOP %d BY NET PROFIT:\n", len(topProfit))
	for i, r := range topProfit {
		comp := ""
		if baselineRes != nil && baselineRes.Report.NetProfit > 0 {
			diff := (r.Report.NetProfit - baselineRes.Report.NetProfit) / baselineRes.Report.NetProfit * 100.0
			comp = fmt.Sprintf(" (%+.0f%% vs base)", diff)
		}
		fmt.Printf("  #%d  %-50s  Profit=+$%.2f  CAGR=%.2f%%%s\n", i+1, r.Label, r.Report.NetProfit, r.Report.CAGR*100, comp)
	}

	// Export HTML Comparison Report
	if reportFile != "" {
		var multiResults []charting.MultiResult
		if baselineRes != nil {
			multiResults = append(multiResults, charting.MultiResult{
				Label:      fmt.Sprintf("⭐ [BASELINE] %s", baselineRes.Label),
				Report:     baselineRes.Report,
				DailyCurve: baselineRes.Curve,
			})
		}
		for _, r := range topCalmar {
			multiResults = append(multiResults, charting.MultiResult{
				Label:      r.Label,
				Report:     r.Report,
				DailyCurve: r.Curve,
			})
		}

		view := charting.FromMultiReports(
			fmt.Sprintf("Grid Search Optimization: %s", strat.Name()),
			fmt.Sprintf("Parameter sweep centered on baked-in defaults — top %d by Calmar Ratio", len(topCalmar)),
			multiResults, signalBars, *capital,
		)
		if err := charting.GenerateHTML(reportFile, view); err != nil {
			log.Printf("Warning: Failed to save HTML report: %v", err)
		} else {
			fmt.Printf("\n✨ Interactive Grid Search Chart saved to: %s\n", reportFile)
			baseName := filepath.Base(reportFile)
			if filepath.Dir(reportFile) != "." && baseName != "" {
				_ = charting.GenerateHTML(baseName, view)
				fmt.Printf("✨ Root copy also generated: %s\n\n", baseName)
			} else {
				fmt.Println()
			}
		}
	}
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
