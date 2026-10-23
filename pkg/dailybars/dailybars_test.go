package dailybars

import (
	"path/filepath"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestCompletedDropsTodayDuplicatesAndBadBars(t *testing.T) {
	bars := []models.Bar{
		{Date: "2026-10-20", Close: 100, AdjClose: 99},
		{Date: "2026-10-19", Close: 101, AdjClose: 100},
		{Date: "2026-10-19", Close: 999, AdjClose: 999}, // duplicate day
		{Date: "2026-10-21", Close: 0, AdjClose: 0},     // no close
		{Date: "2026-10-22", Close: 102, AdjClose: 101}, // after the last completed session
	}
	got := completed(bars, "2026-10-21")
	if len(got) != 2 || got[0].Date != "2026-10-19" || got[1].Date != "2026-10-20" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Close != 101 {
		t.Fatalf("kept the duplicate instead of the first bar: %+v", got[0])
	}
}

func TestReplaceSwapsOneSymbolOnly(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "daily_bars.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := Replace(db, "QQQ", []models.Bar{{Date: "2026-10-19", Close: 1, AdjClose: 1, Volume: 5}, {Date: "2026-10-20", Close: 2, AdjClose: 2, Volume: 6}}); err != nil {
		t.Fatal(err)
	}
	if err := Replace(db, "SPY", []models.Bar{{Date: "2026-10-20", Close: 9, AdjClose: 9, Volume: 1}}); err != nil {
		t.Fatal(err)
	}
	// A fresh download rescales history: the old QQQ rows must be replaced, not merged.
	if err := Replace(db, "QQQ", []models.Bar{{Date: "2026-10-20", Close: 3, AdjClose: 3, Volume: 7}}); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM daily_bar WHERE symbol = 'QQQ'`); err != nil || n != 1 {
		t.Fatalf("QQQ rows = %d, err %v; want 1", n, err)
	}
	if err := db.Get(&n, `SELECT COUNT(*) FROM daily_bar WHERE symbol = 'SPY'`); err != nil || n != 1 {
		t.Fatalf("SPY rows = %d, err %v; want 1 (untouched)", n, err)
	}
	var src string
	if err := db.Get(&src, `SELECT source FROM daily_bar WHERE symbol = 'QQQ'`); err != nil || src != Source {
		t.Fatalf("source = %q, err %v", src, err)
	}
}

func TestRunRejectsBadStart(t *testing.T) {
	if _, err := Run(t.Context(), Config{DB: filepath.Join(t.TempDir(), "x.db"), Start: "2010/01/01"}, nil); err == nil {
		t.Fatal("a malformed start date should be an error")
	}
}
