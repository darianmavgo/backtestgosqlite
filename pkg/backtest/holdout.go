package backtest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/runner"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

// DefaultHoldoutMonths is how much of the end of history a plain run or a
// stack-eval keeps out of the main pass. Selection, ranking and tuning see
// only the earlier data; the held-out months are run once, afterwards, with
// the strategy or stack that pass produced.
const DefaultHoldoutMonths = 12

// minInSampleDays is the shortest in-sample window worth reporting. With less
// history than this plus the holdout, the run uses all history.
const minInSampleDays = 365

// stackEvalOutcome is what stack-eval found, for the out-of-sample pass.
type stackEvalOutcome struct {
	StackID      string // a+b+c, empty when no stack was built
	DefaultAsset string // park symbol applied to every run, if any
}

func holdoutApplies(conf Config) bool {
	if conf.HoldoutMonths <= 0 || conf.List || conf.SignalsOnly {
		return false
	}
	return conf.Mode == "" || conf.Mode == "stack-eval"
}

// lastBarDate returns the newest daily bar date in the market DB.
func lastBarDate(conf Config) (string, error) {
	db, err := storage.OpenSQLite(conf.Db)
	if err != nil {
		return "", err
	}
	defer db.Close()
	if err := storage.ValidateTableName(conf.Table); err != nil {
		return "", err
	}
	var last string
	q := fmt.Sprintf("SELECT COALESCE(MAX(substr(Date,1,10)),'') FROM %s WHERE length(Date) = 10", conf.Table)
	if err := db.Get(&last, q); err != nil {
		return "", err
	}
	return last, nil
}

// holdoutSplit returns the last in-sample date and the first out-of-sample
// date. ok is false when history is too short to split.
func holdoutSplit(conf Config) (inEnd, oosStart string, ok bool, err error) {
	last := conf.End
	if last == "" {
		if last, err = lastBarDate(conf); err != nil {
			return "", "", false, err
		}
	}
	lastT, perr := time.Parse("2006-01-02", last)
	if perr != nil {
		return "", "", false, nil
	}
	cut := lastT.AddDate(0, -conf.HoldoutMonths, 0)
	startT, perr := time.Parse("2006-01-02", conf.Start)
	if conf.Start != "" && perr == nil && cut.Sub(startT) < minInSampleDays*24*time.Hour {
		return "", "", false, nil
	}
	return cut.Format("2006-01-02"), cut.AddDate(0, 0, 1).Format("2006-01-02"), true, nil
}

// oosPath puts an output file beside the original with an _oos suffix.
func oosPath(p string) string {
	if p == "" {
		return p
	}
	ext := filepath.Ext(p)
	return strings.TrimSuffix(p, ext) + "_oos" + ext
}

func banner(title string) {
	fmt.Printf("\n%s\n%s\n%s\n", strings.Repeat("#", 100), title, strings.Repeat("#", 100))
}

// Run executes the command with cfg. It returns errors instead of exiting.
//
// By default the last HoldoutMonths of history are held out: the main pass
// runs through the cutoff, then the same strategy (or the stack stack-eval
// built) runs once on the held-out months, flat at the start, with results in
// <out-dir>/oos. Pass -holdout-months 0 to simulate all history in one pass.
func Run(conf Config) error {
	if conf.Serial {
		conf.Concurrency = 1
	}
	runner.KeepCalc = conf.KeepCalc
	if !holdoutApplies(conf) {
		return runOnce(conf)
	}
	inEnd, oosStart, ok, err := holdoutSplit(conf)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Printf("\nNot enough history to hold out %d months; running on all of it.\n", conf.HoldoutMonths)
		return runOnce(conf)
	}

	in := conf
	in.End = inEnd
	var outcome stackEvalOutcome
	in.outcome = &outcome
	banner(fmt.Sprintf("IN-SAMPLE: %s → %s   (last %d months held out; use this window to select and tune)", conf.Start, inEnd, conf.HoldoutMonths))
	if err := runOnce(in); err != nil {
		return err
	}

	oos := conf
	oos.Start = oosStart
	oos.OutDir = filepath.Join(conf.OutDir, "oos")
	oos.Html = oosPath(conf.Html)
	oos.outcome = nil
	if conf.Mode == "stack-eval" {
		if outcome.StackID == "" {
			fmt.Println("\nstack-eval built no stack, so there is nothing to test out of sample.")
			return nil
		}
		oos.Mode = ""
		oos.Strategy = outcome.StackID
		oos.Primary, oos.Secondary, oos.Args = "", "", nil
		oos.DefaultAsset = outcome.DefaultAsset
	}
	if err := os.MkdirAll(oos.OutDir, 0o755); err != nil {
		return err
	}
	banner(fmt.Sprintf("OUT-OF-SAMPLE: %s → %s   (held out; run once, flat at the start, not used for selection)", oosStart, lastOrLatest(conf.End)))
	return runOnce(oos)
}

func lastOrLatest(end string) string {
	if end == "" {
		return "latest bar"
	}
	return end
}
