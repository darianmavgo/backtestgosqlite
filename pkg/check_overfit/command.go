package check_overfit

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	_ "modernc.org/sqlite"
)

// Config is the CLI surface.
type Config struct {
	DB       string
	RunID    int    // run folder holding walk_forward.db when DB is empty; 0 = the latest run
	Strategy string // comma-separated ids to show; empty or "all" shows every strategy in the file
	Gates    Gates
}

// Main parses flags and runs. It returns errors instead of exiting.
//
// Deprecated: check_overfit is the second half of `validate`. Use `validate verdict`.
func Main(out io.Writer) error {
	var conf Config
	flag.StringVar(&conf.DB, "db", "", "SQLite file written by walk_forward (default: walk_forward.db in the run folder)")
	flag.IntVar(&conf.RunID, "run-id", 0, "run folder under data/reports holding walk_forward.db (0 = the latest run)")
	flag.StringVar(&conf.Strategy, "strategy", "", "comma-separated strategy ids to show (default: every strategy in the file)")
	flag.IntVar(&conf.Gates.MinOOSTrades, "min-oos-trades", 8, "below this many out-of-sample trades the verdict is INSUFFICIENT")
	flag.IntVar(&conf.Gates.TrialCutoff, "trial-cutoff", 20, "parameter combinations at or above this can be CURVE_FIT when the out-of-sample Sharpe collapses")
	flag.Float64Var(&conf.Gates.Decay, "decay", 0.25, "out-of-sample Sharpe below this fraction of in-sample Sharpe is DECAYS (and CURVE_FIT when trials are high)")
	flag.Parse()
	return Report(context.Background(), out, conf)
}

// Report prints the verdict of every strategy in the walk-forward database: the
// file named by conf.DB, otherwise walk_forward.db in the run folder conf.RunID
// (the latest run when 0). It is what validate runs after the folds.
func Report(ctx context.Context, out io.Writer, conf Config) error {
	if conf.DB == "" {
		path, _, err := storage.RunFile(appenv.Reports(), conf.RunID, false, "walk_forward.db")
		if err != nil {
			return fmt.Errorf("check_overfit: %w (run validate first)", err)
		}
		conf.DB = path
	}
	db, err := sql.Open("sqlite", conf.DB)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := Run(ctx, db, conf.Gates)
	if err != nil {
		return err
	}
	rows = Only(rows, conf.Strategy)
	if len(rows) == 0 {
		return fmt.Errorf("check_overfit: walk_forward_summary has no strategies")
	}
	fmt.Fprintf(out, "db: %s\n", conf.DB)
	Write(out, rows)
	fmt.Fprintln(out, "verdicts: HOLDS, DECAYS, CURVE_FIT, INSUFFICIENT")
	return nil
}
