package hold_strategy

import (
	"path/filepath"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/realbars"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

func signalDates(t *testing.T, row refdb.HoldStrategy) ([]string, []string, []float64) {
	t.Helper()
	market := realbars.Copy(t, "GOOGL")
	db, err := storage.OpenSQLite(market)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bars, _, err := storage.FetchBars(db, "backtest_start", []string{"GOOGL"}, "2021-01-01", "")
	if err != nil {
		t.Fatal(err)
	}
	s := &Strategy{Row: row}
	s.SetDatabases(market, filepath.Join(t.TempDir(), "calc.db"))
	var got, dates []string
	var closes []float64
	for _, b := range bars["GOOGL"] {
		dates = append(dates, b.Date[:10])
		closes = append(closes, b.Close)
	}
	for _, sig := range s.GenerateSignals(bars) {
		got = append(got, sig.Date)
	}
	return got, dates, closes
}

// A row that never bails and never re-enters is buy and hold: one entry, on the first bar.
func TestHoldNeverBailIsOneEntryOnFirstBar(t *testing.T) {
	got, dates, _ := signalDates(t, refdb.HoldStrategy{ID: "t", Name: "t", Symbol: "GOOGL", AllocationPct: 1})
	if len(got) != 1 || got[0] != dates[0] {
		t.Fatalf("got %v, want one entry on %s", got, dates[0])
	}
}

// With a re-entry average the entries are the first bar plus every bar after the
// average is full whose close is above it, checked here against a literal loop.
func TestHoldReentryEntriesMatchSMA(t *testing.T) {
	const period = 20
	got, dates, closes := signalDates(t, refdb.HoldStrategy{ID: "t", Name: "t", Symbol: "GOOGL", AllocationPct: 1, TrailingStopPct: 0.07, SMAReentryPeriod: period})
	want := []string{dates[0]}
	for i := period; i < len(closes); i++ {
		sum := 0.0
		for _, c := range closes[i-period+1 : i+1] {
			sum += c
		}
		if closes[i] > sum/period && dates[i] != dates[0] {
			want = append(want, dates[i])
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d: got %s, want %s", i, got[i], want[i])
		}
	}
}

func TestHoldConfigOnlyTrailsWhenSet(t *testing.T) {
	plain := (&Strategy{Row: refdb.HoldStrategy{ID: "p", Symbol: "VOO", AllocationPct: 1}}).DefaultConfig()
	bail := (&Strategy{Row: refdb.HoldStrategy{ID: "b", Symbol: "VOO", AllocationPct: 1, TrailingStopPct: 0.07, SMAReentryPeriod: 20}}).DefaultConfig()
	if plain.UseTrailingStop || plain.TrailingStopPct != 0 {
		t.Errorf("plain hold trails: %+v", plain)
	}
	if !bail.UseTrailingStop || bail.TrailingStopPct != 0.07 {
		t.Errorf("bail hold does not trail: %+v", bail)
	}
}
