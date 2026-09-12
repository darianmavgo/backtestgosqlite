package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/runner"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	fmt.Println("🚀 RUNNING SCOREBOARD: All Strategies (5 Years, $100k Capital)")

	targetDb := "data/market_history.db"
	tableName := "backtest_start"
	capital := 100000.0
	downloadYears := 5
	outDir := "reports"

	if err := os.MkdirAll(outDir, 0755); err != nil {
		log.Fatalf("Failed to create out dir: %v", err)
	}

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

	// 3. Execute all strategies concurrently
	results := make([]runner.RunResult, len(allStrategies))
	var wg sync.WaitGroup

	for i, strat := range allStrategies {
		wg.Add(1)
		go func(idx int, s strategy.Strategy) {
			defer wg.Done()
			cfg := runner.BuildConfig(s, 0.0, 0.0, 0, 0)
			res := runner.ExecuteStrategy(s, cfg, barsBySymbol, sortedDates, capital, "", outDir, targetDb)
			results[idx] = res
			if res.Err != nil {
				log.Printf("❌ [%s] Error: %v\n", s.ID(), res.Err)
			} else {
				log.Printf("✅ [%s] Completed (CAGR: %.2f%%)", s.ID(), res.Report.CAGR*100)
			}
		}(i, strat)
	}
	wg.Wait()

	// 4. Print and capture scoreboard
	// The runner.PrintComparisonTable will sort the slice internally, so when we save to SQLite
	// it will be naturally ordered by CAGR.
	runner.PrintComparisonTable(results)
	saveScoreboardToSQLite(results, filepath.Join(outDir, "scoreboard.db"))
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
		INSERT INTO scoreboard (strategy_id, name, cagr, total_return, sharpe, max_drawdown, win_rate, trades, run_date)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
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
			r.Report.WinRate,
			r.Report.TotalTrades,
			runDate,
		)
	}
	tx.Commit()

	fmt.Printf("\n💾 Scoreboard saved to SQLite: %s\n", dbPath)
}
