package simulator

import (
	"math"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

func flatBars(sym string, dates []string, closes []float64) map[string][]models.Bar {
	bars := make([]models.Bar, len(dates))
	for i := range dates {
		c := closes[i]
		bars[i] = models.Bar{Symbol: sym, Date: dates[i], Open: c, High: c, Low: c, Close: c, Volume: 1}
	}
	return map[string][]models.Bar{sym: bars}
}

func shortCfg() strategy.StrategyConfig {
	return strategy.StrategyConfig{
		AllocationPct:  1,
		PositionCap:    1,
		HoldingWindow:  252,
		TargetPct:      999,
		StopLossPct:    0,
		SlippagePct:    0,
		PositionSizing: "fixed_pct",
	}
}

func TestShortProfitsWhenPriceFalls(t *testing.T) {
	dates := []string{"2021-01-04", "2021-01-05", "2021-12-31"}
	bars := flatBars("TQQQ", dates, []float64{100, 90, 80})
	sigs := []models.Signal{
		{Symbol: "TQQQ", Date: "2021-01-04", Close: 100, OrderType: "market", Entry: 1, Direction: "SHORT"},
		{Symbol: "TQQQ", Date: "2021-12-31", Close: 80, OrderType: "market", Entry: -1, Direction: "SHORT"},
	}
	sim := NewPortfolioSimulator(shortCfg(), 100000)
	_, trades, curve := sim.Run(sigs, bars, dates)
	if len(trades) != 1 {
		t.Fatalf("trades: %+v", trades)
	}
	tr := trades[0]
	if tr.ExitReason != models.ExitReasonSignal || tr.Shares != 1000 {
		t.Fatalf("trade %+v", tr)
	}
	if math.Abs(tr.ReturnPct-0.20) > 1e-9 || math.Abs(tr.NetPnL-20000) > 1e-6 {
		t.Fatalf("pnl return %.4f net %.2f", tr.ReturnPct, tr.NetPnL)
	}
	end := curve[len(curve)-1].TotalEquity
	if math.Abs(end-120000) > 1e-6 {
		t.Fatalf("equity %.2f", end)
	}
}

func TestShortLossIsNotFlooredAtZero(t *testing.T) {
	dates := []string{"2021-01-04", "2021-12-31"}
	bars := flatBars("TQQQ", dates, []float64{100, 350})
	sigs := []models.Signal{
		{Symbol: "TQQQ", Date: "2021-01-04", Close: 100, OrderType: "market", Entry: 1, Direction: "SHORT"},
		{Symbol: "TQQQ", Date: "2021-12-31", Close: 350, OrderType: "market", Entry: -1, Direction: "SHORT"},
	}
	sim := NewPortfolioSimulator(shortCfg(), 100000)
	_, trades, curve := sim.Run(sigs, bars, dates)
	if len(trades) != 1 || math.Abs(trades[0].NetPnL-(-250000)) > 1e-6 {
		t.Fatalf("trade %+v", trades)
	}
	end := curve[len(curve)-1].TotalEquity
	if math.Abs(end-(-150000)) > 1e-6 {
		t.Fatalf("equity floored or wrong: %.2f", end)
	}
}

func TestExitSignalDoesNotOpenAPosition(t *testing.T) {
	dates := []string{"2021-01-04", "2021-06-01", "2021-12-31"}
	bars := flatBars("SPY", dates, []float64{100, 110, 90})
	sigs := []models.Signal{
		{Symbol: "SPY", Date: "2021-01-04", Close: 100, OrderType: "market", Entry: 1},
		{Symbol: "SPY", Date: "2021-06-01", Close: 110, OrderType: "market", Entry: -1},
		{Symbol: "SPY", Date: "2021-12-31", Close: 90, OrderType: "market", Entry: -1},
	}
	sim := NewPortfolioSimulator(shortCfg(), 100000)
	_, trades, _ := sim.Run(sigs, bars, dates)
	if len(trades) != 1 || trades[0].ExitDate != "2021-06-01" || trades[0].ExitReason != models.ExitReasonSignal {
		t.Fatalf("trades %+v", trades)
	}
	if math.Abs(trades[0].ReturnPct-0.10) > 1e-9 {
		t.Fatalf("return %v", trades[0].ReturnPct)
	}
}

func TestShortBorrowReducesEquity(t *testing.T) {
	dates := []string{"2021-01-04", "2021-01-05"}
	bars := flatBars("TQQQ", dates, []float64{100, 100})
	sigs := []models.Signal{
		{Symbol: "TQQQ", Date: "2021-01-04", Close: 100, OrderType: "market", Entry: 1, Direction: "SHORT"},
		{Symbol: "TQQQ", Date: "2021-01-05", Close: 100, OrderType: "market", Entry: -1, Direction: "SHORT"},
	}
	cfg := shortCfg()
	cfg.ShortBorrowAnnual = 0.10
	sim := NewPortfolioSimulator(cfg, 100000)
	_, trades, curve := sim.Run(sigs, bars, dates)
	if len(trades) != 1 || trades[0].NetPnL != 0 {
		t.Fatalf("borrow should not be inside trade pnl: %+v", trades)
	}
	end := curve[len(curve)-1].TotalEquity
	if end >= 100000 {
		t.Fatalf("borrow did not reduce equity: %.2f", end)
	}
}
