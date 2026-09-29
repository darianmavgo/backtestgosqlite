package check_overfit

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"

	_ "modernc.org/sqlite"
)

// Config is the CLI surface.
type Config struct {
	DB    string
	Gates Gates
}

// Main parses flags and runs. It returns errors instead of exiting.
func Main(out io.Writer) error {
	var conf Config
	flag.StringVar(&conf.DB, "db", "reports/walk_forward.db", "SQLite file written by walk_forward")
	flag.IntVar(&conf.Gates.MinOOSTrades, "min-oos-trades", 8, "below this many out-of-sample trades the verdict is INSUFFICIENT")
	flag.IntVar(&conf.Gates.TrialCutoff, "trial-cutoff", 20, "parameter combinations at or above this can be CURVE_FIT when the out-of-sample Sharpe collapses")
	flag.Float64Var(&conf.Gates.Decay, "decay", 0.25, "out-of-sample Sharpe below this fraction of in-sample Sharpe is DECAYS (and CURVE_FIT when trials are high)")
	flag.Parse()
	db, err := sql.Open("sqlite", conf.DB)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := Run(context.Background(), db, conf.Gates)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("check_overfit: walk_forward_summary has no strategies")
	}
	fmt.Fprintf(out, "db: %s\n", conf.DB)
	Write(out, rows)
	fmt.Fprintln(out, "verdicts: HOLDS, DECAYS, CURVE_FIT, INSUFFICIENT")
	return nil
}
