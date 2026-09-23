// cmd/ticker_scan — Applies the MARA "Precision 200-SMA Bounce" decision tree
// (volatility coil + SMA200 re-test) to every candidate symbol in the database,
// to find which tickers the pattern generalizes to beyond MARA.
//
// Usage:
//
//	go run cmd/ticker_scan/main.go
//	go run cmd/ticker_scan/main.go -symbols SOXL,TQQQ,COIN
package ticker_scan

import (
	"flag"
	"fmt"
	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
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

// Config holds the settings of a run.
type Config struct {
	Db          string   // -db
	Symbols     string   // -symbols
	Capital     float64  // -capital
	Alloc       float64  // -alloc
	Tp          float64  // -tp
	Sl          float64  // -sl
	Hold        int      // -hold
	Yield       float64  // -yield
	MinTrades   int      // -min-trades
	OptimizeTop int      // -optimize-top
	Concurrency int      // -concurrency
	Args        []string // positional arguments
}

// DefaultConfig returns the CLI defaults.
func DefaultConfig() Config {
	return Config{
		Db:          appenv.MarketDB(),
		Symbols:     "",
		Capital:     100000.0,
		Alloc:       0.65,
		Tp:          0.05,
		Sl:          0.08,
		Hold:        1,
		Yield:       0.045,
		MinTrades:   10,
		OptimizeTop: 3,
		Concurrency: runtime.NumCPU(),
	}
}

// Main is the CLI entry point.
func Main() {

	conf := DefaultConfig()
	d := conf
	flag.StringVar(&conf.Db, "db", d.Db, "Path to SQLite database")
	flag.StringVar(&conf.Symbols, "symbols", d.Symbols, "Comma-separated symbol list to scan (defaults to every symbol in the database)")
	flag.Float64Var(&conf.Capital, "capital", d.Capital, "Starting cash ($)")
	flag.Float64Var(&conf.Alloc, "alloc", d.Alloc, "Allocation percentage per position")
	flag.Float64Var(&conf.Tp, "tp", d.Tp, "Take-profit percentage")
	flag.Float64Var(&conf.Sl, "sl", d.Sl, "Stop-loss percentage")
	flag.IntVar(&conf.Hold, "hold", d.Hold, "Holding window (days)")
	flag.Float64Var(&conf.Yield, "yield", d.Yield, "Idle cash yield (annualized)")
	flag.IntVar(&conf.MinTrades, "min-trades", d.MinTrades, "Minimum trade count filter")
	flag.IntVar(&conf.OptimizeTop, "optimize-top", d.OptimizeTop, "Sweep TP/SL/hold for this many top-ranked candidates (0 disables)")
	flag.IntVar(&conf.Concurrency, "concurrency", d.Concurrency, "Max concurrent symbol workers. Defaults to all CPU cores.")
	flag.Parse()
	conf.Args = flag.Args()
	if err := Run(conf); err != nil {
		log.Fatal(err)
	}
}

// Run executes the command with cfg. It returns errors instead of exiting.
func Run(conf Config) error {
	var candidates []string
	if strings.TrimSpace(conf.Symbols) != "" {
		for _, s := range strings.Split(conf.Symbols, ",") {
			s = strings.TrimSpace(strings.ToUpper(s))
			if s != "" {
				candidates = append(candidates, s)
			}
		}
	} else {
		var err error
		candidates, err = allUniverseSymbols(conf.Db)
		if err != nil {
			return fmt.Errorf("Failed to enumerate symbols: %v", err)
		}
	}

	db, err := storage.OpenSQLite(conf.Db)
	if err != nil {
		return fmt.Errorf("Failed to open DB: %v", err)
	}
	defer db.Close()

	fmt.Println()
	fmt.Println("=======================================================================================================================")
	fmt.Println("🔎 TICKER SCAN — Precision 200-SMA Bounce Decision Tree across candidate symbols")
	fmt.Println("=======================================================================================================================")
	fmt.Printf("Params: TP=+%.1f%% SL=-%.1f%% Hold=%dd Alloc=%.0f%% Yield=%.1f%% MinTrades=%d\n\n",
		conf.Tp*100, conf.Sl*100, conf.Hold, conf.Alloc*100, conf.Yield*100, conf.MinTrades)

	// Bounded worker pool across candidate symbols (each does its own DB fetch +
	// tree-signal generation + simulation — independent, CPU/IO-bound work).
	workers := conf.Concurrency
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

				sigs := strategy.TreeBounceSignals(sym, bars, conf.Tp, conf.Sl, conf.Hold)
				if len(sigs) < conf.MinTrades {
					continue
				}

				dates := make([]string, len(bars))
				for i, b := range bars {
					dates[i] = b.Date
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
		return nil
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

	if conf.OptimizeTop > 0 {
		n := conf.OptimizeTop
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
						report, _, _ := sim.Run(sigs, map[string][]models.Bar{r.Symbol: bars}, dates)
						if report.TotalTrades < conf.MinTrades {
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
	return nil
}
