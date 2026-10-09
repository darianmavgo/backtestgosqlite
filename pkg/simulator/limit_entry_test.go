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
