package runner

import (
	"strings"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

func engineBars() ([]models.Bar, []string) {
	closes := []float64{100, 99, 98, 103, 104, 105, 106, 107, 108, 109, 110, 111}
	dates := []string{"2026-01-02", "2026-01-05", "2026-01-06", "2026-01-07", "2026-01-08", "2026-01-09",
		"2026-01-12", "2026-01-13", "2026-01-14", "2026-01-15", "2026-01-16", "2026-01-20"}
	var bars []models.Bar
	for i, c := range closes {
		bars = append(bars, models.Bar{Symbol: "AAA", Date: dates[i], Open: c, High: c + 0.5, Low: c - 0.5, Close: c, AdjClose: c, Volume: 1000})
	}
	return bars, dates
}

func runEngine(t *testing.T, margin bool) RunResult {
	t.Helper()
	bars, dates := engineBars()
	market := marketDB(t, map[string][]models.Bar{"AAA": bars})
	strat := streakRow("engine_test", "AAA", 1, 0.5, 0.05, 0.5)
	cfg := strat.DefaultConfig()
	cfg.UseMargin = margin
	res := ExecuteStrategy(strat, cfg, map[string][]models.Bar{"AAA": bars}, dates, 100000, "", t.TempDir(), market)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	return res
}

func storedEngine(t *testing.T, res RunResult) string {
	t.Helper()
	db, err := storage.OpenSQLiteReadOnly(res.DbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var engine *string
	if err := db.Get(&engine, `SELECT engine FROM runs WHERE run_id = ?`, res.RunID); err != nil {
		t.Fatal(err)
	}
	if engine == nil {
		return ""
	}
	return *engine
}

// A backtest runs on the hourly simulator and the stored run says so.
func TestBacktestUsesTheHourlySimulatorAndRecordsIt(t *testing.T) {
	res := runEngine(t, false)
	if res.Engine != storage.EngineHourly {
		t.Fatalf("engine %q, want %q", res.Engine, storage.EngineHourly)
	}
	if got := storedEngine(t, res); got != storage.EngineHourly {
		t.Fatalf("stored engine %q", got)
	}
	if len(res.Trades) == 0 {
		t.Fatal("no trades; the series does not exercise the engine")
	}
	for _, tr := range res.Trades {
		if tr.EntryDate == tr.ExitDate && tr.ExitReason == models.ExitReasonProfitTarget {
			t.Errorf("a daily-bar run booked a same-session target: %+v", tr)
		}
	}
}

// A setting the hourly simulator does not model is run on the daily one, and that is recorded
// too, so a result is never passed off as hourly.
func TestUnsupportedSettingFallsBackToDailyAndRecordsIt(t *testing.T) {
	res := runEngine(t, true)
	if res.Engine != storage.EngineDaily {
		t.Fatalf("engine %q, want %q", res.Engine, storage.EngineDaily)
	}
	if got := storedEngine(t, res); got != storage.EngineDaily {
		t.Fatalf("stored engine %q", got)
	}
}

func storedColumn(t *testing.T, res RunResult, col string) string {
	t.Helper()
	db, err := storage.OpenSQLiteReadOnly(res.DbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v *string
	if err := db.Get(&v, `SELECT `+col+` FROM runs WHERE run_id = ?`, res.RunID); err != nil {
		t.Fatal(err)
	}
	if v == nil {
		return ""
	}
	return *v
}

// A run that traded through an unadjusted split is flagged, and the flag is stored with the run.
func TestRunThroughAnUnadjustedSplitIsSuspect(t *testing.T) {
	bars, dates := engineBars()
	for i := 3; i < len(bars); i++ { // a 2-for-1 split that was never adjusted for
		bars[i].Open, bars[i].High, bars[i].Low, bars[i].Close, bars[i].AdjClose = bars[i].Open/2, bars[i].High/2, bars[i].Low/2, bars[i].Close/2, bars[i].Close/2
	}
	market := marketDB(t, map[string][]models.Bar{"AAA": bars})
	strat := streakRow("split_test", "AAA", 1, 0.5, 0.05, 0.5)
	res := ExecuteStrategy(strat, strat.DefaultConfig(), map[string][]models.Bar{"AAA": bars}, dates, 100000, "", t.TempDir(), market)
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if !strings.Contains(res.Suspect, "AAA 2026-01-07") {
		t.Fatalf("suspect %q, want the split on 2026-01-07 named", res.Suspect)
	}
	if got := storedColumn(t, res, "suspect"); got != res.Suspect {
		t.Fatalf("stored suspect %q, want %q", got, res.Suspect)
	}
}

func TestACleanRunIsNotSuspect(t *testing.T) {
	if res := runEngine(t, false); res.Suspect != "" {
		t.Fatalf("suspect %q on clean bars", res.Suspect)
	}
}
