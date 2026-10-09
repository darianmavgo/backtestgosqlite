package markov_strategy

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/realbars"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/train"
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
	if _, err := train.TrainMarkov(context.Background(), train.MarkovConfig{MarketDB: market, ModelDB: filepath.Join(app, "data", "markov_models.db"), Symbols: []string{"GOOGL"}}); err != nil {
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

// A model trained on older data than the market database says so, and still runs.
func TestStaleModelWarns(t *testing.T) {
	app := t.TempDir()
	t.Setenv("APP_FOLDER", app)
	older := realbars.Copy(t, "GOOGL")
	db, err := storage.OpenSQLite(older)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DELETE FROM backtest_start WHERE substr(Date, 1, 10) > '2025-06-30'"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := train.TrainMarkov(context.Background(), train.MarkovConfig{MarketDB: older, ModelDB: filepath.Join(app, "data", "markov_models.db"), Symbols: []string{"GOOGL"}}); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)
	current := realbars.Copy(t, "GOOGL")
	s := &Strategy{Row: refdb.MarkovStrategy{
		ID: "markov_model_googl", Name: "GOOGL markov", SignalSymbol: "GOOGL", TradeSymbol: "GOOGL",
		Direction: "long", TargetState: "bull", HoldDays: 5, TakeProfitPct: 0.05, StopLossPct: 0.05, AllocationPct: 0.5,
	}}
	s.SetDatabases(current, filepath.Join(t.TempDir(), "calc.db"))
	if sigs := s.GenerateSignals(nil); len(sigs) == 0 {
		t.Fatal("a stale model must still produce signals")
	}
	if got := buf.String(); !strings.Contains(got, "trained through 2025-06-30") || !strings.Contains(got, "run `train markov -symbols GOOGL`") {
		t.Fatalf("no staleness warning in log: %q", got)
	}
}

// A live scan prepares each markov strategy: it trains the signal symbol through the
// latest bar when the saved model is behind, and does nothing when it is current.
func TestPrepareTrainsThroughTheLatestBarOnlyWhenBehind(t *testing.T) {
	app := t.TempDir()
	t.Setenv("APP_FOLDER", app)
	market := realbars.Copy(t, "GOOGL")
	s := &Strategy{Row: refdb.MarkovStrategy{
		ID: "markov_model_googl", Name: "x", SignalSymbol: "GOOGL", TradeSymbol: "GOOGL",
		Direction: "long", TargetState: "bull", HoldDays: 5, AllocationPct: 0.25,
	}}
	modelDB := filepath.Join(app, "data", "markov_models.db")
	last, err := storage.SymbolLastDate(market, "GOOGL")
	if err != nil || last == "" {
		t.Fatalf("last bar %q %v", last, err)
	}

	if err := s.Prepare(context.Background(), market); err != nil {
		t.Fatalf("first Prepare: %v", err)
	}
	trained, ok := ModelLastDate(modelDB, "GOOGL")
	if !ok || trained != last {
		t.Fatalf("model trained through %q (found %v), want %s", trained, ok, last)
	}
	before, err := os.Stat(modelDB)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Prepare(context.Background(), market); err != nil {
		t.Fatalf("second Prepare: %v", err)
	}
	after, _ := os.Stat(modelDB)
	if !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Fatal("a current model was trained again")
	}

	// The model now has a prediction for the latest bar, so the strategy no longer logs a missing model.
	var logs bytes.Buffer
	log.SetOutput(&logs)
	defer log.SetOutput(os.Stderr)
	s.SetDatabases(market, filepath.Join(t.TempDir(), "calc.db"))
	s.GenerateSignals(nil)
	if strings.Contains(logs.String(), "no trained model") || strings.Contains(logs.String(), "was trained through") {
		t.Fatalf("model still reported missing or behind: %s", logs.String())
	}

	if err := (&Strategy{Row: refdb.MarkovStrategy{SignalSymbol: "NOSUCH"}}).Prepare(context.Background(), market); err == nil {
		t.Fatal("a symbol with no bars must fail Prepare, not pass as a quiet day")
	}
}
