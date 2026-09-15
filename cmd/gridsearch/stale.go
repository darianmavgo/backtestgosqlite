package main

import (
	"log"
	"sort"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/runner"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

// runStaleCommand implements `gridsearch stale`: assess every strategy with a
// completed sweep in gridDBPath for staleness (see runner.AssessOne) and
// print a report. No sweeps are run.
func runStaleCommand(gridDBPath, marketDBPath string) {
	gdb, err := storage.OpenSQLite(gridDBPath)
	if err != nil {
		log.Fatalf("Failed to open gridsearch pipeline DB %s: %v", gridDBPath, err)
	}
	defer gdb.Close()
	if err := ensureGridSearchSchema(gdb); err != nil {
		log.Fatalf("Failed to initialize gridsearch pipeline schema: %v", err)
	}

	type row struct {
		StrategyID  string `db:"strategy_id"`
		FinishedAt  string `db:"finished_at"`
		DataMaxDate string `db:"data_max_date"`
	}
	var rows []row
	if err := gdb.Select(&rows, `SELECT strategy_id, finished_at, COALESCE(data_max_date, '') AS data_max_date FROM gridsearch_runs WHERE status = 'done'`); err != nil {
		log.Fatalf("Failed to query gridsearch_runs: %v", err)
	}
	if len(rows) == 0 {
		log.Println("No completed sweeps found in", gridDBPath, "— nothing to assess. Run some sweeps first.")
		return
	}

	marketDB, err := storage.OpenSQLite(marketDBPath)
	if err != nil {
		log.Fatalf("Failed to open market DB %s: %v", marketDBPath, err)
	}
	defer marketDB.Close()
	latestDataDate, err := runner.LatestMarketDate(marketDB)
	if err != nil {
		log.Printf("Warning: could not determine latest market data date (%v) — data-freshness checks will be skipped", err)
		latestDataDate = ""
	}

	swept := make(map[string]bool, len(rows))
	var entries []runner.StaleEntry
	for _, r := range rows {
		swept[r.StrategyID] = true
		computedAt, err := time.Parse(time.RFC3339, r.FinishedAt)
		if err != nil {
			continue
		}
		entries = append(entries, runner.AssessOne(r.StrategyID, computedAt, r.DataMaxDate, latestDataDate))
	}

	var neverSwept []string
	for _, s := range strategy.List() {
		if !swept[s.ID()] {
			neverSwept = append(neverSwept, s.ID())
		}
	}
	sort.Strings(neverSwept)

	runner.PrintStalenessReport(gridDBPath, entries, neverSwept)
}
