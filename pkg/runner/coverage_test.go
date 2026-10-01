package runner

import (
	"path/filepath"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

func TestScanKeepsStandaloneResultWhenSharedFileRepeatsTheID(t *testing.T) {
	dir := t.TempDir()
	own := filepath.Join(dir, "sig-x.db")
	shared := filepath.Join(dir, "shared_sig-x_other.db")

	writeSummary := func(path, id string, cagr float64, curve []models.DailyEquityPoint) {
		t.Helper()
		db, err := storage.OpenSQLite(path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		rep := models.PerformanceReport{CAGR: cagr, TotalTrades: 3}
		if err := storage.SavePerformanceReport(db, id, rep); err != nil {
			t.Fatal(err)
		}
		if err := storage.SaveEquityCurve(db, id, curve); err != nil {
			t.Fatal(err)
		}
	}

	flat := models.DailyEquityPoint{Date: "2024-01-02", Cash: 100, PositionsValue: 0, TotalEquity: 100}
	held := models.DailyEquityPoint{Date: "2024-01-03", Cash: 40, PositionsValue: 60, TotalEquity: 100}
	writeSummary(own, "sig-x", 0.10, []models.DailyEquityPoint{
		{Date: "2024-01-01", Cash: 100, PositionsValue: 0, TotalEquity: 100},
		flat,
		held,
	})
	writeSummary(shared, "sig-x", 0.90, []models.DailyEquityPoint{held})
	writeSummary(shared, "sig-x+other", 0.50, []models.DailyEquityPoint{flat, held})

	by, _, _, _, _ := ScanAndValidate(dir, 1)
	got, ok := by["sig-x"]
	if !ok {
		t.Fatal("sig-x missing")
	}
	if got.DbPath != own {
		t.Fatalf("path %s, want standalone %s", got.DbPath, own)
	}
	if got.Report.CAGR != 0.10 {
		t.Fatalf("cagr %v, want standalone 0.10", got.Report.CAGR)
	}
	if !got.Report.IdleKnown || got.Report.IdleDays != 2 {
		t.Fatalf("idle days %d known %v, want 2 from the standalone curve", got.Report.IdleDays, got.Report.IdleKnown)
	}
	if _, ok := by["sig-x+other"]; ok {
		t.Fatal("combined shared-account id should not enter the scoreboard map")
	}
}
