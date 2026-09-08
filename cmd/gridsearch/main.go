// cmd/gridsearch — Parallel parameter sweep using the unified engine.
//
// Usage:
//
//	# Bear market optimization (inverse ETFs):
//	go run cmd/gridsearch/main.go -mode bear -alloc 0.65
//
//	# Bull market multi-asset sweep:
//	go run cmd/gridsearch/main.go -mode bull -alloc 0.65
package main

import (
	"flag"
	"fmt"
	"log"
	"sort"
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
	Label  string
	Report models.PerformanceReport
	Trades []models.Trade
	Curve  []models.DailyEquityPoint
}

func main() {
	dbPath     := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	mode       := flag.String("mode", "bear", "Sweep mode: 'bear' or 'bull'")
	signalSym  := flag.String("signal", "VOO", "Signal generation symbol")
	capital    := flag.Float64("capital", 100000.0, "Starting cash ($)")
	allocPct   := flag.Float64("alloc", 0.65, "Allocation percentage (0.65 = 65%)")
	cashYield  := flag.Float64("yield", 0.045, "Cash yield on idle reserves")
	minTrades  := flag.Int("min-trades", 10, "Minimum trade count filter")
	topN       := flag.Int("top", 10, "Top N results to display")
	htmlOutput := flag.String("html", "reports/gridsearch_results.html", "Path to export HTML comparison report")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// Fetch VOO signal bars (with SMA200 for regime gate).
	barMap, _, err := storage.FetchBars(db, "backtest_start", []string{*signalSym}, "", "")
	if err != nil {
		log.Fatalf("Failed to fetch %s bars: %v", *signalSym, err)
	}
	signalBars := barMap[*signalSym]

	// Configure sweep parameters based on mode.
	var symbols []string
	var direction string
	var signalDaysList, holdDaysList []int
	var tpList, slList []float64
	var regimeFilters []string

	if *mode == "bear" {
		symbols       = []string{"SPXU", "SQQQ", "SOXS"}
		direction     = "rally"
		signalDaysList = []int{2, 3, 4, 5}
		holdDaysList  = []int{1, 2, 3, 4, 5, 6, 8, 10, 12, 15}
		tpList        = []float64{0.0, 0.03, 0.04, 0.05, 0.06, 0.08, 0.10, 0.15, 0.20}
		slList        = []float64{0.0, 0.03, 0.05, 0.07, 0.10, 0.15}
		regimeFilters = []string{"VOO<SMA200", "VOO<SMA50", "All Regimes"}
	} else {
		symbols       = []string{"TECL", "UPRO", "TQQQ", "SOXL", "FAS", "UDOW"}
		direction     = "drop"
		signalDaysList = []int{2, 3, 4}
		holdDaysList  = []int{2, 4, 6, 8, 10, 12, 14}
		tpList        = []float64{0.03, 0.05, 0.07, 0.10}
		slList        = []float64{0.0, 0.10, 0.15, 0.20}
		regimeFilters = []string{"All Regimes", "VOO>=SMA200"}
	}

	// Fetch trade bars for all target symbols.
	tradeBarsMap := make(map[string][]models.Bar, len(symbols))
	for _, sym := range symbols {
		tBarMap, _, err := storage.FetchBars(db, "backtest_start", []string{sym}, "", "")
		bars := tBarMap[sym]
		if err == nil && len(bars) > 0 {
			tradeBarsMap[sym] = bars
		}
	}

	// Build sorted dates across all bars.
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

	fmt.Printf("\n=======================================================================================================================\n")
	fmt.Printf("⚡ PARALLEL GRID SEARCH (Mode: %s | %.0f%% Alloc + %.1f%% Yield)\n", *mode, *allocPct*100, *cashYield*100)
	fmt.Printf("=======================================================================================================================\n\n")

	start := time.Now()

	type task struct {
		sym        string
		sigDays    int
		hold       int
		tp         float64
		sl         float64
		regime     string
		tradeBars  []models.Bar
	}

	tasks := make(chan task, 5000)
	var results []gridResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Spin up 16 worker goroutines.
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tasks {
				barsBySymbol := map[string][]models.Bar{
					*signalSym: signalBars,
					t.sym:      t.tradeBars,
				}

				cfg := strategy.StrategyConfig{
					AllocationPct:   *allocPct,
					TakeProfitPct:   t.tp,
					StopLossPct:     t.sl,
					HoldingWindow:   t.hold,
					PositionCap:     1,
					CashYieldAnnual: *cashYield,
				}

				sigs := buildSignals(signalBars, t.tradeBars, t.sigDays, direction, t.regime, t.tp, t.sl, t.hold, t.sym)
				if len(sigs) < *minTrades {
					return
				}

				sim := simulator.NewPortfolioSimulator(cfg, *capital)
				report, trades, curve := sim.Run(sigs, barsBySymbol, sortedDates)

				if report.TotalTrades < *minTrades {
					return
				}

				label := fmt.Sprintf("%s/%dd/%dd/+%.0f%%-%.0f%%/%s", t.sym, t.sigDays, t.hold, t.tp*100, t.sl*100, t.regime)
				mu.Lock()
				results = append(results, gridResult{Label: label, Report: report, Trades: trades, Curve: curve})
				mu.Unlock()
			}
		}()
	}

	// Enqueue all permutations.
	for _, sym := range symbols {
		tBars, ok := tradeBarsMap[sym]
		if !ok {
			continue
		}
		for _, sigDays := range signalDaysList {
			for _, regime := range regimeFilters {
				for _, hold := range holdDaysList {
					for _, tp := range tpList {
						for _, sl := range slList {
							tasks <- task{sym: sym, sigDays: sigDays, hold: hold, tp: tp, sl: sl, regime: regime, tradeBars: tBars}
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

	// Rank by Calmar.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Report.CalmarRatio > results[j].Report.CalmarRatio
	})
	topCalmar := results
	if len(topCalmar) > *topN {
		topCalmar = topCalmar[:*topN]
	}

	fmt.Printf("⭐ TOP %d BY CALMAR RATIO:\n", len(topCalmar))
	for i, r := range topCalmar {
		fmt.Printf("  #%d  %-50s  CAGR=%.2f%%  DD=%.2f%%  Calmar=%.2f  WR=%.1f%%  Trades=%d\n",
			i+1, r.Label, r.Report.CAGR, r.Report.MaxDrawdownPct, r.Report.CalmarRatio, r.Report.WinRate*100, r.Report.TotalTrades)
	}

	// Rank by Net Profit.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Report.NetProfit > results[j].Report.NetProfit
	})
	topProfit := results
	if len(topProfit) > *topN {
		topProfit = topProfit[:*topN]
	}
	fmt.Printf("\n💰 TOP %d BY NET PROFIT:\n", len(topProfit))
	for i, r := range topProfit {
		fmt.Printf("  #%d  %-50s  Profit=+$%.2f  CAGR=%.2f%%\n", i+1, r.Label, r.Report.NetProfit, r.Report.CAGR)
	}

	// Export HTML.
	if *htmlOutput != "" {
		multiResults := make([]charting.MultiResult, len(topCalmar))
		for i, r := range topCalmar {
			multiResults[i] = charting.MultiResult{Label: r.Label, Report: r.Report, DailyCurve: r.Curve}
		}
		view := charting.FromMultiReports(
			fmt.Sprintf("Grid Search Optimization Matrix (%s Mode)", *mode),
			fmt.Sprintf("Parallel parameter sweep — top %d by Calmar Ratio", len(topCalmar)),
			multiResults, signalBars, *capital,
		)
		if err := charting.GenerateHTML(*htmlOutput, view); err != nil {
			log.Printf("Warning: Failed to save HTML report: %v", err)
		} else {
			fmt.Printf("\n✨ Interactive Grid Search Chart saved to: %s\n\n", *htmlOutput)
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
		switch regime {
		case "VOO<SMA200":
			if voo.SMA200 > 0 && voo.Close >= voo.SMA200 {
				continue
			}
		case "VOO<SMA50":
			if voo.SMA50 > 0 && voo.Close >= voo.SMA50 {
				continue
			}
		case "VOO>=SMA200":
			if voo.SMA200 > 0 && voo.Close < voo.SMA200 {
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
			HoldDaysOverride: holdDays,
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
