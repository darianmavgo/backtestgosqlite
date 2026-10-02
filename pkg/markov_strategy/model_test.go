package markov_strategy

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/realbars"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/train_markov"
)

func TestBacktestReadsTrainedModelAndNeverTrains(t *testing.T) {
	app := t.TempDir()
	t.Setenv("APP_FOLDER", app)
	market := realbars.Copy(t, "GOOGL")

	row := refdb.MarkovStrategy{
		ID: "markov_model_googl", Name: "GOOGL markov", SignalSymbol: "GOOGL", TradeSymbol: "GOOGL",
		Direction: "long", TargetState: "bull", HoldDays: 5, TakeProfitPct: 0.05, StopLossPct: 0.05, AllocationPct: 0.5,
	}
	run := func() []string {
		s := &Strategy{Row: row}
		s.SetDatabases(market, filepath.Join(t.TempDir(), "calc.db"))
		var dates []string
		for _, sig := range s.GenerateSignals(nil) {
			dates = append(dates, sig.Date)
		}
		return dates
	}

	// No trained model: no signals, and the model database is not created by the backtest.
	if got := run(); len(got) != 0 {
		t.Fatalf("untrained symbol produced signals %v", got)
	}

	if _, err := os.Stat(filepath.Join(app, "data", "markov_models.db")); err == nil {
		t.Fatal("the backtest created the model database")
	}

	// Train as its own step, into the path backtest reads.
	if _, err := train_markov.Train(context.Background(), train_markov.Config{MarketDB: market, ModelDB: filepath.Join(app, "data", "markov_models.db"), Symbols: []string{"GOOGL"}}); err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenSQLiteReadOnly(filepath.Join(app, "data", "markov_models.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var want []string
	if err := db.Select(&want, "SELECT date FROM markov_prediction WHERE symbol = 'GOOGL' AND state = 1 AND signal > 0.10 ORDER BY date"); err != nil {
		t.Fatal(err)
	}
	if len(want) == 0 {
		t.Fatal("the trained model has no bull entries; the series does not exercise the strategy")
	}

	got := run()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("backtest dates %v\nwant (from the persisted model) %v", got, want)
	}

	// Running the backtest leaves the model as it was.
	var n int
	if err := db.Get(&n, "SELECT COUNT(*) FROM markov_prediction"); err != nil || n != len(realbars.Closes(t, market, "GOOGL"))-20 {
		t.Fatalf("model rows after backtest: %d (err %v)", n, err)
	}
}
