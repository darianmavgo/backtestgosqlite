package main

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/jmoiron/sqlx"
)

// This file is the "pipeline controller" for cmd/gridsearch: a persisted record
// of which strategies have a completed sweep (gridsearch_runs) and the full
// evaluated-configuration results for each (gridsearch_results), both in
// reports/gridsearch.db. It lets a batch run of many strategies be interrupted
// and resumed — anything not marked 'done' gets retried — and lets `-strategy
// all` skip strategies that already have a usable sweep, the same
// skip-duplicate-work default used by cmd/backtest/cmd/scoreboard/
// cmd/etf_decision_trees.

// gridDBMu serializes all writes to the gridsearch pipeline DB — SQLite allows
// only one writer at a time, and multi-strategy batch mode calls these from
// several goroutines concurrently.
var gridDBMu sync.Mutex

func ensureGridSearchSchema(gdb *sqlx.DB) error {
	_, err := gdb.Exec(`
		CREATE TABLE IF NOT EXISTS gridsearch_runs (
			strategy_id            TEXT PRIMARY KEY,
			strategy_name          TEXT,
			status                 TEXT NOT NULL, -- 'running' | 'done' | 'failed'
			total_permutations     INTEGER,
			evaluated_configs      INTEGER,
			baseline_calmar        REAL,
			best_calmar            REAL,
			best_calmar_label      TEXT,
			best_resilience_score  REAL,
			best_resilience_label  TEXT,
			started_at             DATETIME,
			finished_at            DATETIME,
			duration_ms            INTEGER,
			error                  TEXT
		);
		CREATE TABLE IF NOT EXISTS gridsearch_results (
			id                 INTEGER PRIMARY KEY AUTOINCREMENT,
			strategy_id        TEXT NOT NULL,
			label              TEXT,
			is_baseline        INTEGER,
			net_profit         REAL,
			cagr               REAL,
			max_drawdown_pct   REAL,
			max_drawdown_days  INTEGER,
			calmar_ratio       REAL,
			resilience_score   REAL,
			total_trades       INTEGER,
			win_rate           REAL
		);
		CREATE INDEX IF NOT EXISTS idx_gridsearch_results_strategy ON gridsearch_results(strategy_id);
	`)
	return err
}

// isStrategyDone reports whether strategy_id has a completed ('done') sweep
// already recorded. 'running' (crashed/interrupted mid-sweep) and 'failed' are
// both treated as not-done, so a retry picks them back up.
func isStrategyDone(gdb *sqlx.DB, stratID string) bool {
	gridDBMu.Lock()
	defer gridDBMu.Unlock()
	var status string
	err := gdb.Get(&status, `SELECT status FROM gridsearch_runs WHERE strategy_id = ?`, stratID)
	return err == nil && status == "done"
}

// recordRunStart marks a strategy as in-progress before its sweep begins, so an
// interrupted process leaves a visible 'running' (not falsely 'done') row.
func recordRunStart(gdb *sqlx.DB, strat strategy.Strategy, totalPerms int) {
	gridDBMu.Lock()
	defer gridDBMu.Unlock()
	_, err := gdb.Exec(`
		INSERT INTO gridsearch_runs (strategy_id, strategy_name, status, total_permutations, started_at)
		VALUES (?, ?, 'running', ?, ?)
		ON CONFLICT(strategy_id) DO UPDATE SET
			strategy_name = excluded.strategy_name,
			status = 'running',
			total_permutations = excluded.total_permutations,
			started_at = excluded.started_at,
			finished_at = NULL,
			error = NULL
	`, strat.ID(), strat.Name(), totalPerms, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		log.Printf("Warning: failed to record run start for %s: %v", strat.ID(), err)
	}
}

// recordRun persists the final outcome of a sweep: updates the controller row in
// gridsearch_runs (status, best scores, timing) and replaces that strategy's
// rows in gridsearch_results with the freshly-evaluated set.
func recordRun(gdb *sqlx.DB, strat strategy.Strategy, outcome sweepOutcome, runErr error) {
	gridDBMu.Lock()
	defer gridDBMu.Unlock()

	status := "done"
	errMsg := ""
	if runErr != nil {
		status = "failed"
		errMsg = runErr.Error()
	} else if len(outcome.Results) == 0 {
		status = "failed"
		errMsg = "no configurations met the minimum trade count filter"
	}

	var baselineCalmar float64
	if outcome.BaselineRes != nil {
		baselineCalmar = outcome.BaselineRes.Report.CalmarRatio
	}
	var bestCalmar float64
	var bestCalmarLabel string
	if len(outcome.TopCalmar) > 0 {
		bestCalmar = outcome.TopCalmar[0].Report.CalmarRatio
		bestCalmarLabel = outcome.TopCalmar[0].Label
	}
	var bestResilience float64
	var bestResilienceLabel string
	if len(outcome.TopResilience) > 0 {
		bestResilience = resilienceScore(outcome.TopResilience[0].Report)
		bestResilienceLabel = outcome.TopResilience[0].Label
	}

	if _, err := gdb.Exec(`
		INSERT INTO gridsearch_runs (
			strategy_id, strategy_name, status, total_permutations, evaluated_configs,
			baseline_calmar, best_calmar, best_calmar_label, best_resilience_score, best_resilience_label,
			started_at, finished_at, duration_ms, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(strategy_id) DO UPDATE SET
			strategy_name = excluded.strategy_name,
			status = excluded.status,
			total_permutations = excluded.total_permutations,
			evaluated_configs = excluded.evaluated_configs,
			baseline_calmar = excluded.baseline_calmar,
			best_calmar = excluded.best_calmar,
			best_calmar_label = excluded.best_calmar_label,
			best_resilience_score = excluded.best_resilience_score,
			best_resilience_label = excluded.best_resilience_label,
			finished_at = excluded.finished_at,
			duration_ms = excluded.duration_ms,
			error = excluded.error
	`,
		strat.ID(), strat.Name(), status, outcome.TotalPerms, len(outcome.Results),
		baselineCalmar, bestCalmar, bestCalmarLabel, bestResilience, bestResilienceLabel,
		time.Now().UTC().Add(-outcome.Elapsed).Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339),
		outcome.Elapsed.Milliseconds(), errMsg,
	); err != nil {
		log.Printf("Warning: failed to record run result for %s: %v", strat.ID(), err)
	}

	if _, err := gdb.Exec(`DELETE FROM gridsearch_results WHERE strategy_id = ?`, strat.ID()); err != nil {
		log.Printf("Warning: failed to clear old results for %s: %v", strat.ID(), err)
		return
	}
	if len(outcome.Results) == 0 {
		return
	}

	tx, err := gdb.Begin()
	if err != nil {
		log.Printf("Warning: failed to begin results transaction for %s: %v", strat.ID(), err)
		return
	}
	stmt, err := tx.Prepare(`
		INSERT INTO gridsearch_results (
			strategy_id, label, is_baseline, net_profit, cagr, max_drawdown_pct,
			max_drawdown_days, calmar_ratio, resilience_score, total_trades, win_rate
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		log.Printf("Warning: failed to prepare results insert for %s: %v", strat.ID(), err)
		tx.Rollback()
		return
	}
	for _, r := range outcome.Results {
		isBaseline := 0
		if r.IsBaseline {
			isBaseline = 1
		}
		if _, err := stmt.Exec(
			strat.ID(), r.Label, isBaseline, r.Report.NetProfit, r.Report.CAGR, r.Report.MaxDrawdownPct,
			r.Report.MaxDrawdownDuration, r.Report.CalmarRatio, resilienceScore(r.Report), r.Report.TotalTrades, r.Report.WinRate,
		); err != nil {
			log.Printf("Warning: failed to insert result row for %s: %v", strat.ID(), err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		log.Printf("Warning: failed to commit results for %s: %v", strat.ID(), err)
	}
}

// printCachedResults prints a strategy's already-persisted top results (used
// when a single-strategy invocation is skipped because it's already 'done').
func printCachedResults(gdb *sqlx.DB, strat strategy.Strategy) {
	type row struct {
		Label       string  `db:"label"`
		CAGR        float64 `db:"cagr"`
		MaxDD       float64 `db:"max_drawdown_pct"`
		DDDays      int     `db:"max_drawdown_days"`
		Score       float64 `db:"resilience_score"`
		TotalTrades int     `db:"total_trades"`
	}
	var rows []row
	if err := gdb.Select(&rows, `
		SELECT label, cagr, max_drawdown_pct, max_drawdown_days, resilience_score, total_trades
		FROM gridsearch_results WHERE strategy_id = ? ORDER BY resilience_score DESC LIMIT 10
	`, strat.ID()); err != nil || len(rows) == 0 {
		return
	}
	fmt.Println("\n🛡️  Cached TOP 10 BY RESILIENCE (from a previous sweep):")
	for i, r := range rows {
		fmt.Printf("  #%d  %-50s  CAGR=%.2f%%  DD=%.2f%%  DDdays=%d  Score=%.4f  Trades=%d\n",
			i+1, r.Label, r.CAGR*100, r.MaxDD*100, r.DDDays, r.Score, r.TotalTrades)
	}
}

// runBatchSweep sweeps every strategy in targets, bounded to `concurrency`
// strategies in flight at once. Each strategy's own inner sweep runs with a
// small fixed worker count (rather than `concurrency`) since the outer pool
// already saturates available cores — oversubscribing both dimensions at once
// is what OOM-killed cmd/backtest -strategy all earlier in this project.
// Strategies already marked 'done' in the pipeline DB are skipped unless force.
func runBatchSweep(db, gdb *sqlx.DB, targets []strategy.Strategy, opts sweepOptions, concurrency int, force, noHTML bool, gridDBPath string) {
	const innerWorkersInBatchMode = 2

	var toRun []strategy.Strategy
	for _, s := range targets {
		if !force && isStrategyDone(gdb, s.ID()) {
			continue
		}
		toRun = append(toRun, s)
	}

	fmt.Printf("\n=======================================================================================================================\n")
	fmt.Printf("⚡ BATCH GRID SEARCH — %d strategies selected, %d already done in %s (skipped), %d to sweep\n",
		len(targets), len(targets)-len(toRun), gridDBPath, len(toRun))
	fmt.Printf("   Concurrency: %d strategies in flight, %d inner workers each\n", concurrency, innerWorkersInBatchMode)
	fmt.Printf("=======================================================================================================================\n\n")

	if len(toRun) == 0 {
		fmt.Println("✅ Nothing to sweep — every selected strategy already has a completed sweep. Use -force to redo.")
		printBatchSummary(gdb, targets)
		return
	}

	if concurrency < 1 {
		concurrency = 1
	}
	jobs := make(chan strategy.Strategy, len(toRun))
	for _, s := range toRun {
		jobs <- s
	}
	close(jobs)

	var wg sync.WaitGroup
	var mu sync.Mutex
	completed, failed := 0, 0

	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for strat := range jobs {
				stratOpts := opts
				stratOpts.InnerWorkers = innerWorkersInBatchMode

				paramSpace := strategy.AssessParameterSpace(strat)
				totalPerms := len(paramSpace.Symbols) * len(paramSpace.SignalDays) * len(paramSpace.HoldDays) *
					len(paramSpace.TakeProfits) * len(paramSpace.StopLosses) * len(paramSpace.Regimes) * len(paramSpace.Allocations)
				recordRunStart(gdb, strat, totalPerms)

				outcome, err := runSweep(db, strat, stratOpts)
				recordRun(gdb, strat, outcome, err)

				mu.Lock()
				if err != nil || len(outcome.Results) == 0 {
					failed++
					reason := "no configs met the minimum trade filter"
					if err != nil {
						reason = err.Error()
					}
					fmt.Printf("❌ [%s] %s\n", strat.ID(), reason)
				} else {
					completed++
					best := outcome.TopResilience[0]
					fmt.Printf("✅ [%s] %d configs evaluated in %v — best: %s (CAGR=%.2f%% Score=%.4f)\n",
						strat.ID(), len(outcome.Results), outcome.Elapsed.Round(time.Millisecond), best.Label, best.Report.CAGR*100, resilienceScore(best.Report))
					if !noHTML {
						reportFile := defaultReportPath(strat, "")
						if err := exportSweepHTML(strat, outcome, reportFile, opts.Capital); err != nil {
							log.Printf("Warning: HTML export failed for %s: %v", strat.ID(), err)
						}
					}
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	fmt.Printf("\n⚡ Batch complete: %d succeeded, %d failed/empty.\n", completed, failed)
	printBatchSummary(gdb, targets)
}

// printBatchSummary reads every targeted strategy's persisted best-by-resilience
// result back from gridsearch_runs and prints an aggregate ranking across all of
// them — the cross-strategy view a single-strategy sweep can't give you.
func printBatchSummary(gdb *sqlx.DB, targets []strategy.Strategy) {
	type row struct {
		StrategyID   string  `db:"strategy_id"`
		StrategyName string  `db:"strategy_name"`
		Status       string  `db:"status"`
		BestResScore float64 `db:"best_resilience_score"`
		BestResLabel string  `db:"best_resilience_label"`
		BestCalmar   float64 `db:"best_calmar"`
	}
	ids := make([]string, len(targets))
	args := make([]interface{}, len(targets))
	for i, s := range targets {
		ids[i] = s.ID()
		args[i] = s.ID()
	}
	if len(ids) == 0 {
		return
	}
	placeholders := ""
	for i := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
	}
	var rows []row
	query := fmt.Sprintf(`
		SELECT strategy_id, strategy_name, status, best_resilience_score, best_resilience_label, best_calmar
		FROM gridsearch_runs WHERE strategy_id IN (%s) AND status = 'done'
	`, placeholders)
	if err := gdb.Select(&rows, query, args...); err != nil || len(rows) == 0 {
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].BestResScore > rows[j].BestResScore })

	n := 20
	if len(rows) < n {
		n = len(rows)
	}
	fmt.Printf("\n🏆 TOP %d ACROSS ALL SWEPT STRATEGIES BY RESILIENCE SCORE:\n", n)
	for i, r := range rows[:n] {
		fmt.Printf("  #%2d  %-20s  Score=%.4f  Calmar=%.2f  %s\n", i+1, r.StrategyID, r.BestResScore, r.BestCalmar, r.BestResLabel)
	}
}
