package gridsearch

import (
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

// marketWith writes a real market database with literal VOO and TQQQ closes.
func marketWith(t *testing.T, voo, tqqq []float64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "market.db")
	db, err := storage.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE backtest_start (idx INTEGER, symbol TEXT, Date TEXT, open REAL, high REAL, low REAL, close REAL, volume INTEGER, "Adj Close" REAL)`); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for sym, closes := range map[string][]float64{"VOO": voo, "TQQQ": tqqq} {
		for i, c := range closes {
			d := day.AddDate(0, 0, i).Format("2006-01-02")
			if _, err := db.Exec(`INSERT INTO backtest_start VALUES (?, ?, ?, ?, ?, ?, ?, 1000, ?)`, i, sym, d, c, c+1, c-1, c, c); err != nil {
				t.Fatal(err)
			}
		}
	}
	return path
}


func entryDates(t *testing.T, market, direction, start string, days int, regime string) []string {
	t.Helper()
	entries, err := buildStreakEntries(market, "t", "VOO", direction, start, []string{"TQQQ"}, []int{days}, []string{regime})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, s := range entries[streakEntryKey{"TQQQ", days, regime}] {
		out = append(out, s.Date)
	}
	return out
}

func TestBuildStreakEntries(t *testing.T) {
	// VOO down-streak length by day index: 0,1,2,0,1,2,3,0 ; up-streak: 0,0,0,1,0,0,0,1.
	voo := []float64{10, 9, 8, 9, 8, 7, 6, 7}
	tqqq := []float64{100, 90, 80, 90, 80, 70, 60, 70}
	m := marketWith(t, voo, tqqq)

	if got, want := entryDates(t, m, "drop", "", 2, "All Regimes"), []string{"2024-01-03", "2024-01-06", "2024-01-07"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("drop 2d: got %v want %v", got, want)
	}
	if got, want := entryDates(t, m, "drop", "", 3, "All Regimes"), []string{"2024-01-07"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("drop 3d: got %v want %v", got, want)
	}
	if got, want := entryDates(t, m, "rally", "", 1, "All Regimes"), []string{"2024-01-04", "2024-01-08"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("rally 1d: got %v want %v", got, want)
	}
	if got := entryDates(t, m, "rally", "", 2, "All Regimes"); len(got) != 0 {
		t.Fatalf("rally 2d: got %v want none", got)
	}

	// Every drop-2d bar closes below its running 50-bar mean, so >=SMA50 keeps none and <SMA50 keeps all.
	if got := entryDates(t, m, "drop", "", 2, "VOO>=SMA50"); len(got) != 0 {
		t.Fatalf(">=SMA50: got %v want none", got)
	}
	if got := entryDates(t, m, "drop", "", 2, "VOO<SMA50"); len(got) != 3 {
		t.Fatalf("<SMA50: got %v want 3", got)
	}

	// Bars before the start date do not count toward a streak: the run 2024-01-05..07 restarts at 05.
	if got, want := entryDates(t, m, "drop", "2024-01-05", 2, "All Regimes"), []string{"2024-01-07"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("start 01-05, drop 2d: got %v want %v", got, want)
	}
}

func TestStreakSignalsForPricesExitsAbsolute(t *testing.T) {
	m := marketWith(t, []float64{10, 9, 8, 9, 8, 7, 6, 7}, []float64{100, 90, 80, 90, 80, 70, 60, 70})
	entries, err := buildStreakEntries(m, "t", "VOO", "drop", "", []string{"TQQQ"}, []int{3}, []string{"All Regimes"})
	if err != nil {
		t.Fatal(err)
	}
	sigs := streakSignalsFor(entries[streakEntryKey{"TQQQ", 3, "All Regimes"}], 0.10, 0.20, 15, "TQQQ-opt")
	if len(sigs) != 1 {
		t.Fatalf("got %d signals", len(sigs))
	}
	s := sigs[0]
	if s.Close != 60 || math.Abs(s.TakeProfit-66) > 1e-9 || math.Abs(s.StopLoss-48) > 1e-9 || s.HoldDaysOverride != 15 || s.Entry != 1 || s.StrategyID != "TQQQ-opt" {
		t.Fatalf("signal %+v", s)
	}
}
