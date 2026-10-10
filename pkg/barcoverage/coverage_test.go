package barcoverage

import (
	"path/filepath"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

func insertBars(t *testing.T, path string, rows [][2]string) {
	t.Helper()
	db, err := storage.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.EnsureBarTable(db, "backtest_start"); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO backtest_start (symbol, Date, open, high, low, close, volume) VALUES (?, ?, 1, 1, 1, 1, 1)`, r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCoverageDailyAndHourly(t *testing.T) {
	dir := t.TempDir()
	daily := filepath.Join(dir, "market_history.db")
	hourly := filepath.Join(dir, "market_history_hourly.db")
	days := []string{"2021-01-04", "2021-01-05", "2021-01-06", "2021-01-07", "2021-01-08"}
	var rows [][2]string
	for _, d := range days {
		rows = append(rows, [2]string{"SPY", d})
	}
	// AAA misses the 5th and 6th; BBB lists on the 6th.
	for _, d := range []string{"2021-01-04", "2021-01-07", "2021-01-08"} {
		rows = append(rows, [2]string{"AAA", d})
	}
	for _, d := range days[2:] {
		rows = append(rows, [2]string{"BBB", d})
	}
	insertBars(t, daily, rows)
	insertBars(t, hourly, [][2]string{
		{"AAA", "2021-01-04 14:00:00"}, {"AAA", "2021-01-04 15:00:00"}, {"AAA", "2021-01-08 14:00:00"},
		{"BBB", "2021-01-06 14:00:00"},
	})

	got, err := Coverage(CoverageOptions{Symbols: []string{"AAA", "BBB", "ZZZ"}, DailyDB: daily, BarDB: daily, Start: "2021-01-01"})
	if err != nil {
		t.Fatal(err)
	}
	a, b, z := got[0], got[1], got[2]
	// AAA's own window is 01-04..01-08, 5 sessions, 3 with bars, the other 2 adjacent.
	if a.Expected != 5 || a.Have != 3 || a.Missing != 2 || a.LongestGap != 2 {
		t.Errorf("AAA daily = %+v", a)
	}
	// BBB is only expected from its listing.
	if b.Expected != 3 || b.Missing != 0 || b.Pct() != 100 {
		t.Errorf("BBB daily = %+v", b)
	}
	if z.Note != "no daily bars in window" {
		t.Errorf("ZZZ = %+v", z)
	}

	got, err = Coverage(CoverageOptions{Symbols: []string{"AAA", "BBB"}, DailyDB: daily, BarDB: hourly, Hourly: true, Start: "2021-01-01"})
	if err != nil {
		t.Fatal(err)
	}
	a, b = got[0], got[1]
	// AAA's daily sessions are 01-04, 01-07, 01-08; hourly covers 01-04 and 01-08.
	if a.Expected != 5 || a.Have != 2 || a.Bars != 3 || a.BarsPerDay != 1.5 {
		t.Errorf("AAA hourly = %+v", a)
	}
	if b.Have != 1 || b.Missing != 2 || b.LongestGap != 2 {
		t.Errorf("BBB hourly = %+v", b)
	}
}
