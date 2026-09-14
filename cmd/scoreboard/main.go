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
	force := flag.Bool("force", false, "(default mode only) redo every strategy's backtest even if a usable result already exists")

	// Subcommand dispatch:
	//   scoreboard          -> skip strategies that already have a usable result, backtest what's missing, then compile
	//   scoreboard compile  -> skip backtests entirely, compile from result DBs already in reports/
	//   scoreboard status   -> like compile, but just report whether all compute is done (no table, no save)
	mode := "run"
	if len(os.Args) > 1 && (os.Args[1] == "compile" || os.Args[1] == "status") {
		mode = os.Args[1]
		os.Args = append(os.Args[:1], os.Args[2:]...) // drop the subcommand so flag.Parse still works
	}
	flag.Parse()

	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Fatalf("Failed to create out dir: %v", err)
	}

	switch mode {
	case "compile":
		runCompile(*concurrency)
	case "status":
		runStatus(*concurrency)
	default:
		runAll(*concurrency, *force)
	}
}

// runAll ensures every registered strategy has a usable backtest result. Unlike
// the old behavior, it does NOT blindly redo everything: it first checks
// reports/ (the same highest-increment-first, skip-if-compromised validation
// used by `compile`/`status`) and only actually backtests strategies that are
// missing a usable result. Pass -force to ignore existing results and redo
// everything anyway. The final table/scoreboard.db always covers every
// strategy — freshly run ones plus whatever was already valid.
func runAll(concurrency int, force bool) {
	fmt.Println("🚀 RUNNING SCOREBOARD: All Strategies (5 Years, $100k Capital)")

	allStrategies := strategy.List()
	if len(allStrategies) == 0 {
		log.Fatalf("No strategies registered.")
	}

	existing := map[string]compiledResult{}
	if !force {
		fmt.Println("🔎 Checking reports/ for strategies that already have a usable result...")
		existing, _, _, _, _ = scanAndValidate(concurrency)
	}

	var toRun []strategy.Strategy
	for _, s := range allStrategies {
		if _, ok := existing[s.ID()]; !ok {
			toRun = append(toRun, s)
		}
	}

	if force {
		fmt.Printf("   -force set: redoing all %d strategies regardless of existing results.\n\n", len(allStrategies))
	} else {
		fmt.Printf("   %d/%d strategies already have a usable result and will be skipped; %d need backtesting.\n\n",
			len(allStrategies)-len(toRun), len(allStrategies), len(toRun))
	}

	freshResults := make(map[string]runner.RunResult)
	if len(toRun) > 0 {
		// 1. Detect and Download missing data for the strategies we're actually running.
		if err := runner.DetectAndDownloadMissingData(targetDb, tableName, toRun, "", true, downloadYears); err != nil {
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

		// 3. Execute only the missing strategies with a bounded worker pool.
		// Unbounded one-goroutine-per-strategy here OOM-kills the process once
		// there are hundreds of registered strategies (the full shared bar map
		// plus every in-flight simulator/equity curve at once) — see the same
		// fix in cmd/backtest.
		fmt.Printf("   Concurrency: %d workers across %d strategies to run\n\n", concurrency, len(toRun))

		resultsSlice := make([]runner.RunResult, len(toRun))
		jobs := make(chan int, len(toRun))
		for i := range toRun {
			jobs <- i
		}
		close(jobs)

		var mu sync.Mutex
		var wg sync.WaitGroup
		for w := 0; w < concurrency; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for idx := range jobs {
					s := toRun[idx]
					cfg := runner.BuildConfig(s, 0.0, 0.0, 0, 0)
					res := runner.ExecuteStrategy(s, cfg, barsBySymbol, sortedDates, capital, "", outDir, targetDb)
					resultsSlice[idx] = res
					if res.Err != nil {
						log.Printf("❌ [%s] Error: %v\n", s.ID(), res.Err)
					} else {
						log.Printf("✅ [%s] Completed (CAGR: %.2f%%)", s.ID(), res.Report.CAGR*100)
					}
					mu.Lock()
					freshResults[s.ID()] = res
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
	} else {
		fmt.Println("✅ Nothing to backtest — every registered strategy already has a usable result. (Use -force to redo everything.)")
	}

	// 4. Merge freshly-run results with whatever was already valid so the final
	// table/scoreboard.db covers every registered strategy, not just the ones we
	// just ran.
	results := make([]runner.RunResult, 0, len(allStrategies))
	for _, s := range allStrategies {
		if res, ok := freshResults[s.ID()]; ok {
			results = append(results, res)
			continue
		}
		if c, ok := existing[s.ID()]; ok {
			results = append(results, runner.RunResult{Strat: s, Report: c.Report, DbPath: c.DbPath})
		}
	}

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

// compiledResult is the winning (highest-increment, non-compromised) result for
// one strategy after validation.
type compiledResult struct {
	StrategyID string
	Report     models.PerformanceReport
	DbPath     string
	Increment  int
}

// scanAndValidate globs every result DB in reports/, groups them by strategy,
// and for each strategy tries its run increments highest-first, skipping any
// that are compromised (see validatePerformanceSummary), until it finds a usable
// one or runs out. This is the shared work behind both `scoreboard compile`
// (prints the full table) and `scoreboard status` (just reports coverage).
func scanAndValidate(concurrency int) (byStrategy map[string]compiledResult, totalFiles, totalGroups, usedFallback, allCompromised int) {
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
	totalFiles = len(files)
	totalGroups = len(groups)
	if totalGroups == 0 {
		return map[string]compiledResult{}, totalFiles, totalGroups, 0, 0
	}
	for base := range groups {
		sort.Slice(groups[base], func(i, j int) bool {
			return groups[base][i].increment > groups[base][j].increment // highest increment first
		})
	}

	fmt.Printf("   Found %d result databases across %d strategies. Validating with %d workers (highest run increment first, falling back on corruption)...\n\n",
		totalFiles, totalGroups, concurrency)

	var mu sync.Mutex
	byStrategy = make(map[string]compiledResult)

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
					byStrategy[row.StrategyID] = compiledResult{
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

	return byStrategy, totalFiles, totalGroups, usedFallback, allCompromised
}

// missingStrategies returns the IDs of every currently-registered strategy that
// has no usable (non-compromised) entry in byStrategy — i.e. it has genuinely
// never been backtested, or every run it has was corrupted/incomplete.
func missingStrategies(byStrategy map[string]compiledResult) []string {
	var missing []string
	for _, s := range strategy.List() {
		if _, ok := byStrategy[s.ID()]; !ok {
			missing = append(missing, s.ID())
		}
	}
	sort.Strings(missing)
	return missing
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

	byStrategy, _, totalGroups, usedFallback, allCompromised := scanAndValidate(concurrency)
	if totalGroups == 0 {
		log.Fatalf("No result databases found in %s/*.db. Run backtests first (e.g. cmd/backtest -strategy all).", outDir)
	}

	fmt.Printf("⚡ %d strategies compiled (%d fell back to a lower run increment after finding corruption, %d had every increment compromised and were skipped).\n",
		len(byStrategy), usedFallback, allCompromised)

	if missing := missingStrategies(byStrategy); len(missing) > 0 {
		fmt.Printf("⚠️  %d currently-registered strategies have NO usable result at all (never backtested, or every run compromised) — compute is NOT fully done:\n",
			len(missing))
		printStrategyList(missing)
	} else {
		fmt.Println("✅ Every currently-registered strategy has a usable result. Compute is fully done.")
	}

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

// runStatus answers "is all the necessary compute already done?" without
// printing the full comparison table or touching scoreboard.db — it just
// validates every existing result DB (same as compile) and reports which
// currently-registered strategies are covered vs. missing/compromised.
func runStatus(concurrency int) {
	fmt.Println("🔎 SCOREBOARD STATUS — checking whether every registered strategy has a usable backtest result")

	strategy.AutoRegisterSQLStrategies(".", targetDb)

	total := len(strategy.List())
	byStrategy, _, totalGroups, usedFallback, allCompromised := scanAndValidate(concurrency)

	fmt.Printf("\n📋 %d strategies currently registered.\n", total)
	fmt.Printf("   %d result-DB groups found in %s/, %d validated successfully (%d needed a fallback to an older run increment).\n",
		totalGroups, outDir, len(byStrategy), usedFallback)
	if allCompromised > 0 {
		fmt.Printf("   %d strategies have result files but every increment is compromised.\n", allCompromised)
	}

	missing := missingStrategies(byStrategy)
	if len(missing) == 0 {
		fmt.Println("\n✅ All necessary compute is done — every registered strategy has a usable result. Safe to run `scoreboard compile`.")
		return
	}

	fmt.Printf("\n❌ Compute is NOT done — %d of %d registered strategies have no usable result:\n", len(missing), total)
	printStrategyList(missing)
	fmt.Printf("\nRun backtests for these (e.g. `go run cmd/backtest/main.go -strategy %s` or `-strategy all`) before compiling.\n",
		strings.Join(missing[:min(3, len(missing))], ","))
}

// printStrategyList prints IDs one per line, capped so a status/compile run
// against hundreds of missing strategies doesn't flood the terminal.
func printStrategyList(ids []string) {
	const maxShown = 25
	for i, id := range ids {
		if i >= maxShown {
			fmt.Printf("   ... and %d more\n", len(ids)-maxShown)
			break
		}
		fmt.Printf("   - %s\n", id)
	}
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
