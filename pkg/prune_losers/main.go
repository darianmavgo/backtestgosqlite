// Package prune_losers deletes strategies from strategies.db whose most
// recent backtest has a win_rate below a threshold. The backtest results are
// the performance_summary tables of the result DBs in the reports folder (the
// top-level <id>.db files and the numbered pipeline run folders; oos/ is not
// read). "Most recent" is the row with the latest created_at per strategy id.
//
// Subcommand `untrainable` prunes tree strategies and models whose signal symbol
// cannot be trained on the bars before the holdout cutoff (see untrainable.go).
// Subcommand `symbols` takes the losing names out of a rotation strategy's list
// (see symbols.go).
package prune_losers

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	_ "modernc.org/sqlite"
)

// MaxWorkers caps the number of result DBs read concurrently.
const MaxWorkers = 32

// familyTables are the row-backed strategy tables of strategies.db.
var familyTables = []string{
	"streak_strategy", "hold_strategy",
	"tree_strategy", "markov_strategy", "rotation_strategy",
}

// Config holds the settings of a run.
type Config struct {
	StrategiesDB string  // -db
	ReportsDir   string  // -reports
	MinWinRate   float64 // -min-win-rate: strictly below this is a loser
	Workers      int     // -workers, capped at MaxWorkers
	DryRun       bool    // -dry-run
	Out          io.Writer
}

// result is one strategy's most recent backtest.
type result struct {
	created  string
	winRate  float64
	trades   int64
	resultDB string
}

// Main parses flags and runs.
func Main() {
	if len(os.Args) > 1 && os.Args[1] == "untrainable" {
		os.Exit(runUntrainable(os.Args[2:], os.Stdout, os.Stderr))
	}
	if len(os.Args) > 1 && os.Args[1] == "symbols" {
		os.Exit(runSymbols(os.Args[2:], os.Stdout, os.Stderr))
	}
	cfg := Config{Out: os.Stdout}
	flag.StringVar(&cfg.StrategiesDB, "db", appenv.RefDB(), "strategies database to prune")
	flag.StringVar(&cfg.ReportsDir, "reports", appenv.Reports(), "reports folder holding the result DBs")
	flag.Float64Var(&cfg.MinWinRate, "min-win-rate", 0.5, "delete strategies whose latest win_rate is below this")
	flag.IntVar(&cfg.Workers, "workers", MaxWorkers, "concurrent result DB readers (max 32)")
	flag.BoolVar(&cfg.DryRun, "dry-run", false, "report what would be deleted without deleting")
	flag.Parse()
	if flag.NArg() > 0 {
		// A stray word (a mistyped subcommand) must never fall through to a real prune.
		fmt.Fprintf(os.Stderr, "prune_losers: unexpected argument %q (subcommands: untrainable, symbols)\n", flag.Arg(0))
		os.Exit(2)
	}
	if _, err := Run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "prune_losers:", err)
		os.Exit(1)
	}
}

// ResultDBs lists the result DBs to read: reports/*.db and reports/<N>/*.db.
func ResultDBs(dir string) ([]string, error) {
	top, err := filepath.Glob(filepath.Join(dir, "*.db"))
	if err != nil {
		return nil, err
	}
	runs, err := filepath.Glob(filepath.Join(dir, "[0-9]*", "*.db"))
	if err != nil {
		return nil, err
	}
	out := append(top, runs...)
	sort.Strings(out)
	return out, nil
}

// Latest reads every result DB with up to workers goroutines and returns the
// most recent backtest per strategy id.
func Latest(paths []string, workers int) (map[string]result, error) {
	if workers < 1 {
		workers = 1
	}
	if workers > MaxWorkers {
		workers = MaxWorkers
	}
	var (
		mu     sync.Mutex
		latest = map[string]result{}
		errs   []error
		wg     sync.WaitGroup
	)
	jobs := make(chan string)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				rows, err := readSummary(p)
				mu.Lock()
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", p, err))
				}
				for id, r := range rows {
					if cur, ok := latest[id]; !ok || r.created > cur.created {
						latest[id] = r
					}
				}
				mu.Unlock()
			}
		}()
	}
	for _, p := range paths {
		jobs <- p
	}
	close(jobs)
	wg.Wait()
	if len(errs) > 0 {
		return latest, errs[0]
	}
	return latest, nil
}

// readSummary returns the newest performance_summary row per strategy in one
// result DB. A DB without the table (a scoreboard, say) yields nothing.
func readSummary(path string) (map[string]result, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='performance_summary'`).Scan(&n); err != nil || n == 0 {
		return nil, err
	}
	rows, err := db.Query(`SELECT strategy_id, COALESCE(created_at,''), win_rate, COALESCE(total_trades,0) FROM performance_summary`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]result{}
	for rows.Next() {
		var id string
		var wr sql.NullFloat64
		r := result{resultDB: path}
		if err := rows.Scan(&id, &r.created, &wr, &r.trades); err != nil {
			return nil, err
		}
		r.winRate = wr.Float64
		if !wr.Valid {
			r.trades = 0 // no win rate: treated as no trades
		}
		if cur, ok := out[id]; !ok || r.created > cur.created {
			out[id] = r
		}
	}
	return out, rows.Err()
}

// Losers returns the ids whose latest backtest made no trades, or made trades
// and had a win_rate below min. A strategy with no backtest never appears in
// latest, so it is never pruned.
func Losers(latest map[string]result, min float64) []string {
	var out []string
	for id, r := range latest {
		if r.trades == 0 || r.winRate < min {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// Run prunes strategies.db and returns the number of strategy rows deleted
// (or that would be, on a dry run).
func Run(cfg Config) (int, error) {
	if cfg.Out == nil {
		cfg.Out = io.Discard
	}
	paths, err := ResultDBs(cfg.ReportsDir)
	if err != nil {
		return 0, err
	}
	latest, err := Latest(paths, cfg.Workers)
	if err != nil {
		return 0, err
	}
	losers := Losers(latest, cfg.MinWinRate)
	fmt.Fprintf(cfg.Out, "read %d result DBs, %d strategies with a latest backtest, %d to prune (no trades, or win_rate below %.2f)\n",
		len(paths), len(latest), len(losers), cfg.MinWinRate)

	db, err := sql.Open("sqlite", cfg.StrategiesDB)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // the temp table lives on one connection
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TEMP TABLE loser(id TEXT PRIMARY KEY)`); err != nil {
		return 0, err
	}
	for _, id := range losers {
		if _, err := tx.Exec(`INSERT INTO loser(id) VALUES (?)`, id); err != nil {
			return 0, err
		}
	}
	total := 0
	for _, t := range familyTables {
		var exists int
		if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, t).Scan(&exists); err != nil {
			return 0, err
		}
		if exists == 0 {
			continue
		}
		var n int
		if cfg.DryRun {
			err = tx.QueryRow(`SELECT count(*) FROM ` + t + ` WHERE id IN (SELECT id FROM loser)`).Scan(&n)
		} else {
			var res sql.Result
			if res, err = tx.Exec(`DELETE FROM ` + t + ` WHERE id IN (SELECT id FROM loser)`); err == nil {
				var c int64
				c, err = res.RowsAffected()
				n = int(c)
			}
		}
		if err != nil {
			return 0, fmt.Errorf("%s: %w", t, err)
		}
		fmt.Fprintf(cfg.Out, "  %-20s %d\n", strings.TrimSuffix(t, "_strategy"), n)
		total += n
	}
	if cfg.DryRun {
		fmt.Fprintf(cfg.Out, "would delete %d strategies\n", total)
		return total, nil
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	fmt.Fprintf(cfg.Out, "deleted %d strategies from %s\n", total, cfg.StrategiesDB)
	return total, nil
}

var _ = strconv.Itoa
