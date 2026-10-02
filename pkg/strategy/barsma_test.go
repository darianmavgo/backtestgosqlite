package strategy

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

func TestLoadBarSMAFromSliceTable(t *testing.T) {
	dir := t.TempDir()
	market := filepath.Join(dir, "market.db")
	calc := filepath.Join(dir, "calc.db")

	mdb, err := storage.OpenSQLite(market)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mdb.Exec(`CREATE TABLE backtest_start (idx INTEGER, symbol TEXT, Date TEXT, open REAL, high REAL, low REAL, close REAL, volume INTEGER)`); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	var bars []models.Bar
	for i := 1; i <= 260; i++ {
		d := day.AddDate(0, 0, i).Format("2006-01-02")
		if _, err := mdb.Exec(`INSERT INTO backtest_start VALUES (?, 'ZZZ', ?, ?, ?, ?, ?, 1000)`, i, d, float64(i), float64(i), float64(i), float64(i)); err != nil {
			t.Fatal(err)
		}
		bars = append(bars, models.Bar{Symbol: "ZZZ", Date: d, Close: float64(i)})
	}
	mdb.Close()

	// Only the last 10 bars are handed in: the averages must still come from full history.
	in := map[string][]models.Bar{"ZZZ": bars[250:]}
	if err := LoadBarSMA(market, calc, in); err != nil {
		t.Fatal(err)
	}
	last := in["ZZZ"][len(in["ZZZ"])-1]
	if want := 235.5; math.Abs(last.SMA50-want) > 1e-9 { // mean of 211..260
		t.Fatalf("SMA50 = %v, want %v", last.SMA50, want)
	}
	if want := 160.5; math.Abs(last.SMA200-want) > 1e-9 { // mean of 61..260
		t.Fatalf("SMA200 = %v, want %v", last.SMA200, want)
	}
	first := in["ZZZ"][0] // i=251
	if want := (52.0 + 251.0) / 2; math.Abs(first.SMA200-want) > 1e-9 {
		t.Fatalf("first SMA200 = %v, want %v", first.SMA200, want)
	}
}
