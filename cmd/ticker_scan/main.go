// cmd/ticker_scan — Applies the MARA "Precision 200-SMA Bounce" decision tree
// (volatility coil + SMA200 re-test) to every candidate symbol in the database,
// to find which tickers the pattern generalizes to beyond MARA.
//
// Usage:
//
//	go run cmd/ticker_scan/main.go
//	go run cmd/ticker_scan/main.go -symbols SOXL,TQQQ,COIN
package main

import (
	"flag"
	"fmt"
	"log"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	_ "modernc.org/sqlite"
)

// excludedSymbols are already covered by dedicated baked-in strategies (or are
// low-volatility rates/benchmark instruments where the tree-bounce pattern doesn't
// apply) and are skipped from the full-universe scan.
var excludedSymbols = map[string]bool{
	"VOO": true, "GLD": true, "TECL": true, "SPXU": true, "UTEN": true,
}

// allUniverseSymbols queries every distinct symbol available in the market database.
func allUniverseSymbols(dbPath string) ([]string, error) {
	db, err := storage.OpenSQLite(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var symbols []string
	if err := db.Select(&symbols, `SELECT DISTINCT symbol FROM backtest_start ORDER BY symbol ASC;`); err != nil {
		return nil, err
	}

	var out []string
	for _, s := range symbols {
		if !excludedSymbols[strings.ToUpper(s)] {
			out = append(out, s)
		}
	}
	return out, nil
}

type scanResult struct {
	Symbol  string
	Report  models.PerformanceReport
	Score   float64
	NumBars int
}

func resilienceScore(r models.PerformanceReport) float64 {
	durationYears := float64(r.MaxDrawdownDuration) / 365.0
	return r.CAGR / ((1.0 + r.MaxDrawdownPct) * (1.0 + durationYears))
}

func main() {
	dbPath := flag.String("db", "data/market_history.db", "Path to SQLite database")
	symbolsFlag := flag.String("symbols", "", "Comma-separated symbol list to scan (defaults to every symbol in the database)")
	capital := flag.Float64("capital", 100000.0, "Starting cash ($)")
	alloc := flag.Float64("alloc", 0.65, "Allocation percentage per position")
	tp := flag.Float64("tp", 0.05, "Take-profit percentage")
	sl := flag.Float64("sl", 0.08, "Stop-loss percentage")
	hold := flag.Int("hold", 1, "Holding window (days)")
	cashYield := flag.Float64("yield", 0.045, "Idle cash yield (annualized)")
	minTrades := flag.Int("min-trades", 10, "Minimum trade count filter")
	optimizeTop := flag.Int("optimize-top", 3, "Sweep TP/SL/hold for this many top-ranked candidates (0 disables)")
	concurrency := flag.Int("concurrency", runtime.NumCPU(), "Max concurrent symbol workers. Defaults to all CPU cores.")
	flag.Parse()

	var candidates []string
	if strings.TrimSpace(*symbolsFlag) != "" {
		for _, s := range strings.Split(*symbolsFlag, ",") {
			s = strings.TrimSpace(strings.ToUpper(s))
			if s != "" {
				candidates = append(candidates, s)
			}
		}
	} else {
		var err error
		candidates, err = allUniverseSymbols(*dbPath)
		if err != nil {
			log.Fatalf("Failed to enumerate symbols: %v", err)
		}
	}

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	fmt.Println()
	fmt.Println("=======================================================================================================================")
	fmt.Println("🔎 TICKER SCAN — Precision 200-SMA Bounce Decision Tree across candidate symbols")
	fmt.Println("=======================================================================================================================")
	fmt.Printf("Params: TP=+%.1f%% SL=-%.1f%% Hold=%dd Alloc=%.0f%% Yield=%.1f%% MinTrades=%d\n\n",
		*tp*100, *sl*100, *hold, *alloc*100, *cashYield*100, *minTrades)

	// Bounded worker pool across candidate symbols (each does its own DB fetch +
	// tree-signal generation + simulation — independent, CPU/IO-bound work).
	workers := *concurrency
	if workers < 1 {
		workers = 1
	}
	if workers > len(candidates) {
		workers = len(candidates)
	}
	fmt.Printf("Scanning %d candidates with %d workers...\n\n", len(candidates), workers)

	jobs := make(chan string, len(candidates))
	for _, sym := range candidates {
		jobs <- sym
	}
	close(jobs)

	var mu sync.Mutex
	var results []scanResult
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sym := range jobs {
				barMap, _, err := storage.FetchBars(db, "backtest_start", []string{sym}, "", "")
				if err != nil {
					mu.Lock()
					fmt.Printf("  ⚠️  %-6s  fetch error: %v\n", sym, err)
					mu.Unlock()
					continue
				}
				bars := barMap[sym]
				if len(bars) < 201 {
					continue
				}

				sigs := strategy.TreeBounceSignals(sym, bars, *tp, *sl, *hold)
				if len(sigs) < *minTrades {
					continue
				}

				dates := make([]string, len(bars))
				for i, b := range bars {
					dates[i] = b.Date
				}

				cfg := strategy.StrategyConfig{
					AllocationPct:      *alloc,
					PositionCap:        1,
					CashYieldAnnual:    *cashYield,
					SlippagePct:        0.0005,
					CommissionPerShare: 0.0001,
				}
				sim := simulator.NewPortfolioSimulator(cfg, *capital)
				report, _, _ := sim.Run(sigs, map[string][]models.Bar{sym: bars}, dates)

				if report.TotalTrades < *minTrades {
					continue
				}

				mu.Lock()
				results = append(results, scanResult{
					Symbol:  sym,
					Report:  report,
					Score:   resilienceScore(report),
					NumBars: len(bars),
				})
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if len(results) == 0 {
		fmt.Println("\nNo candidates produced enough trades to evaluate.")
		return
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	fmt.Println()
	fmt.Println("🛡️  RANKED BY RESILIENCE (High CAGR, Shallow & Brief Drawdowns):")
	for i, r := range results {
		fmt.Printf("  #%2d  %-6s  CAGR=%7.2f%%  MaxDD=%6.2f%%  DDdays=%4d  Calmar=%6.2f  WR=%5.1f%%  Trades=%3d  NetProfit=%+12.2f  Score=%.4f\n",
			i+1, r.Symbol, r.Report.CAGR*100, r.Report.MaxDrawdownPct*100, r.Report.MaxDrawdownDuration,
			r.Report.CalmarRatio, r.Report.WinRate*100, r.Report.TotalTrades, r.Report.NetProfit, r.Score)
	}
	fmt.Println()

	if *optimizeTop > 0 {
		n := *optimizeTop
		if n > len(results) {
			n = len(results)
		}
		tpGrid := []float64{0.03, 0.04, 0.05, 0.06, 0.07, 0.08, 0.10, 0.12, 0.15}
		slGrid := []float64{0.03, 0.04, 0.05, 0.06, 0.07, 0.08, 0.10}
		holdGrid := []int{1, 2, 3, 4, 5, 6, 8, 10}

		for _, r := range results[:n] {
			barMap, _, err := storage.FetchBars(db, "backtest_start", []string{r.Symbol}, "", "")
			if err != nil {
				continue
			}
			bars := barMap[r.Symbol]
			dates := make([]string, len(bars))
			for i, b := range bars {
				dates[i] = b.Date
			}

			type swept struct {
				tp, sl float64
				hold   int
				report models.PerformanceReport
				score  float64
			}
			var sweepResults []swept
			for _, tpv := range tpGrid {
				for _, slv := range slGrid {
					for _, hv := range holdGrid {
						sigs := strategy.TreeBounceSignals(r.Symbol, bars, tpv, slv, hv)
						if len(sigs) < *minTrades {
							continue
						}
						cfg := strategy.StrategyConfig{
							AllocationPct:      *alloc,
							PositionCap:        1,
							CashYieldAnnual:    *cashYield,
							SlippagePct:        0.0005,
							CommissionPerShare: 0.0001,
						}
						sim := simulator.NewPortfolioSimulator(cfg, *capital)
						report, _, _ := sim.Run(sigs, map[string][]models.Bar{r.Symbol: bars}, dates)
						if report.TotalTrades < *minTrades {
							continue
						}
						sweepResults = append(sweepResults, swept{tpv, slv, hv, report, resilienceScore(report)})
					}
				}
			}
			if len(sweepResults) == 0 {
				continue
			}
			sort.Slice(sweepResults, func(i, j int) bool { return sweepResults[i].score > sweepResults[j].score })

			fmt.Printf("🔧 %s — top 5 TP/SL/Hold configs by resilience (baseline TP=5%%/SL=8%%/Hold=1d Score=%.4f):\n", r.Symbol, r.Score)
			top := sweepResults
			if len(top) > 5 {
				top = top[:5]
			}
			for i, s := range top {
				fmt.Printf("  #%d  TP+%.0f%%/SL-%.0f%%/Hold-%dd  CAGR=%.2f%%  MaxDD=%.2f%%  DDdays=%d  WR=%.1f%%  Trades=%d  Score=%.4f\n",
					i+1, s.tp*100, s.sl*100, s.hold, s.report.CAGR*100, s.report.MaxDrawdownPct*100, s.report.MaxDrawdownDuration, s.report.WinRate*100, s.report.TotalTrades, s.score)
			}
			fmt.Println()
		}
	}
}
