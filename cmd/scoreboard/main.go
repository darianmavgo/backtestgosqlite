package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/runner"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	_ "github.com/mattn/go-sqlite3"
)

const (
	targetDb      = "data/market_history.db"
	tableName     = "backtest_start"
	capital       = 100000.0
	downloadYears = 5
	outDir        = "reports"
)

func main() {
	concurrency := flag.Int("concurrency", 8, "Max concurrent workers (bounds memory/IO use with hundreds of strategies)")

	// Subcommand dispatch: `scoreboard compile` skips running backtests and just
	// compiles the scoreboard from the per-strategy result DBs already in reports/
	// (e.g. from a prior `cmd/backtest -strategy all` run). Any other/no first
	// argument keeps the original "run everything" behavior.
	compileMode := false
	if len(os.Args) > 1 && os.Args[1] == "compile" {
		compileMode = true
		os.Args = append(os.Args[:1], os.Args[2:]...) // drop the subcommand so flag.Parse still works
	}
	flag.Parse()

	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Fatalf("Failed to create out dir: %v", err)
	}

	if compileMode {
		runCompile(*concurrency)
		return
	}
	runAll(*concurrency)
}

// runAll executes every registered strategy end-to-end (the original scoreboard
// behavior) and compiles the results.
func runAll(concurrency int) {
	fmt.Println("🚀 RUNNING SCOREBOARD: All Strategies (5 Years, $100k Capital)")

	allStrategies := strategy.List()
	if len(allStrategies) == 0 {
		log.Fatalf("No strategies registered.")
	}

	// 1. Detect and Download missing data for all strategies
	if err := runner.DetectAndDownloadMissingData(targetDb, tableName, allStrategies, "", true, downloadYears); err != nil {
		log.Fatalf("Market data resolution error: %v", err)
	}

	// 2. Open market DB
	db, err := storage.OpenSQLite(targetDb)
	if err != nil {
		log.Fatalf("Failed to open market DB: %v", err)
	}
	defer db.Close()

	fmt.Printf("\n⚙️ Loading chronological bars from '%s'...\n", tableName)
	barsBySymbol, sortedDates, err := storage.FetchAllBarsChronological(db, tableName)
	if err != nil {
		log.Fatalf("Error loading bars: %v", err)
	}

	// 3. Execute all strategies with a bounded worker pool. Unbounded one-goroutine-
	// per-strategy here OOM-kills the process once there are hundreds of registered
	// strategies (the full shared bar map plus every in-flight simulator/equity curve
	// at once) — see the same fix in cmd/backtest.
	fmt.Printf("   Concurrency: %d workers across %d strategies\n\n", concurrency, len(allStrategies))

	results := make([]runner.RunResult, len(allStrategies))
	jobs := make(chan int, len(allStrategies))
	for i := range allStrategies {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				s := allStrategies[idx]
				cfg := runner.BuildConfig(s, 0.0, 0.0, 0, 0)
				res := runner.ExecuteStrategy(s, cfg, barsBySymbol, sortedDates, capital, "", outDir, targetDb)
				results[idx] = res
				if res.Err != nil {
					log.Printf("❌ [%s] Error: %v\n", s.ID(), res.Err)
				} else {
					log.Printf("✅ [%s] Completed (CAGR: %.2f%%)", s.ID(), res.Report.CAGR*100)
				}
			}
		}()
	}
	wg.Wait()

	// 4. Print and capture scoreboard
	// The runner.PrintComparisonTable will sort the slice internally, so when we save to SQLite
	// it will be naturally ordered by CAGR.
	runner.PrintComparisonTable(results)
	saveScoreboardToSQLite(results, filepath.Join(outDir, "scoreboard.db"))
}

// runCompile assumes every strategy has already been backtested (e.g. via
// `cmd/backtest -strategy all`) and just reads each per-strategy SQLite DB's
// performance_summary table already sitting in reports/, instead of re-running
// anything. Much cheaper: no bar loading, no simulation, no tree fitting.
func runCompile(concurrency int) {
	fmt.Println("📖 COMPILING SCOREBOARD from existing per-strategy result databases (no backtests run)")

	strategy.AutoRegisterSQLStrategies(".", targetDb) // so -sql strategy names/descriptions resolve too

	files, err := filepath.Glob(filepath.Join(outDir, "*.db"))
	if err != nil {
		log.Fatalf("Failed to list %s/*.db: %v", outDir, err)
	}
	// Exclude the scoreboard output itself.
	var candidates []string
	for _, f := range files {
		if filepath.Base(f) == "scoreboard.db" {
			continue
		}
		candidates = append(candidates, f)
	}
	if len(candidates) == 0 {
		log.Fatalf("No result databases found in %s/*.db. Run backtests first (e.g. cmd/backtest -strategy all).", outDir)
	}

	// Sort by modification time ascending so that when multiple files exist for the
	// same strategy_id (e.g. dt_hibl.db and dt_hibl_2.db from repeated runs), the
	// most recently modified one wins when we dedupe below.
	sort.Slice(candidates, func(i, j int) bool {
		fi, _ := os.Stat(candidates[i])
		fj, _ := os.Stat(candidates[j])
		if fi == nil || fj == nil {
			return false
		}
		return fi.ModTime().Before(fj.ModTime())
	})

	fmt.Printf("   Found %d result databases. Reading with %d workers...\n\n", len(candidates), concurrency)

	type compiled struct {
		StrategyID string
		Report     models.PerformanceReport
		DbPath     string
		ModTime    time.Time
	}

	var mu sync.Mutex
	byStrategy := make(map[string]compiled)
	var read, skippedNoTable, skippedErr int

	jobs := make(chan string, len(candidates))
	for _, f := range candidates {
		jobs <- f
	}
	close(jobs)

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				rows, err := readPerformanceSummary(path)
				mu.Lock()
				if err != nil {
					skippedErr++
					mu.Unlock()
					continue
				}
				if len(rows) == 0 {
					skippedNoTable++
					mu.Unlock()
					continue
				}
				fi, _ := os.Stat(path)
				modTime := time.Time{}
				if fi != nil {
					modTime = fi.ModTime()
				}
				for _, row := range rows {
					existing, ok := byStrategy[row.StrategyID]
					if !ok || modTime.After(existing.ModTime) {
						byStrategy[row.StrategyID] = compiled{
							StrategyID: row.StrategyID,
							Report:     row.Report,
							DbPath:     path,
							ModTime:    modTime,
						}
					}
				}
				read++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	fmt.Printf("⚡ Read %d files (%d skipped: no performance_summary table, %d skipped: open/query error). %d unique strategies compiled.\n",
		read, skippedNoTable, skippedErr, len(byStrategy))

	if len(byStrategy) == 0 {
		log.Fatalf("No usable performance_summary rows found.")
	}

	results := make([]runner.RunResult, 0, len(byStrategy))
	for id, c := range byStrategy {
		results = append(results, runner.RunResult{
			Strat:  resolveStrategy(id),
			Report: c.Report,
			DbPath: c.DbPath,
		})
	}

	runner.PrintComparisonTable(results)
	saveScoreboardToSQLite(results, filepath.Join(outDir, "scoreboard.db"))
}

type performanceRow struct {
	StrategyID string
	Report     models.PerformanceReport
}

// readPerformanceSummary opens a single result DB and reads every row of its
// performance_summary table (normally exactly one, keyed by strategy_id).
func readPerformanceSummary(path string) ([]performanceRow, error) {
	db, err := storage.OpenSQLite(path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var tableExists string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='performance_summary'`).Scan(&tableExists)
	if err != nil {
		return nil, nil // no such table — not an error, just nothing to compile
	}

	rows, err := db.Query(`
		SELECT strategy_id, start_date, end_date, total_trading_days, total_calendar_years,
			initial_capital, final_equity, net_profit, total_return_pct, cagr,
			sharpe_ratio, sortino_ratio, calmar_ratio, omega_ratio, ulcer_index,
			alpha, beta, benchmark_return_pct, max_drawdown_pct, max_drawdown_dollars,
			max_drawdown_peak_equity, max_drawdown_trough_equity,
			max_drawdown_peak_date, max_drawdown_trough_date, max_drawdown_days,
			total_trades, winning_trades, losing_trades, win_rate, profit_factor,
			avg_trade_return_pct, avg_win_amount, avg_loss_amount, payoff_ratio,
			avg_holding_days, avg_mae, avg_mfe, total_commission_paid
		FROM performance_summary
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []performanceRow
	for rows.Next() {
		var r performanceRow
		var rep models.PerformanceReport
		if err := rows.Scan(
			&r.StrategyID, &rep.StartDate, &rep.EndDate, &rep.TotalTradingDays, &rep.TotalCalendarYears,
			&rep.InitialCapital, &rep.FinalEquity, &rep.NetProfit, &rep.TotalReturnPct, &rep.CAGR,
			&rep.SharpeRatio, &rep.SortinoRatio, &rep.CalmarRatio, &rep.OmegaRatio, &rep.UlcerIndex,
			&rep.Alpha, &rep.Beta, &rep.BenchmarkReturnPct, &rep.MaxDrawdownPct, &rep.MaxDrawdownDollars,
			&rep.MaxDrawdownPeakEquity, &rep.MaxDrawdownTroughEquity,
			&rep.MaxDrawdownPeakDate, &rep.MaxDrawdownTroughDate, &rep.MaxDrawdownDuration,
			&rep.TotalTrades, &rep.WinningTrades, &rep.LosingTrades, &rep.WinRate, &rep.ProfitFactor,
			&rep.AvgTradeReturnPct, &rep.AvgWinAmount, &rep.AvgLossAmount, &rep.PayoffRatio,
			&rep.AvgHoldingDays, &rep.AvgMAE, &rep.AvgMFE, &rep.TotalCommissionPaid,
		); err != nil {
			continue
		}
		r.Report = rep
		out = append(out, r)
	}
	return out, rows.Err()
}

// resolveStrategy looks up the live registered strategy for display purposes
// (name/description); falls back to a minimal stub carrying just the ID when a
// result DB refers to a strategy that's no longer registered (renamed/removed).
func resolveStrategy(id string) strategy.Strategy {
	if s, ok := strategy.Get(id); ok {
		return s
	}
	return &staleStrategy{id: id}
}

type staleStrategy struct{ id string }

func (s *staleStrategy) ID() string                                              { return s.id }
func (s *staleStrategy) Name() string                                            { return s.id + " (not currently registered)" }
func (s *staleStrategy) Description() string                                     { return "" }
func (s *staleStrategy) DefaultConfig() strategy.StrategyConfig                  { return strategy.StrategyConfig{} }
func (s *staleStrategy) Validate() error                                         { return nil }
func (s *staleStrategy) SetDatabases(a, b string)                                {}
func (s *staleStrategy) GenerateSignals(map[string][]models.Bar) []models.Signal { return nil }

func saveScoreboardToSQLite(results []runner.RunResult, dbPath string) {
	os.Remove(dbPath) // Fresh scoreboard every time
	db, err := storage.OpenSQLite(dbPath)
	if err != nil {
		log.Printf("Warning: Failed to create %s: %v", dbPath, err)
		return
	}
	defer db.Close()

	schema := `
	CREATE TABLE IF NOT EXISTS scoreboard (
		rank INTEGER PRIMARY KEY AUTOINCREMENT,
		strategy_id TEXT,
		name TEXT,
		cagr REAL,
		total_return REAL,
		sharpe REAL,
		max_drawdown REAL,
		max_drawdown_days INTEGER,
		win_rate REAL,
		trades INTEGER,
		run_date TEXT
	);`
	if _, err := db.Exec(schema); err != nil {
		log.Printf("Warning: Failed to create scoreboard schema: %v", err)
		return
	}

	tx, err := db.Begin()
	if err != nil {
		return
	}

	stmt, err := tx.Prepare(`
		INSERT INTO scoreboard (strategy_id, name, cagr, total_return, sharpe, max_drawdown, max_drawdown_days, win_rate, trades, run_date)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return
	}
	defer stmt.Close()

	// Make sure we iterate through results that have been sorted by PrintComparisonTable
	// PrintComparisonTable mutates the slice

	runDate := time.Now().Format(time.RFC3339)
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		stmt.Exec(
			r.Strat.ID(),
			r.Strat.Name(),
			r.Report.CAGR,
			r.Report.TotalReturnPct,
			r.Report.SharpeRatio,
			r.Report.MaxDrawdownPct,
			r.Report.MaxDrawdownDuration,
			r.Report.WinRate,
			r.Report.TotalTrades,
			runDate,
		)
	}
	tx.Commit()

	fmt.Printf("\n💾 Scoreboard saved to SQLite: %s\n", dbPath)
}
