// cmd/gridsearch — Thin CLI for running high-speed parallel parameter sweeps.
//
// Usage:
//
//	# Bear market optimization:
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
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/charting"
	"github.com/darianmavgo/backtestgosqlite/internal/dipsim"
	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	mode := flag.String("mode", "bear", "Sweep mode: 'bear' (inverse ETFs) or 'bull' (leveraged bull ETFs)")
	signalSymbol := flag.String("signal", "VOO", "Signal generation symbol")
	capital := flag.Float64("capital", 100000.0, "Starting cash ($)")
	allocPct := flag.Float64("alloc", 0.65, "Allocation percentage (0.65 = 65%)")
	cashYield := flag.Float64("yield", 0.045, "Cash yield on idle reserves (4.5%)")
	minTrades := flag.Int("min-trades", 10, "Minimum trade count filter")
	topN := flag.Int("top", 10, "Top N results to display")
	htmlOutput := flag.String("html", "reports/gridsearch_results.html", "Path to export HTML comparison report")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// 1. Fetch signal bars
	vooBars, err := storage.FetchDipBars(db, *signalSymbol)
	if err != nil {
		log.Fatalf("Failed to fetch %s bars: %v", *signalSymbol, err)
	}
	sigBars, err := storage.FetchSignalBars(db, *signalSymbol)
	if err != nil {
		log.Fatalf("Failed to fetch signal bars: %v", err)
	}

	signalBarsMap := map[string][]models.BarData{
		*signalSymbol: vooBars,
	}

	// 2. Configure Ranges based on mode
	var symbols []string
	var direction string
	var rallyDays []int
	var holdDays []int
	var tpList []float64
	var slList []float64
	var regimeFilters []string

	if *mode == "bear" {
		symbols = []string{"SPXU", "SQQQ", "SOXS"}
		direction = "rally"
		rallyDays = []int{2, 3, 4, 5}
		holdDays = []int{1, 2, 3, 4, 5, 6, 8, 10, 12, 15}
		tpList = []float64{0.0, 0.03, 0.04, 0.05, 0.06, 0.08, 0.10, 0.15, 0.20}
		slList = []float64{0.0, 0.03, 0.05, 0.07, 0.10, 0.15}
		regimeFilters = []string{"VOO < SMA200", "VOO < SMA50", "All Regimes"}
	} else {
		symbols = []string{"TECL", "UPRO", "TQQQ", "SOXL", "FAS", "UDOW"}
		direction = "drop"
		rallyDays = []int{2, 3, 4}
		holdDays = []int{2, 4, 6, 8, 10, 12, 14}
		tpList = []float64{0.03, 0.05, 0.07, 0.10}
		slList = []float64{0.0, 0.10, 0.15, 0.20}
		regimeFilters = []string{"All Regimes", "VOO >= SMA200"}
	}

	// Fetch trade bars for all target symbols
	tradeBarsMap := make(map[string][]models.BarData, len(symbols))
	for _, sym := range symbols {
		bars, err := storage.FetchDipBars(db, sym)
		if err == nil && len(bars) > 0 {
			tradeBarsMap[sym] = bars
		}
	}

	ranges := dipsim.GridRanges{
		Symbols:        symbols,
		SignalDays:     rallyDays,
		HoldDays:       holdDays,
		TakeProfitPcts: tpList,
		StopLossPcts:   slList,
		RegimeFilters:  regimeFilters,
		AllocationPcts: []float64{*allocPct},
	}

	baseCfg := dipsim.DipSimConfig{
		SignalSymbol:    *signalSymbol,
		SignalDirection: direction,
		InitialCapital:  *capital,
		CashYieldAnnual: *cashYield,
		AllocationPct:   *allocPct,
	}

	fmt.Printf("\n=======================================================================================================================\n")
	fmt.Printf("⚡ RUNNING PARALLEL GRID SEARCH OPTIMIZER (Mode: %s | Sizing: %.0f%% Alloc + %.1f%% Yield)\n", *mode, *allocPct*100, *cashYield*100)
	fmt.Printf("=======================================================================================================================\n\n")

	start := time.Now()
	results := dipsim.GridSearch(baseCfg, ranges, signalBarsMap, tradeBarsMap, sigBars, *minTrades)
	elapsed := time.Since(start)

	fmt.Printf("⚡ Evaluated and collected %d valid configurations in %v\n", len(results), elapsed)

	if len(results) == 0 {
		fmt.Println("No configurations met the minimum trade count filter.")
		return
	}

	// 1. Rank by Calmar Ratio
	sort.Slice(results, func(i, j int) bool {
		return results[i].CalmarRatio > results[j].CalmarRatio
	})

	topCalmar := results
	if len(topCalmar) > *topN {
		topCalmar = topCalmar[:*topN]
	}
	dipsim.PrintResultsRanked(fmt.Sprintf("⭐ TOP %d CONFIGURATIONS RANKED BY CALMAR RATIO (CAGR / MAX DD):", len(topCalmar)), topCalmar)

	// 2. Rank by Net Profit
	sort.Slice(results, func(i, j int) bool {
		return results[i].NetProfit > results[j].NetProfit
	})

	topProfit := results
	if len(topProfit) > *topN {
		topProfit = topProfit[:*topN]
	}
	dipsim.PrintResultsRanked(fmt.Sprintf("\n💰 TOP %d CONFIGURATIONS RANKED BY 5-YEAR NET PROFIT:", len(topProfit)), topProfit)

	// 3. Export HTML Report if requested
	if *htmlOutput != "" {
		reportView := charting.FromMultiResults(
			fmt.Sprintf("Grid Search Optimization Matrix (%s Mode)", *mode),
			fmt.Sprintf("Parallel parameter sweep across %d configurations", len(results)),
			topCalmar,
			vooBars,
			*capital,
		)
		if err := charting.GenerateHTML(*htmlOutput, reportView); err != nil {
			log.Printf("Warning: Failed to save HTML report: %v", err)
		} else {
			fmt.Printf("\n✨ Interactive Grid Search Comparison Chart saved to: %s\n\n", *htmlOutput)
		}
	}
}
