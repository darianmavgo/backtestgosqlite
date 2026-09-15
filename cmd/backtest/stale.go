package main

import (
	"fmt"
	"log"
	"os"

	"github.com/darianmavgo/backtestgosqlite/pkg/runner"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

// runStaleCommand implements `backtest stale`: assess every strategy with a
// usable result in outDir for staleness (see runner.AssessOne) and print a
// report. No backtests are run.
func runStaleCommand(outDir, marketDBPath string, concurrency int) {
	fmt.Println("🔎 Checking", outDir, "for strategies with results to assess...")
	byStrategy, _, _, _, _ := runner.ScanAndValidate(outDir, concurrency)
	if len(byStrategy) == 0 {
		fmt.Println("No usable results found — nothing to assess. Run some backtests first.")
		return
	}

	marketDB, err := storage.OpenSQLite(marketDBPath)
	if err != nil {
		log.Printf("Warning: could not open market DB %s (%v) — data-freshness checks will be skipped", marketDBPath, err)
		marketDB = nil
	} else {
		defer marketDB.Close()
	}

	var entries []runner.StaleEntry
	for id, result := range byStrategy {
		info, err := os.Stat(result.DbPath)
		if err != nil {
			continue
		}
		entries = append(entries, runner.AssessOne(id, info.ModTime(), result.Report.EndDate, marketDB))
	}

	runner.PrintStalenessReport(outDir, entries, runner.MissingStrategies(byStrategy))
}
