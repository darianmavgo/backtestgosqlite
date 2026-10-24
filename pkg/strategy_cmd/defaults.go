package strategy_cmd

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

// defaultsColumns are the columns of a strategy row a sweep varies in every row family, and so
// the ones `strategy defaults` copies from a gridsearch_results row.
var defaultsColumns = []string{"hold_days", "take_profit_pct", "stop_loss_pct"}

// defaultsTables are the row-family tables `strategy defaults` looks the id up in.
var defaultsTables = []string{"streak_strategy", "hold_strategy", "tree_strategy", "markov_strategy", "hmm_strategy", "rotation_strategy"}

type sweepRow struct {
	ID         int64   `db:"id"`
	StrategyID string  `db:"strategy_id"`
	Label      string  `db:"label"`
	HoldDays   int     `db:"hold_days"`
	TakeProfit float64 `db:"take_profit_pct"`
	StopLoss   float64 `db:"stop_loss_pct"`
}

// defaultsMain is `strategy defaults`.
func defaultsMain() {
	id := flag.String("id", "", "strategy id whose row gets the parameters")
	label := flag.String("label", "", "label of the gridsearch_results row to copy, as gridsearch prints it, like TECL/Hold-8d/TP+0%/SL-2%")
	grid := flag.String("gridsearch-db", "", "gridsearch.db holding that row, usually data/reports/<run>/gridsearch.db")
	ref := flag.String("ref", appenv.RefDB(), "reference database whose strategy row is changed")
	dryRun := flag.Bool("dry-run", false, "show the change and write nothing")
	flag.Parse()
	if err := SetDefaults(os.Stdout, *ref, *grid, *id, *label, *dryRun); err != nil {
		log.Fatal(err)
	}
}

func looseID(s string) string {
	return strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(s))
}

// SetDefaults makes the hold days, take-profit and stop-loss of the gridsearch_results row with
// the given label the
// stored defaults of the strategy id in the reference database at refPath. The row must belong to
// that strategy. Nothing else in the strategy row changes. With dryRun it only reports.
func SetDefaults(out io.Writer, refPath, gridPath, id, label string, dryRun bool) error {
	label = strings.TrimSpace(label)
	if id == "" || gridPath == "" || label == "" {
		return fmt.Errorf("defaults needs -id <strategy>, -label <gridsearch_results label> and -gridsearch-db <path>")
	}
	gdb, err := storage.OpenSQLiteReadOnly(gridPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", gridPath, err)
	}
	defer gdb.Close()
	var found []sweepRow
	if err := gdb.Select(&found, `SELECT id, strategy_id, label, hold_days, take_profit_pct, stop_loss_pct FROM gridsearch_results WHERE label = ? ORDER BY id`, label); err != nil {
		return fmt.Errorf("read %s: %w", gridPath, err)
	}
	var mine []sweepRow
	for _, f := range found {
		if looseID(f.StrategyID) == looseID(id) {
			mine = append(mine, f)
		}
	}
	if len(mine) == 0 {
		if len(found) > 0 {
			return fmt.Errorf("label %q in %s is for strategy %q, not %q", label, gridPath, found[0].StrategyID, id)
		}
		return fmt.Errorf("no gridsearch_results row labelled %q in %s", label, gridPath)
	}
	r := mine[0]
	for _, m := range mine[1:] {
		if m.HoldDays != r.HoldDays || m.TakeProfit != r.TakeProfit || m.StopLoss != r.StopLoss {
			return fmt.Errorf("label %q appears %d times for %s in %s with different parameters", label, len(mine), id, gridPath)
		}
	}

	rdb, err := refdb.Open(refPath)
	if err != nil {
		return err
	}
	defer rdb.Close()
	table, stored, err := findStrategyRow(rdb, id)
	if err != nil {
		return err
	}
	var cols []string
	if err := rdb.Select(&cols, `SELECT name FROM pragma_table_info('`+table+`')`); err != nil {
		return err
	}
	have := map[string]bool{}
	for _, c := range cols {
		have[c] = true
	}
	for _, c := range defaultsColumns {
		if !have[c] {
			return fmt.Errorf("%s has no %s column, so a sweep row cannot be copied into it", table, c)
		}
	}

	var cur sweepRow
	if err := rdb.Get(&cur, `SELECT hold_days, take_profit_pct, stop_loss_pct FROM `+table+` WHERE id = ?`, stored); err != nil {
		return err
	}
	fmt.Fprintf(out, "%s in %s (%s from %s)\n", stored, table, r.Label, gridPath)
	fmt.Fprintf(out, "  hold_days        %d -> %d\n", cur.HoldDays, r.HoldDays)
	fmt.Fprintf(out, "  take_profit_pct  %g -> %g\n", cur.TakeProfit, r.TakeProfit)
	fmt.Fprintf(out, "  stop_loss_pct    %g -> %g\n", cur.StopLoss, r.StopLoss)
	if cur.HoldDays == r.HoldDays && cur.TakeProfit == r.TakeProfit && cur.StopLoss == r.StopLoss {
		fmt.Fprintln(out, "no change")
		return nil
	}
	if dryRun {
		fmt.Fprintln(out, "dry run: nothing written")
		return nil
	}
	if _, err := rdb.Exec(`UPDATE `+table+` SET hold_days = ?, take_profit_pct = ?, stop_loss_pct = ? WHERE id = ?`, r.HoldDays, r.TakeProfit, r.StopLoss, stored); err != nil {
		return err
	}
	fmt.Fprintf(out, "written to %s. The seed file that created the row (sql/seed) still holds the old values: update it too, and re-export the bundle if the strategy is deployed.\n", refPath)
	return nil
}

// findStrategyRow returns the table and stored id of the strategy row id, matching loosely on
// case, "-", "_" and spaces as strategy lookup does.
func findStrategyRow(db *sqlx.DB, id string) (table, stored string, err error) {
	for _, t := range defaultsTables {
		if got, ok := refdb.CanonicalID(db, t, id); ok {
			return t, got, nil
		}
	}
	return "", "", fmt.Errorf("no strategy row %q in %s", id, strings.Join(defaultsTables, ", "))
}
