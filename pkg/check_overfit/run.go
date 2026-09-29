package check_overfit

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"strings"

	sqlfiles "github.com/darianmavgo/backtestgosqlite/sql"
)

// Gates are the thresholds the verdict SQL reads from check_overfit_gate.
// Zero values fall back to 8 out-of-sample trades, 20 trials, and a 0.25
// decay ratio (out-of-sample Sharpe below a quarter of in-sample).
type Gates struct {
	MinOOSTrades int
	TrialCutoff  int
	Decay        float64
}

func (g Gates) norm() Gates {
	if g.MinOOSTrades < 1 {
		g.MinOOSTrades = 8
	}
	if g.TrialCutoff < 1 {
		g.TrialCutoff = 20
	}
	if g.Decay <= 0 || g.Decay >= 1 {
		g.Decay = 0.25
	}
	return g
}

// Verdict is one strategy's curve-fit reading of its walk-forward summary.
type Verdict struct {
	StrategyID   string
	Folds        int
	ISSharpe     float64
	OOSSharpe    float64
	ISReturnPct  float64
	OOSReturnPct float64
	OOSTrades    int
	OOSPositive  int
	Trials       int
	Verdict      string
}

// Run writes check_overfit from walk_forward_summary. The summary is produced
// by walk_forward; this command does not re-simulate.
func Run(ctx context.Context, db *sql.DB, gates Gates) ([]Verdict, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'walk_forward_summary'`).Scan(&name)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("check_overfit: walk_forward_summary is missing; run walk_forward first")
	}
	if err != nil {
		return nil, err
	}
	gates = gates.norm()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS check_overfit_gate (
		min_oos_trades INTEGER NOT NULL,
		trial_cutoff INTEGER NOT NULL,
		decay REAL NOT NULL
	)`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`DELETE FROM check_overfit_gate`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`INSERT INTO check_overfit_gate (min_oos_trades, trial_cutoff, decay) VALUES (?, ?, ?)`,
		gates.MinOOSTrades, gates.TrialCutoff, gates.Decay); err != nil {
		return nil, err
	}
	script, err := sqlfiles.Validation.ReadFile("validation/check_overfit.sql")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(string(script)); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT strategy_id, folds, is_sharpe, oos_sharpe, is_return_pct, oos_return_pct,
		oos_trades, oos_positive_folds, trials, verdict FROM check_overfit ORDER BY strategy_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Verdict
	for rows.Next() {
		var v Verdict
		if err := rows.Scan(&v.StrategyID, &v.Folds, &v.ISSharpe, &v.OOSSharpe, &v.ISReturnPct, &v.OOSReturnPct,
			&v.OOSTrades, &v.OOSPositive, &v.Trials, &v.Verdict); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Format renders the verdict table.
func Format(rows []Verdict) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-28s %-14s %7s %7s %6s %6s %s\n",
		"strategy", "verdict", "is_shp", "oos_shp", "trials", "oos_n", "oos folds up")
	for _, r := range rows {
		fmt.Fprintf(&b, "%-28s %-14s %7.2f %7.2f %6d %6d %d/%d\n",
			r.StrategyID, r.Verdict, r.ISSharpe, r.OOSSharpe, r.Trials, r.OOSTrades, r.OOSPositive, r.Folds)
	}
	return b.String()
}

// Write prints rows to out.
func Write(out io.Writer, rows []Verdict) {
	fmt.Fprint(out, Format(rows))
}
