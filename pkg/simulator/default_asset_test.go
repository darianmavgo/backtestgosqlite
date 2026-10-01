package simulator

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

func evenBars(dates []string, price float64) []models.Bar {
	bars := make([]models.Bar, len(dates))
	for i, d := range dates {
		bars[i] = models.Bar{Date: d, Open: price, High: price, Low: price, Close: price}
	}
	return bars
}

func parkEntry(id string, alloc float64, hold int, stop float64) StrategyPriorityEntry {
	cfg := strategy.StrategyConfig{
		ID:             id,
		AllocationPct:  alloc,
		PositionCap:    1,
		HoldingWindow:  hold,
		PositionSizing: "fixed_pct",
		StopLossPct:    stop,
	}
	return StrategyPriorityEntry{
		Strategy: &mockStrategy{id: id, name: id, cfg: cfg},
		Priority: 0,
		Config:   cfg,
	}
}

func TestDefaultAssetBeatsIdleCash(t *testing.T) {
	dates := []string{"2026-01-02", "2026-01-05", "2026-01-06", "2026-01-07"}
	entry := parkEntry("sleeve", 0.10, 1, 0)
	entry.Priority = 0
	signals := []models.Signal{{
		Date: dates[0], Symbol: "FLAT", Close: 50, BuyLimit: 50,
		StrategyID: "sleeve", Priority: 0, OrderType: "limit",
	}}
	sleeveBars := evenBars(dates, 50)
	parkPrices := []float64{100, 125, 150, 200}
	parkBars := make([]models.Bar, len(dates))
	for i, d := range dates {
		px := parkPrices[i]
		parkBars[i] = models.Bar{Date: d, Open: px, High: px, Low: px, Close: px}
	}
	bars := map[string][]models.Bar{"FLAT": sleeveBars, "PARK": parkBars}

	cashSim := NewSharedAccountSimulator([]StrategyPriorityEntry{entry}, 100000)
	cashReport, _, cashTrades, _ := cashSim.Run(signals, bars, dates)

	parkSim := NewSharedAccountSimulator([]StrategyPriorityEntry{entry}, 100000)
	parkSim.SetDefaultAsset("PARK", nil)
	parkReport, _, parkTrades, curve := parkSim.Run(signals, bars, dates)

	if parkReport.FinalEquity <= cashReport.FinalEquity {
		t.Fatalf("parked equity %.2f, cash equity %.2f; a rising park should finish ahead", parkReport.FinalEquity, cashReport.FinalEquity)
	}
	if len(parkTrades) != len(cashTrades) {
		t.Fatalf("park sleeve trades %d, cash sleeve trades %d", len(parkTrades), len(cashTrades))
	}
	if len(parkTrades) != 1 {
		t.Fatalf("sleeve trades = %d, want 1", len(parkTrades))
	}
	for _, pt := range curve {
		if pt.Cash >= 1 {
			t.Errorf("%s cash %.2f, want less than $1 after an even share lot", pt.Date, pt.Cash)
		}
	}
	if parkSim.DefaultAssetResult().DaysUnparked != 0 {
		t.Errorf("unparked days = %d, want 0", parkSim.DefaultAssetResult().DaysUnparked)
	}
}

func TestDefaultAssetFundsEntryWithoutPreemption(t *testing.T) {
	dates := []string{"2026-01-02", "2026-01-05"}
	secondary := parkEntry("secondary", 0.10, 20, 0)
	secondary.Priority = 1
	primary := parkEntry("primary", 0.10, 20, 0)
	primary.Priority = 0
	signals := []models.Signal{
		{Date: dates[0], Symbol: "SEC", Close: 100, BuyLimit: 100, StrategyID: "secondary", Priority: 1, OrderType: "limit"},
		{Date: dates[1], Symbol: "PRI", Close: 100, BuyLimit: 100, StrategyID: "primary", Priority: 0, OrderType: "limit"},
	}
	bars := map[string][]models.Bar{
		"SEC":  evenBars(dates, 100),
		"PRI":  evenBars(dates, 100),
		"PARK": evenBars(dates, 100),
	}
	sim := NewSharedAccountSimulator([]StrategyPriorityEntry{primary, secondary}, 100000)
	sim.SetDefaultAsset("PARK", nil)
	_, _, trades, curve := sim.Run(signals, bars, dates)

	if sim.PreemptedTradeCount != 0 {
		t.Fatalf("preempted %d; the park should fund the primary", sim.PreemptedTradeCount)
	}
	if len(trades) != 2 {
		t.Fatalf("trades = %d, want the secondary and the primary", len(trades))
	}
	if curve[0].OpenPositions != 2 {
		t.Errorf("day 1 open = %d, want sleeve + park", curve[0].OpenPositions)
	}
	if curve[1].OpenPositions != 3 {
		t.Errorf("day 2 open = %d, want both sleeves + park", curve[1].OpenPositions)
	}
	if curve[1].Cash >= 100 {
		t.Errorf("day 2 cash %.2f, want less than one PARK share", curve[1].Cash)
	}
}

func TestDefaultAssetSameSymbolKeepsSleeveLot(t *testing.T) {
	dates := []string{"2026-01-02", "2026-01-05", "2026-01-06"}
	entry := parkEntry("sleeve", 0.10, 20, 0.90)
	bars := map[string][]models.Bar{
		"GOOGL": {
			{Date: dates[0], Open: 100, High: 100, Low: 100, Close: 100},
			{Date: dates[1], Open: 100, High: 100, Low: 100, Close: 100},
			{Date: dates[2], Open: 85, High: 85, Low: 80, Close: 80},
		},
	}
	signals := []models.Signal{{
		Date: dates[1], Symbol: "GOOGL", Close: 100, BuyLimit: 100,
		StrategyID: "sleeve", Priority: 0, OrderType: "limit",
	}}
	sim := NewSharedAccountSimulator([]StrategyPriorityEntry{entry}, 100000)
	sim.SetDefaultAsset("GOOGL", nil)
	_, _, trades, curve := sim.Run(signals, bars, dates)

	if len(trades) != 1 {
		t.Fatalf("trades = %d, want the sleeve lot only", len(trades))
	}
	if trades[0].Symbol != "GOOGL" || trades[0].Shares != 100 {
		t.Fatalf("sleeve trade = %s x %d, want GOOGL x 100", trades[0].Symbol, trades[0].Shares)
	}
	if trades[0].ExitReason != models.ExitReasonStopLoss {
		t.Errorf("exit %s, want stop", trades[0].ExitReason)
	}
	if curve[0].OpenPositions != 1 {
		t.Errorf("day 1 open = %d, want the park only", curve[0].OpenPositions)
	}
	if curve[1].OpenPositions != 2 {
		t.Errorf("day 2 open = %d, want park and sleeve lots", curve[1].OpenPositions)
	}
	if curve[2].OpenPositions != 1 {
		t.Errorf("day 3 open = %d, want the park after the sleeve stop", curve[2].OpenPositions)
	}
}

func TestDefaultAssetMissingBarLeavesCash(t *testing.T) {
	dates := []string{"2026-01-02", "2026-01-05"}
	entry := parkEntry("sleeve", 0.10, 5, 0)
	bars := map[string][]models.Bar{
		"FLAT": evenBars(dates, 50),
	}
	sim := NewSharedAccountSimulator([]StrategyPriorityEntry{entry}, 100000)
	sim.SetDefaultAsset("PARK", nil)
	report, _, _, curve := sim.Run(nil, bars, dates)
	if len(curve) != 2 {
		t.Fatalf("curve points = %d, want 2", len(curve))
	}
	if report.FinalEquity != 100000 {
		t.Fatalf("equity %.2f, want the starting cash when PARK has no bars", report.FinalEquity)
	}
	got := sim.DefaultAssetResult()
	if got.DaysUnparked != 2 || got.AvgWeight != 0 {
		t.Fatalf("result %+v, want 2 unparked days and zero weight", got)
	}
}

func TestDefaultAssetDividendIsReinvested(t *testing.T) {
	dates := []string{"2026-01-02", "2026-01-05", "2026-01-06"}
	entry := parkEntry("sleeve", 0.10, 5, 0)
	bars := map[string][]models.Bar{
		"PARK": evenBars(dates, 10),
	}
	sim := NewSharedAccountSimulator([]StrategyPriorityEntry{entry}, 100000)
	sim.SetDefaultAsset("PARK", map[string]float64{dates[1]: 1})
	report, _, _, curve := sim.Run(nil, bars, dates)

	got := sim.DefaultAssetResult()
	if got.Dividends != 10000 {
		t.Fatalf("dividends %.2f, want 10000 (10000 shares x $1)", got.Dividends)
	}
	if curve[1].Cash >= 10 || curve[2].Cash >= 10 {
		t.Fatalf("cash after the ex-date is %.2f then %.2f; the sweep should reinvest it", curve[1].Cash, curve[2].Cash)
	}
	if report.FinalEquity < 109000 {
		t.Fatalf("equity %.2f, want the dividend still in the book", report.FinalEquity)
	}
}
