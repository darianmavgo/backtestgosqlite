package simulator

import (
	"math"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func hours(sym string, rows ...[5]any) []models.Bar { // time, open, high, low, close
	out := make([]models.Bar, len(rows))
	for i, r := range rows {
		out[i] = models.Bar{Symbol: sym, Date: r[0].(string), Open: r[1].(float64), High: r[2].(float64), Low: r[3].(float64), Close: r[4].(float64)}
	}
	return out
}

// A day whose daily bar says "filled and +5%" but whose high came before the fill.
func highBeforeFillDay() (map[string][]models.Bar, Intraday) {
	daily := day("AAA", limitDates, [4]float64{100, 100, 100, 100}, [4]float64{94, 95, 89, 91}, [4]float64{91, 91, 91, 91})
	h := Intraday{"AAA": hours("AAA",
		[5]any{"2026-03-03 09:00", 94.0, 95.0, 93.0, 93.0}, // the high of the day, before any fill
		[5]any{"2026-03-03 10:00", 93.0, 93.0, 89.0, 90.0}, // trades down through the 90 limit
		[5]any{"2026-03-03 11:00", 90.0, 91.0, 89.5, 91.0},
	)}
	return daily, h
}

func TestIntradayStopsTheDailyBarFromCreditingAHighThatCameBeforeTheFill(t *testing.T) {
	daily, h := highBeforeFillDay()

	flat := NewPortfolioSimulator(limitCfg(), 100000)
	_, trades, _ := flat.Run([]models.Signal{buy("2026-03-03")}, daily, limitDates)
	if len(trades) != 1 || trades[0].ExitDate != "2026-03-03" {
		t.Fatalf("the daily bar alone should show the same-day win, got %+v", trades)
	}

	sim := NewPortfolioSimulator(limitCfg(), 100000)
	sim.Intraday = h
	_, trades, _ = sim.Run([]models.Signal{buy("2026-03-03")}, daily, limitDates)
	if len(trades) != 1 {
		t.Fatalf("trades: %+v", trades)
	}
	if tr := trades[0]; tr.ExitDate == tr.EntryDate || tr.ExitReason == models.ExitReasonProfitTarget {
		t.Fatalf("the 95 high was before the fill: no same-day profit, got %+v", tr)
	}
}

func TestIntradayExitsOnALaterHourReachingTheTarget(t *testing.T) {
	daily, h := highBeforeFillDay()
	h["AAA"][2].High = 95 // 11:00 now trades up through 90 * 1.05
	sim := NewPortfolioSimulator(limitCfg(), 100000)
	sim.Intraday = h
	_, trades, _ := sim.Run([]models.Signal{buy("2026-03-03")}, daily, limitDates)
	if len(trades) != 1 || trades[0].ExitDate != "2026-03-03" || trades[0].ExitReason != models.ExitReasonProfitTarget {
		t.Fatalf("want a same-day profit-target exit after the fill, got %+v", trades)
	}
}

func TestIntradayFillBarHighDoesNotCountAfterTheFill(t *testing.T) {
	daily, h := highBeforeFillDay()
	h["AAA"][1].High = 99 // the fill hour itself reaches 99: order inside the hour is unknown
	sim := NewPortfolioSimulator(limitCfg(), 100000)
	sim.Intraday = h
	_, trades, _ := sim.Run([]models.Signal{buy("2026-03-03")}, daily, limitDates)
	if len(trades) != 1 || trades[0].ExitDate == trades[0].EntryDate {
		t.Fatalf("the fill hour's high must not make a same-day exit, got %+v", trades)
	}
}

func TestIntradayGapOpenFillsAtTheOpenAndTheFirstHourCounts(t *testing.T) {
	daily := day("AAA", limitDates, [4]float64{100, 100, 100, 100}, [4]float64{85, 92, 84, 90}, [4]float64{90, 90, 90, 90})
	h := Intraday{"AAA": hours("AAA",
		[5]any{"2026-03-03 09:00", 80.0, 92.0, 84.0, 90.0}, // premarket 80 is under the day's low and is clipped
		[5]any{"2026-03-03 10:00", 90.0, 90.0, 90.0, 90.0},
	)}
	sim := NewPortfolioSimulator(limitCfg(), 100000)
	sim.Intraday = h
	_, trades, _ := sim.Run([]models.Signal{buy("2026-03-03")}, daily, limitDates)
	if len(trades) != 1 || trades[0].EntryPrice != 85 || trades[0].ExitDate != "2026-03-03" {
		t.Fatalf("want a fill at the 85 open and the 89.25 target hit in the first hour (high 92), got %+v", trades)
	}
	if math.Abs(trades[0].ExitPrice-85*1.05) > 1e-9 {
		t.Fatalf("exit %v", trades[0].ExitPrice)
	}
}

func TestIntradayCoveredDayWithNoHourlyBarsMakesNoEntry(t *testing.T) {
	daily := day("AAA", limitDates, [4]float64{100, 100, 100, 100}, [4]float64{99, 99, 88, 95}, [4]float64{95, 95, 95, 95})
	h := Intraday{"AAA": hours("AAA", // coverage on 03-02 and 03-04 but nothing on 03-03
		[5]any{"2026-03-02 10:00", 100.0, 100.0, 100.0, 100.0},
		[5]any{"2026-03-04 10:00", 95.0, 95.0, 95.0, 95.0},
	)}
	sim := NewPortfolioSimulator(limitCfg(), 100000)
	sim.Intraday = h
	_, trades, _ := sim.Run([]models.Signal{buy("2026-03-03")}, daily, limitDates)
	if len(trades) != 0 {
		t.Fatalf("no hourly bar can confirm the fill: want no trade, got %+v", trades)
	}
}

func TestIntradayOutsideCoverageFallsBackToTheDailyBar(t *testing.T) {
	daily, _ := highBeforeFillDay()
	h := Intraday{"AAA": hours("AAA", [5]any{"2026-02-02 10:00", 100.0, 100.0, 100.0, 100.0})}
	sim := NewPortfolioSimulator(limitCfg(), 100000)
	sim.Intraday = h
	_, trades, _ := sim.Run([]models.Signal{buy("2026-03-03")}, daily, limitDates)
	if len(trades) != 1 || trades[0].ExitDate != "2026-03-03" {
		t.Fatalf("a day with no hourly coverage uses the daily bar, got %+v", trades)
	}
}

func TestUnmetLimitLeavesTheAccountUntouched(t *testing.T) {
	// The low of 91 never reaches the 90 limit, so there is no position and no cash used.
	daily := day("AAA", limitDates, [4]float64{100, 100, 100, 100}, [4]float64{99, 101, 91, 100}, [4]float64{100, 100, 100, 100})
	exit := models.Signal{Symbol: "AAA", Date: "2026-03-04", Close: 100, Entry: -1, Direction: "LONG"}
	sim := NewPortfolioSimulator(limitCfg(), 100000)
	report, trades, curve := sim.Run([]models.Signal{buy("2026-03-03"), exit}, daily, limitDates)
	if len(trades) != 0 || report.NetProfit != 0 {
		t.Fatalf("unmet limit must not enter: trades %+v net %v", trades, report.NetProfit)
	}
	for _, pt := range curve {
		if pt.OpenPositions != 0 || pt.Cash != 100000 {
			t.Fatalf("account moved on an unmet limit: %+v", pt)
		}
	}
}
