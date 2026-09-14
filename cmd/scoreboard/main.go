package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
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

// dbIncrementPattern matches storage.CreateUniqueDB's naming scheme: the first run
// of a strategy is "<id>.db", every repeat run after that is "<id>_<n>.db" for
// n = 2, 3, 4, .... Captures the base id and the trailing numeric suffix.
var dbIncrementPattern = regexp.MustCompile(`^(.+)_(\d+)$`)

// parseDBIncrement splits a result DB's filename into its strategy base name and
// run increment (1 for the un-suffixed first run, 2+ for "_<n>" repeat runs).
func parseDBIncrement(path string) (base string, increment int) {
	stem := strings.TrimSuffix(filepath.Base(path), ".db")
	if m := dbIncrementPattern.FindStringSubmatch(stem); m != nil {
		if n, err := strconv.Atoi(m[2]); err == nil {
			return m[1], n
		}
	}
	return stem, 1
}

// candidate is one result DB file competing to represent its strategy in the
// scoreboard — there's one per run increment (dt_hibl.db, dt_hibl_2.db, ...).
type candidate struct {
	path      string
	increment int
}

// runCompile assumes every strategy has already been backtested (e.g. via
// `cmd/backtest -strategy all`) and just reads each per-strategy SQLite DB's
// performance_summary table already sitting in reports/, instead of re-running
// anything. Much cheaper: no bar loading, no simulation, no tree fitting.
//
// When a strategy has been backtested more than once (dt_hibl.db, dt_hibl_2.db, ...),
// it picks the highest-increment run first, verifies that database isn't
// compromised (corrupt SQLite file, failed integrity check, missing/unreadable
// performance_summary), and falls back to the next-lower increment if it is —
// repeating down to increment 1 until a healthy database is found.
func runCompile(concurrency int) {
	fmt.Println("📖 COMPILING SCOREBOARD from existing per-strategy result databases (no backtests run)")

	strategy.AutoRegisterSQLStrategies(".", targetDb) // so -sql strategy names/descriptions resolve too

	files, err := filepath.Glob(filepath.Join(outDir, "*.db"))
	if err != nil {
		log.Fatalf("Failed to list %s/*.db: %v", outDir, err)
	}

	// Group every candidate file by its strategy base name, tracking each one's run
	// increment so we can try highest-first per group.
	groups := make(map[string][]candidate)
	for _, f := range files {
		if filepath.Base(f) == "scoreboard.db" {
			continue
		}
		base, inc := parseDBIncrement(f)
		groups[base] = append(groups[base], candidate{path: f, increment: inc})
	}
	if len(groups) == 0 {
		log.Fatalf("No result databases found in %s/*.db. Run backtests first (e.g. cmd/backtest -strategy all).", outDir)
	}
	for base := range groups {
		sort.Slice(groups[base], func(i, j int) bool {
			return groups[base][i].increment > groups[base][j].increment // highest increment first
		})
	}

	fmt.Printf("   Found %d result databases across %d strategies. Validating with %d workers (highest run increment first, falling back on corruption)...\n\n",
		len(files), len(groups), concurrency)

	type compiled struct {
		StrategyID string
		Report     models.PerformanceReport
		DbPath     string
		Increment  int
	}

	var mu sync.Mutex
	byStrategy := make(map[string]compiled)
	var usedFallback, allCompromised int

	bases := make([]string, 0, len(groups))
	for base := range groups {
		bases = append(bases, base)
	}
	jobs := make(chan string, len(bases))
	for _, b := range bases {
		jobs <- b
	}
	close(jobs)

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for base := range jobs {
				cands := groups[base]
				var rows []performanceRow
				var chosen candidate
				fellBack := false
				for i, c := range cands {
					r, err := validatePerformanceSummary(c.path)
					if err != nil {
						log.Printf("⚠️  [%s] run increment %d (%s) is compromised (%v)%s",
							base, c.increment, filepath.Base(c.path), err, fallbackNote(i, cands))
						fellBack = true
						continue
					}
					rows = r
					chosen = c
					break
				}
				if rows == nil {
					mu.Lock()
					allCompromised++
					mu.Unlock()
					continue
				}

				mu.Lock()
				if fellBack {
					usedFallback++
				}
				for _, row := range rows {
					byStrategy[row.StrategyID] = compiled{
						StrategyID: row.StrategyID,
						Report:     row.Report,
						DbPath:     chosen.path,
						Increment:  chosen.increment,
					}
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	fmt.Printf("⚡ %d strategies compiled (%d fell back to a lower run increment after finding corruption, %d had every increment compromised and were skipped).\n",
		len(byStrategy), usedFallback, allCompromised)

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

// fallbackNote describes what happens next when a candidate fails validation.
func fallbackNote(i int, cands []candidate) string {
	if i+1 < len(cands) {
		return fmt.Sprintf(" — reverting to run increment %d", cands[i+1].increment)
	}
	return " — no earlier run increment available, skipping this strategy"
}

type performanceRow struct {
	StrategyID string
	Report     models.PerformanceReport
}

// validatePerformanceSummary opens a single result DB, confirms it isn't
// compromised, and reads every row of its performance_summary table (normally
// exactly one, keyed by strategy_id). "Compromised" covers everything that can go
// wrong with one of these files in practice: the SQLite file itself is corrupt or
// truncated (e.g. the process was killed mid-write), or it's a schema-only stub
// with no performance_summary rows at all (e.g. from an interrupted backtest run
// that created the file via storage.CreateUniqueDB but never got to write results).
// Any of these return an error so the caller can fall back to the previous run
// increment instead of silently compiling a blank/broken row.
func validatePerformanceSummary(path string) ([]performanceRow, error) {
	db, err := storage.OpenSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open: %w", err)
	}
	defer db.Close()

	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check;`).Scan(&integrity); err != nil {
		return nil, fmt.Errorf("integrity_check query failed: %w", err)
	}
	if integrity != "ok" {
		return nil, fmt.Errorf("integrity_check failed: %s", integrity)
	}

	var tableExists string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='performance_summary'`).Scan(&tableExists)
	if err != nil {
		return nil, fmt.Errorf("no performance_summary table (schema-only stub?)")
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
		return nil, fmt.Errorf("failed to query performance_summary: %w", err)
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
			return nil, fmt.Errorf("malformed performance_summary row: %w", err)
		}
		r.Report = rep
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("performance_summary table is empty")
	}
	return out, nil
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
