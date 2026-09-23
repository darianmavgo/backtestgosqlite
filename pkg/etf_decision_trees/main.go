// cmd/etf_decision_trees — Fits a CloudForest decision tree per ETF (see
// pkg/strategy/decisiontree.go), sweeps a small TP/SL/hold grid against each
// tree's BUY predictions, and writes the best-found config per symbol to
// the reference DB's etf_dt_strategies table — which pkg/strategy/etf_decision_tree.go reads on
// startup to register one ETFDecisionTreeStrategy per qualifying ETF (ID "dt_<symbol>"),
// so every symbol becomes independently runnable via cmd/backtest / cmd/gridsearch.
//
// Usage:
//
//	go run cmd/etf_decision_trees/main.go
//	go run cmd/etf_decision_trees/main.go -list 6yr -workers 16
package etf_decision_trees

import (
	"flag"
	"fmt"
	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"log"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

type dtResult struct {
	Symbol    string
	TP, SL    float64
	Hold      int
	CAGR      float64
	MaxDD     float64
	MaxDDDays int
	Trades    int
	WinRate   float64
	Score     float64
}

func resilienceScore(r models.PerformanceReport) float64 {
	durationYears := float64(r.MaxDrawdownDuration) / 365.0
	return r.CAGR / ((1.0 + r.MaxDrawdownPct) * (1.0 + durationYears))
}

// readExistingResults loads previously-saved results from the reference DB so
// a rerun can skip re-fitting a tree + re-sweeping TP/SL/hold for symbols that
// already have a usable result — the same "don't redo finished work by default"
// as cmd/backtest/cmd/scoreboard, applied here to CloudForest fits instead of
// full backtests.
func readExistingResults(ref *sqlx.DB) map[string]dtResult {
	out := make(map[string]dtResult)
	rows, err := refdb.DTStrategies(ref)
	if err != nil {
		return out
	}
	for _, r := range rows {
		sym := strings.ToUpper(r.Symbol)
		out[sym] = dtResult{
			Symbol: sym, TP: r.TP, SL: r.SL, Hold: r.Hold, CAGR: r.CAGR, MaxDD: r.MaxDD,
			MaxDDDays: r.MaxDDDays, Trades: r.Trades, WinRate: r.WinRate, Score: r.Score,
		}
	}
	return out
}

// Config holds the settings of a run.
type Config struct {
	Db        string   // -db
	RefDb     string   // -ref-db
	List      string   // -list
	MinTrades int      // -min-trades
	Workers   int      // -workers
	Top       int      // -top
	Capital   float64  // -capital
	Alloc     float64  // -alloc
	Yield     float64  // -yield
	Force     bool     // -force
	Args      []string // positional arguments
}

// DefaultConfig returns the CLI defaults.
func DefaultConfig() Config {
	return Config{
		Db:        appenv.MarketDB(),
		RefDb:     refdb.DefaultPath,
		List:      refdb.List6Yr,
		MinTrades: 15,
		Workers:   runtime.NumCPU(),
		Top:       40,
		Capital:   100000.0,
		Alloc:     0.65,
		Yield:     0.045,
		Force:     false,
	}
}

// Main is the CLI entry point.
func Main() {

	conf := DefaultConfig()
	d := conf
	flag.StringVar(&conf.Db, "db", d.Db, "Path to SQLite database")
	flag.StringVar(&conf.RefDb, "ref-db", d.RefDb, "Reference DB holding the ETF universe and receiving etf_dt_strategies")
	flag.StringVar(&conf.List, "list", d.List, "etf_universe list to fit (all, 6yr, sweep)")
	flag.IntVar(&conf.MinTrades, "min-trades", d.MinTrades, "Minimum trade count for a config to be considered valid")
	flag.IntVar(&conf.Workers, "workers", d.Workers, "Concurrent worker count (CPU-bound: tree fit + grid sweep)")
	flag.IntVar(&conf.Top, "top", d.Top, "How many top results to print")
	flag.Float64Var(&conf.Capital, "capital", d.Capital, "Starting cash ($)")
	flag.Float64Var(&conf.Alloc, "alloc", d.Alloc, "Allocation percentage per position")
	flag.Float64Var(&conf.Yield, "yield", d.Yield, "Idle cash yield (annualized)")
	flag.BoolVar(&conf.Force, "force", d.Force, "Refit every symbol even if -out already has a usable result for it")
	flag.Parse()
	conf.Args = flag.Args()
	if err := Run(conf); err != nil {
		log.Fatal(err)
	}
}

// Run executes the command with cfg. It returns errors instead of exiting.
func Run(conf Config) error {
	ref, err := refdb.Open(conf.RefDb)
	if err != nil {
		return fmt.Errorf("Failed to open reference DB %s: %v", conf.RefDb, err)
	}
	defer ref.Close()
	allSymbols, err := refdb.Universe(ref, conf.List)
	if err != nil || len(allSymbols) == 0 {
		return fmt.Errorf("No symbols in %s etf_universe list %q (err: %v)", conf.RefDb, conf.List, err)
	}

	existing := map[string]dtResult{}
	if !conf.Force {
		existing = readExistingResults(ref)
	}
	var symbols []string
	for _, s := range allSymbols {
		if _, ok := existing[s]; !ok {
			symbols = append(symbols, s)
		}
	}

	db, err := storage.OpenSQLite(conf.Db)
	if err != nil {
		return fmt.Errorf("Failed to open DB: %v", err)
	}
	defer db.Close()

	fmt.Println()
	fmt.Println("=======================================================================================================================")
	if conf.Force {
		fmt.Printf("🌲 ETF DECISION TREE GENERATOR — -force set: refitting all %d symbols with %d workers\n", len(allSymbols), conf.Workers)
	} else {
		fmt.Printf("🌲 ETF DECISION TREE GENERATOR — %d/%d symbols already have a usable result and will be skipped; fitting %d with %d workers\n",
			len(existing), len(allSymbols), len(symbols), conf.Workers)
	}
	fmt.Println("=======================================================================================================================")

	if len(symbols) == 0 {
		fmt.Println("\n✅ Nothing to fit — every symbol already has a usable result. (Use -force to refit everything.)")
		if err := printAndWriteResults(existing, ref, conf.Top); err != nil {
			return err
		}
		return nil
	}

	tpGrid := []float64{0.03, 0.05, 0.08, 0.12}
	slGrid := []float64{0.04, 0.06, 0.08}
	holdGrid := []int{1, 3, 5}

	var mu sync.Mutex
	var results []dtResult
	var fitted, skippedNoTree, skippedNoTrades int

	jobs := make(chan string, len(symbols))
	for _, s := range symbols {
		jobs <- s
	}
	close(jobs)

	start := time.Now()
	var wg sync.WaitGroup
	processed := 0

	for w := 0; w < conf.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sym := range jobs {
				barMap, _, err := storage.FetchBars(db, "backtest_start", []string{sym}, "", "")
				if err != nil {
					mu.Lock()
					processed++
					mu.Unlock()
					continue
				}
				bars := barMap[sym]

				buyDates, err := strategy.FitDecisionTreeBuyDates(bars)
				if err != nil {
					mu.Lock()
					skippedNoTree++
					processed++
					mu.Unlock()
					continue
				}

				dates := make([]string, len(bars))
				for i, b := range bars {
					dates[i] = b.Date
				}

				var best *dtResult
				for _, tp := range tpGrid {
					for _, sl := range slGrid {
						for _, hold := range holdGrid {
							sigs := strategy.BuildDecisionTreeSignals(sym, bars, buyDates, tp, sl, hold)
							if len(sigs) < conf.MinTrades {
								continue
							}
							cfg := strategy.StrategyConfig{
								AllocationPct:      conf.Alloc,
								PositionCap:        1,
								CashYieldAnnual:    conf.Yield,
								SlippagePct:        0.0005,
								CommissionPerShare: 0.0001,
							}
							sim := simulator.NewPortfolioSimulator(cfg, conf.Capital)
							report, _, _ := sim.Run(sigs, map[string][]models.Bar{sym: bars}, dates)
							if report.TotalTrades < conf.MinTrades {
								continue
							}
							score := resilienceScore(report)
							if best == nil || score > best.Score {
								best = &dtResult{
									Symbol: sym, TP: tp, SL: sl, Hold: hold,
									CAGR: report.CAGR, MaxDD: report.MaxDrawdownPct, MaxDDDays: report.MaxDrawdownDuration,
									Trades: report.TotalTrades, WinRate: report.WinRate, Score: score,
								}
							}
						}
					}
				}

				mu.Lock()
				processed++
				if best != nil {
					results = append(results, *best)
					fitted++
				} else {
					skippedNoTrades++
				}
				if processed%100 == 0 {
					fmt.Printf("   ...%d/%d symbols processed (%d fitted, %d no-tree, %d no-min-trades) [%s elapsed]\n",
						processed, len(symbols), fitted, skippedNoTree, skippedNoTrades, time.Since(start).Round(time.Second))
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	fmt.Printf("\n⚡ Done in %s: %d symbols fitted a usable tree+config, %d had too few +-5%% extreme days to fit a tree, %d fit a tree but no TP/SL/hold combo cleared %d min trades.\n",
		time.Since(start).Round(time.Second), fitted, skippedNoTree, skippedNoTrades, conf.MinTrades)

	// Merge freshly-fitted results with whatever was already valid so the output
	// CSV (and the strategy registry that reads it) still covers every symbol
	// ever successfully fitted, not just the ones fitted this run.
	merged := existing
	if merged == nil {
		merged = map[string]dtResult{}
	}
	for _, r := range results {
		merged[r.Symbol] = r
	}

	if len(merged) == 0 {
		fmt.Println("No usable decision trees found.")
		return nil
	}

	if err := printAndWriteResults(merged, ref, conf.Top); err != nil {
		return err
	}
	return nil
}

// printAndWriteResults prints the top-N ranked results and writes the full set
// to the reference DB. Shared between the normal fit-then-report path and the
// nothing-to-fit (everything already existed) early-return path.
func printAndWriteResults(bySymbol map[string]dtResult, ref *sqlx.DB, topN int) error {
	results := make([]dtResult, 0, len(bySymbol))
	for _, r := range bySymbol {
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })

	n := topN
	if n > len(results) {
		n = len(results)
	}
	fmt.Printf("\n🛡️  TOP %d ETFs BY RESILIENCE SCORE (best TP/SL/hold per symbol):\n", n)
	for i, r := range results[:n] {
		fmt.Printf("  #%3d  %-6s  TP+%.0f%%/SL-%.0f%%/Hold-%dd  CAGR=%7.2f%%  MaxDD=%6.2f%%  DDdays=%4d  WR=%5.1f%%  Trades=%4d  Score=%.4f\n",
			i+1, r.Symbol, r.TP*100, r.SL*100, r.Hold, r.CAGR*100, r.MaxDD*100, r.MaxDDDays, r.WinRate*100, r.Trades, r.Score)
	}

	rows := make([]refdb.DTStrategy, 0, len(results))
	for _, r := range results {
		rows = append(rows, refdb.DTStrategy{
			Symbol: r.Symbol, TP: r.TP, SL: r.SL, Hold: r.Hold, CAGR: r.CAGR, MaxDD: r.MaxDD,
			MaxDDDays: r.MaxDDDays, Trades: r.Trades, WinRate: r.WinRate, Score: r.Score,
		})
	}
	if err := refdb.SaveDTStrategies(ref, rows); err != nil {
		return fmt.Errorf("Failed to save etf_dt_strategies: %v", err)
	}
	fmt.Printf("\n✨ Wrote %d symbol configs to etf_dt_strategies in the reference DB\n   Run any of them: go run cmd/backtest/main.go -strategy dt_<symbol>\n", len(results))

	return nil
}
