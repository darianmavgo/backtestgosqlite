package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

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
	concurrency := flag.Int("concurrency", runtime.NumCPU(), "Max concurrent workers (defaults to all CPU cores; bounds memory/IO use with hundreds of strategies)")
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

// runAll ensures every registered strategy has a usable backtest result. It does
// NOT blindly redo everything: it first checks reports/ (pkg/runner.ScanAndValidate
// — the same highest-increment-first, skip-if-compromised logic used by
// compile/status, and by cmd/backtest's multi-strategy mode) and only actually
// backtests strategies that are missing a usable result. Pass -force to ignore
// existing results and redo everything anyway. The final table/scoreboard.db
// always covers every strategy — freshly run ones plus whatever was already valid.
func runAll(concurrency int, force bool) {
	fmt.Println("🚀 RUNNING SCOREBOARD: All Strategies (5 Years, $100k Capital)")

	allStrategies := strategy.List()
	if len(allStrategies) == 0 {
		log.Fatalf("No strategies registered.")
	}

	existing := map[string]runner.CompiledResult{}
	if !force {
		fmt.Println("🔎 Checking reports/ for strategies that already have a usable result...")
		existing, _, _, _, _ = runner.ScanAndValidate(outDir, concurrency)
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

// runCompile assumes every strategy has already been backtested (e.g. via
// `cmd/backtest -strategy all`) and just reads each per-strategy SQLite DB's
// performance_summary table already sitting in reports/, instead of re-running
// anything. Much cheaper: no bar loading, no simulation, no tree fitting.
func runCompile(concurrency int) {
	fmt.Println("📖 COMPILING SCOREBOARD from existing per-strategy result databases (no backtests run)")

	strategy.AutoRegisterSQLStrategies(".", targetDb) // so -sql strategy names/descriptions resolve too

	byStrategy, _, totalGroups, usedFallback, allCompromised := runner.ScanAndValidate(outDir, concurrency)
	if totalGroups == 0 {
		log.Fatalf("No result databases found in %s/*.db. Run backtests first (e.g. cmd/backtest -strategy all).", outDir)
	}

	fmt.Printf("⚡ %d strategies compiled (%d fell back to a lower run increment after finding corruption, %d had every increment compromised and were skipped).\n",
		len(byStrategy), usedFallback, allCompromised)

	if missing := runner.MissingStrategies(byStrategy); len(missing) > 0 {
		fmt.Printf("⚠️  %d currently-registered strategies have NO usable result at all (never backtested, or every run compromised) — compute is NOT fully done:\n",
			len(missing))
		runner.PrintMissingList(missing)
	} else {
		fmt.Println("✅ Every currently-registered strategy has a usable result. Compute is fully done.")
	}

	if len(byStrategy) == 0 {
		log.Fatalf("No usable performance_summary rows found.")
	}

	results := make([]runner.RunResult, 0, len(byStrategy))
	for id, c := range byStrategy {
		results = append(results, runner.RunResult{
			Strat:  runner.ResolveStrategy(id),
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
	byStrategy, _, totalGroups, usedFallback, allCompromised := runner.ScanAndValidate(outDir, concurrency)

	fmt.Printf("\n📋 %d strategies currently registered.\n", total)
	fmt.Printf("   %d result-DB groups found in %s/, %d validated successfully (%d needed a fallback to an older run increment).\n",
		totalGroups, outDir, len(byStrategy), usedFallback)
	if allCompromised > 0 {
		fmt.Printf("   %d strategies have result files but every increment is compromised.\n", allCompromised)
	}

	missing := runner.MissingStrategies(byStrategy)
	if len(missing) == 0 {
		fmt.Println("\n✅ All necessary compute is done — every registered strategy has a usable result. Safe to run `scoreboard compile`.")
		return
	}

	fmt.Printf("\n❌ Compute is NOT done — %d of %d registered strategies have no usable result:\n", len(missing), total)
	runner.PrintMissingList(missing)
	shown := missing
	if len(shown) > 3 {
		shown = shown[:3]
	}
	fmt.Printf("\nRun backtests for these (e.g. `go run cmd/backtest/main.go -strategy %s` or `-strategy all`) before compiling.\n",
		strings.Join(shown, ","))
}

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
