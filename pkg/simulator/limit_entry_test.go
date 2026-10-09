package simulator

import (
	"math"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

func limitCfg() strategy.StrategyConfig {
	return strategy.StrategyConfig{
		AllocationPct: 1, PositionCap: 1, HoldingWindow: 99999, PositionSizing: "fixed_pct",
		EntryLimitPct: 0.9, TakeProfitPct: 0.05, SameDayExit: true,
	}
}

// day builds one bar per row (open, high, low, close) on the given dates.
func day(sym string, dates []string, rows ...[4]float64) map[string][]models.Bar {
	out := make([]models.Bar, len(rows))
	for i, r := range rows {
		out[i] = models.Bar{Symbol: sym, Date: dates[i], Open: r[0], High: r[1], Low: r[2], Close: r[3], Volume: 1}
	}
	return map[string][]models.Bar{sym: out}
}

var limitDates = []string{"2026-03-02", "2026-03-03", "2026-03-04"}

func buy(date string) models.Signal {
	return models.Signal{Symbol: "AAA", Date: date, Close: 1, Entry: 1, Direction: "LONG"}
}

func TestLimitEntryIsPercentOfThePreviousClose(t *testing.T) {
	bars := day("AAA", limitDates, [4]float64{100, 100, 100, 100}, [4]float64{99, 99, 88, 95}, [4]float64{95, 95, 95, 95})
	got := ApplyLiveEntryModel([]models.Signal{buy("2026-03-03")}, bars, func(models.Signal) strategy.StrategyConfig { return limitCfg() })
	if len(got) != 1 || math.Abs(got[0].BuyLimit-90) > 1e-9 || got[0].Date != "2026-03-03" {
		t.Fatalf("want a buy at 90 (0.9 of the 100 close) on 03-03, got %+v", got)
	}
}

func TestLimitEntryFillsAtTheOpenWhenItGapsBelow(t *testing.T) {
	bars := day("AAA", limitDates, [4]float64{100, 100, 100, 100}, [4]float64{85, 92, 84, 90}, [4]float64{90, 90, 90, 90})
	got := ApplyLiveEntryModel([]models.Signal{buy("2026-03-03")}, bars, func(models.Signal) strategy.StrategyConfig { return limitCfg() })
	if len(got) != 1 || got[0].BuyLimit != 85 {
		t.Fatalf("want the 85 open, got %+v", got)
	}
}

func TestLimitEntryIsSkippedWhenTheLowStaysAboveTheLimit(t *testing.T) {
	bars := day("AAA", limitDates, [4]float64{100, 100, 100, 100}, [4]float64{99, 101, 91, 100}, [4]float64{100, 100, 100, 100})
	got := ApplyLiveEntryModel([]models.Signal{buy("2026-03-03")}, bars, func(models.Signal) strategy.StrategyConfig { return limitCfg() })
	if len(got) != 0 {
		t.Fatalf("low 91 never reaches 90: want no entry, got %+v", got)
	}
}

func TestSameDayExitOnTheHighReachingTheProfitTaker(t *testing.T) {
	// Fills at the 90 limit and the high of 95 is above 90 * 1.05 = 94.5.
	bars := day("AAA", limitDates, [4]float64{100, 100, 100, 100}, [4]float64{99, 95, 89, 91}, [4]float64{91, 91, 91, 91})
	sim := NewPortfolioSimulator(limitCfg(), 100000)
	_, trades, _ := sim.Run([]models.Signal{buy("2026-03-03")}, bars, limitDates)
	if len(trades) != 1 {
		t.Fatalf("trades: %+v", trades)
	}
	tr := trades[0]
	if tr.EntryDate != "2026-03-03" || tr.ExitDate != "2026-03-03" || tr.ExitReason != models.ExitReasonProfitTarget {
		t.Fatalf("want a profit-target exit on the entry day, got %+v", tr)
	}
	if math.Abs(tr.ExitPrice-tr.EntryPrice*1.05) > 1e-9 {
		t.Fatalf("exit %v is not 5%% over entry %v", tr.ExitPrice, tr.EntryPrice)
	}
}

func TestNoSameDayExitWhenTheHighFallsShortOrTheFlagIsOff(t *testing.T) {
	bars := day("AAA", limitDates, [4]float64{100, 100, 100, 100}, [4]float64{99, 94, 89, 91}, [4]float64{91, 91, 91, 91})
	sim := NewPortfolioSimulator(limitCfg(), 100000)
	_, trades, _ := sim.Run([]models.Signal{buy("2026-03-03")}, bars, limitDates)
	if len(trades) != 1 || trades[0].ExitDate == trades[0].EntryDate {
		t.Fatalf("high 94 is under the 94.5 target: the position stays open past the entry day, got %+v", trades)
	}

	bars = day("AAA", limitDates, [4]float64{100, 100, 100, 100}, [4]float64{99, 95, 89, 91}, [4]float64{91, 91, 91, 91})
	cfg := limitCfg()
	cfg.SameDayExit = false
	sim = NewPortfolioSimulator(cfg, 100000)
	_, trades, _ = sim.Run([]models.Signal{buy("2026-03-03")}, bars, limitDates)
	for _, tr := range trades {
		if tr.ExitDate == tr.EntryDate {
			t.Fatalf("flag off must not exit on the entry day: %+v", tr)
		}
	}
}

func TestOneFillPerMonthTakesTheSymbolOutUntilNextMonth(t *testing.T) {
	dates := []string{"2026-03-27", "2026-03-30", "2026-03-31", "2026-04-01", "2026-04-02"}
	// Every session trades down to the 90 limit (the previous close is always 100).
	bars := day("AAA", dates, [4]float64{100, 100, 85, 100}, [4]float64{100, 100, 85, 100}, [4]float64{100, 100, 85, 100}, [4]float64{100, 100, 85, 100}, [4]float64{100, 100, 85, 100})
	var signals []models.Signal
	for _, d := range dates {
		signals = append(signals, buy(d))
	}
	cfg := func(oneFill bool) strategy.StrategyConfig {
		return strategy.StrategyConfig{
			AllocationPct: 0.1, PositionCap: 10, HoldingWindow: 1, PositionSizing: "fixed_pct",
			EntryLimitPct: 0.9, OneFillPerMonth: oneFill,
		}
	}
	entries := func(oneFill bool) []string {
		sim := NewPortfolioSimulator(cfg(oneFill), 100000)
		_, trades, _ := sim.Run(signals, bars, dates)
		var got []string
		for _, tr := range trades {
			got = append(got, tr.EntryDate)
		}
		return got
	}
	if got := entries(false); len(got) != 4 {
		t.Fatalf("without the flag every session after the first fills: got %v", got)
	}
	got := entries(true)
	if len(got) != 2 || got[0] != "2026-03-30" || got[1] != "2026-04-01" {
		t.Fatalf("want one fill in March (03-30) and one in April (04-01), got %v", got)
	}
}

func TestOneFillPerMonthIgnoresAnOrderThatCouldNotBeBooked(t *testing.T) {
	dates := []string{"2026-03-02", "2026-03-03", "2026-03-04", "2026-03-05"}
	bars := day("AAA", dates, [4]float64{100, 100, 100, 100}, [4]float64{100, 100, 85, 100}, [4]float64{100, 100, 85, 100}, [4]float64{100, 100, 85, 100})
	cfg := strategy.StrategyConfig{
		AllocationPct: 0.1, PositionCap: 10, HoldingWindow: 1, PositionSizing: "fixed_pct",
		EntryLimitPct: 0.9, OneFillPerMonth: true,
	}
	// No cash on 03-03: the order cannot be booked, so AAA is still in the rotation.
	sim := NewPortfolioSimulator(cfg, 100000)
	sim.Cash = 0
	signals := []models.Signal{buy("2026-03-03"), buy("2026-03-04")}
	sim.Run(signals, bars, dates)
	if len(sim.Positions) != 0 || sim.Cash != 0 {
		t.Fatalf("with no cash nothing can be booked, positions %v cash %v", sim.Positions, sim.Cash)
	}
	if got := sim.filledMonth["AAA"]; got != "" {
		t.Fatalf("an unbooked order marked AAA as filled in %q", got)
	}
}
