package sessions

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

func TestDeriveBuildsSessionBarsFromRegularHours(t *testing.T) {
	db, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "market_history_hourly.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.EnsureBarTable(db, "backtest_start"); err != nil {
		t.Fatal(err)
	}
	// 2021-03-02 is EST: 14:00 UTC is the 9:00 bar, 20:00 UTC the 15:00 bar.
	// 13:00 UTC (8:00, pre-market) and 21:00 UTC (16:00, after hours) are not session.
	hours := []struct {
		at                     string
		open, high, low, close float64
	}{
		{"2021-03-02 13:00:00", 90, 91, 89, 90},
		{"2021-03-02 14:00:00", 100, 102, 99, 101},
		{"2021-03-02 15:00:00", 101, 105, 100, 104},
		{"2021-03-02 20:00:00", 104, 106, 103, 105},
		{"2021-03-02 21:00:00", 105, 120, 105, 118},
	}
	for _, h := range hours {
		if _, err := db.Exec(`INSERT INTO backtest_start (symbol, Date, timeframe, open, high, low, close, volume) VALUES ('AAA', ?, '1h', ?, ?, ?, ?, 10)`,
			h.at, h.open, h.high, h.low, h.close); err != nil {
			t.Fatal(err)
		}
	}
	n, err := Derive(db, "backtest_start")
	if err != nil || n != 1 {
		t.Fatalf("Derive = %d, %v; want 1 session", n, err)
	}
	var open, high, low, closep float64
	var vol int64
	if err := db.QueryRow(`SELECT open, high, low, close, volume FROM backtest_start WHERE symbol='AAA' AND timeframe='1d' AND Date='2021-03-02'`).Scan(&open, &high, &low, &closep, &vol); err != nil {
		t.Fatal(err)
	}
	if open != 100 || high != 106 || low != 99 || closep != 105 || vol != 30 {
		t.Errorf("session = o%v h%v l%v c%v v%v", open, high, low, closep, vol)
	}
	// A second pass changes nothing and adds no rows.
	if n, err := Derive(db, "backtest_start"); err != nil || n != 1 {
		t.Errorf("rederive = %d, %v", n, err)
	}
	var count int
	db.Get(&count, `SELECT COUNT(*) FROM backtest_start WHERE timeframe='1d'`)
	if count != 1 {
		t.Errorf("session rows = %d, want 1", count)
	}
}

func TestRegularHandlesDaylightSaving(t *testing.T) {
	for utc, want := range map[string]bool{
		"2021-07-01 13:00:00": true,  // EDT: 9:00
		"2021-07-01 12:00:00": false, // 8:00
		"2021-07-01 19:00:00": true,  // 15:00
		"2021-07-01 20:00:00": false, // 16:00
		"2021-12-01 14:00:00": true,  // EST: 9:00
		"2021-12-01 13:00:00": false,
	} {
		if _, ok := Regular(utc); ok != want {
			t.Errorf("Regular(%s) = %v, want %v", utc, ok, want)
		}
	}
}

// A close that moves beyond the ratio from the session before is reported, in either direction,
// and only inside the window asked for.
func TestSplitJumpsFindsAnUnadjustedSplit(t *testing.T) {
	db, err := storage.OpenSQLite(filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.EnsureBarTable(db, "backtest_start"); err != nil {
		t.Fatal(err)
	}
	var bars []models.Bar
	add := func(sym string, closes ...float64) {
		for i, c := range closes {
			bars = append(bars, models.Bar{Symbol: sym, Date: fmt.Sprintf("2026-03-%02d", 2+i), Timeframe: "1d", AssetClass: "equity",
				Open: c, High: c, Low: c, Close: c, AdjClose: c, Volume: 1})
		}
	}
	add("SPLT", 100, 101, 50.5, 51) // 2-for-1 split, unadjusted: x0.5
	add("REVS", 10, 10.1, 25, 25.2) // reverse split: x2.47
	add("CALM", 100, 103, 99, 104)  // 3% moves
	if err := storage.UpsertBars(db, "backtest_start", bars); err != nil {
		t.Fatal(err)
	}
	got, err := SplitJumps(db, []string{"SPLT", "REVS", "CALM"}, 0, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Symbol != "REVS" || got[0].Date != "2026-03-04" || got[1].Symbol != "SPLT" || got[1].Date != "2026-03-04" {
		t.Fatalf("jumps %v", got)
	}
	if got, _ := SplitJumps(db, []string{"SPLT"}, 0, "2026-03-05", ""); len(got) != 0 {
		t.Errorf("a jump before the window was reported: %v", got)
	}
}
