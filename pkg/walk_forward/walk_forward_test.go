package walk_forward

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	_ "modernc.org/sqlite"
)

func TestFoldsRollMonthly(t *testing.T) {
	var dates []string
	d := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	for len(dates) < 90 {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			dates = append(dates, d.Format("2006-01-02"))
		}
		d = d.AddDate(0, 0, 1)
	}
	folds, err := Folds(dates, 1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(folds) < 2 {
		t.Fatalf("folds = %d", len(folds))
	}
	if folds[0].ISEnd >= folds[0].OOSStart {
		t.Fatalf("IS runs into OOS: %+v", folds[0])
	}
	if folds[1].OOSStart <= folds[0].OOSStart {
		t.Fatalf("second fold did not move forward: %+v then %+v", folds[0], folds[1])
	}
}

func TestRunStrategyWritesSummary(t *testing.T) {
	strat, ok := strategy.Get("tsll-daily-one-share")
	if !ok {
		t.Fatal("tsll-daily-one-share is not registered")
	}
	var bars []models.Bar
	d := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	px := 20.0
	for len(bars) < 80 {
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			bars = append(bars, models.Bar{
				Symbol: "TSLL", Date: d.Format("2006-01-02"),
				Open: px, High: px + 0.05, Low: px - 0.05, Close: px, Volume: 1000,
			})
			px += 0.01
		}
		d = d.AddDate(0, 0, 1)
	}
	rows, err := RunStrategy(context.Background(), strat, map[string][]models.Bar{"TSLL": bars}, nil, Options{TrainMonths: 1, TestMonths: 1, StepMonths: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].StrategyID != "tsll-daily-one-share" {
		t.Fatalf("rows = %+v", rows)
	}
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "wf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Save(db, strat.ID(), rows); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM walk_forward_summary WHERE strategy_id = ?`, strat.ID()).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("summary rows = %d", n)
	}
}
