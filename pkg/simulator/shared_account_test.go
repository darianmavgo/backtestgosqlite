package simulator

import (
	"fmt"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/streak_strategy"
)

// rowStrategy is a real streak_strategy row, used for its id. The simulator takes
// each strategy's sizing from the entry's Config and its trades from the signals,
// and both are written out literally in the tests.
func rowStrategy(id string) strategy.Strategy {
	return &streak_strategy.Strategy{Row: refdb.StreakStrategy{
		ID: id, Name: id, SignalSymbol: "VOO", TradeSymbol: "TECL", Direction: "drop",
		SignalDays: 3, HoldDays: 8, Regime: "All Regimes", AllocationPct: 0.1,
	}}
}

func TestSharedAccountPreemption(t *testing.T) {
	primaryCfg := strategy.StrategyConfig{
		ID:            "streak-voo-buy-tecl",
		AllocationPct: 0.65, // Needs 65% ($65,000 on $100k equity)
		PositionCap:   1,
		HoldingWindow: 8,
	}
	secondaryCfg := strategy.StrategyConfig{
		ID:            "streak-amd-down3",
		AllocationPct: 0.20, // Needs 20% ($20,000)
		PositionCap:   5,
		HoldingWindow: 10,
	}

	entries := []StrategyPriorityEntry{
		{Strategy: rowStrategy("streak-voo-buy-tecl"), Priority: 0, Config: primaryCfg},
		{Strategy: rowStrategy("streak-amd-down3"), Priority: 1, Config: secondaryCfg},
	}

	sim := NewSharedAccountSimulator(entries, 100000.0)

	// Timeline: 2026-01-01 to 2026-01-03
	sortedDates := []string{"2026-01-01", "2026-01-02", "2026-01-03"}

	barsBySymbol := map[string][]models.Bar{
		"TECL": {
			{Date: "2026-01-01", Open: 50, High: 52, Low: 49, Close: 50},
			{Date: "2026-01-02", Open: 50, High: 52, Low: 49, Close: 50},
			{Date: "2026-01-03", Open: 50, High: 55, Low: 50, Close: 54},
		},
		"AMD": {
			{Date: "2026-01-01", Open: 100, High: 102, Low: 98, Close: 100},
			{Date: "2026-01-02", Open: 100, High: 105, Low: 99, Close: 104},
			{Date: "2026-01-03", Open: 104, High: 106, Low: 103, Close: 105},
		},
		"NVDA": {
			{Date: "2026-01-01", Open: 100, High: 102, Low: 98, Close: 100},
			{Date: "2026-01-02", Open: 100, High: 103, Low: 99, Close: 101},
			{Date: "2026-01-03", Open: 101, High: 104, Low: 100, Close: 102},
		},
		"AAPL": {
			{Date: "2026-01-01", Open: 100, High: 102, Low: 98, Close: 100},
			{Date: "2026-01-02", Open: 100, High: 103, Low: 99, Close: 101},
			{Date: "2026-01-03", Open: 101, High: 104, Low: 100, Close: 102},
		},
	}

	// Day 1 (2026-01-01): streak-amd-down3 enters 3 positions ($20k each = $60k invested, $40k cash remaining)
	// Day 2 (2026-01-02): streak-voo-buy-tecl fires buy signal for TECL (needs 65% of $100k = $65k).
	// Since Cash is only $40k, it must PREEMPT one or more streak-amd-down3 positions!
	signals := []models.Signal{
		// Day 1: Secondary signals
		{Date: "2026-01-01", Symbol: "AMD", Close: 100, BuyLimit: 100, StrategyID: "streak-amd-down3", Priority: 1, OrderType: "limit"},
		{Date: "2026-01-01", Symbol: "NVDA", Close: 100, BuyLimit: 100, StrategyID: "streak-amd-down3", Priority: 1, OrderType: "limit"},
		{Date: "2026-01-01", Symbol: "AAPL", Close: 100, BuyLimit: 100, StrategyID: "streak-amd-down3", Priority: 1, OrderType: "limit"},

		// Day 2: Primary signal (TECL)
		{Date: "2026-01-02", Symbol: "TECL", Close: 50, BuyLimit: 50, StrategyID: "streak-voo-buy-tecl", Priority: 0, OrderType: "limit"},
	}

	report, perStratReport, closedTrades, equityCurve := sim.Run(signals, barsBySymbol, sortedDates)

	if len(equityCurve) != 3 {
		t.Fatalf("expected 3 equity curve points, got %d", len(equityCurve))
	}

	// Verify preemption occurred
	if sim.PreemptedTradeCount == 0 {
		t.Errorf("expected at least 1 trade to be preempted by primary signal, got 0")
	}

	// Check closed trades for PREEMPTED_BY_PRIMARY
	var foundPreempted bool
	for _, tr := range closedTrades {
		if tr.ExitReason == models.ExitReasonPreempted {
			foundPreempted = true
			if tr.StrategyID != "streak-amd-down3" {
				t.Errorf("expected preempted trade to belong to streak-amd-down3, got %s", tr.StrategyID)
			}
			t.Logf("✅ Verified Preempted Trade: %s on %s with PnL $%.2f", tr.Symbol, tr.ExitDate, tr.NetPnL)
		}
	}

	if !foundPreempted {
		t.Errorf("expected to find trade with ExitReasonPreempted in closed trades")
	}

	// Verify TECL was entered by streak-voo-buy-tecl
	var foundTECL bool
	for _, tr := range closedTrades {
		if tr.Symbol == "TECL" && tr.StrategyID == "streak-voo-buy-tecl" {
			foundTECL = true
		}
	}
	if !foundTECL {
		t.Errorf("expected TECL trade to have executed for primary strategy streak-voo-buy-tecl")
	}

	// Check per-strategy reports exist
	if _, ok := perStratReport["streak-voo-buy-tecl"]; !ok {
		t.Errorf("missing report for streak-voo-buy-tecl")
	}
	if _, ok := perStratReport["streak-amd-down3"]; !ok {
		t.Errorf("missing report for streak-amd-down3")
	}

	t.Logf("Combined Total Return: %.2f%%", report.TotalReturnPct*100)
}

func TestSharedAccountTenPercentBookFillsEverySlot(t *testing.T) {
	const (
		n       = 10
		capital = 100000.0
		price   = 100.0
	)
	entries := make([]StrategyPriorityEntry, n)
	bars := map[string][]models.Bar{}
	var signals []models.Signal
	dates := []string{"2026-01-02", "2026-01-05"}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("slot-%d", i)
		sym := fmt.Sprintf("S%02d", i)
		cfg := strategy.StrategyConfig{
			ID:                 id,
			AllocationPct:      0.10,
			PositionCap:        1,
			HoldingWindow:      20,
			PositionSizing:     "fixed_pct",
			SlippagePct:        0.0005,
			CommissionPerShare: 0.0001,
		}
		entries[i] = StrategyPriorityEntry{
			Strategy: rowStrategy(id),
			Priority: i,
			Config:   cfg,
		}
		bars[sym] = []models.Bar{
			{Date: dates[0], Open: price, High: price, Low: price, Close: price},
			{Date: dates[1], Open: price, High: price, Low: price, Close: price},
		}
		signals = append(signals, models.Signal{
			Date: dates[0], Symbol: sym, Close: price, BuyLimit: price,
			StrategyID: id, Priority: i, OrderType: "limit",
		})
	}

	sim := NewSharedAccountSimulator(entries, capital)
	_, _, trades, curve := sim.Run(signals, bars, dates)

	if sim.PreemptedTradeCount != 0 {
		t.Errorf("PreemptedTradeCount = %d, want 0; a 10%% primary must not evict other 10%% slots while cash remains", sim.PreemptedTradeCount)
	}
	if len(trades) != n {
		t.Fatalf("filled %d positions, want %d", len(trades), n)
	}
	if len(curve) > 0 && curve[0].OpenPositions != n {
		t.Errorf("day-1 open positions = %d, want %d", curve[0].OpenPositions, n)
	}
	var invested float64
	for _, tr := range trades {
		invested += tr.InvestedCapital
		pct := tr.InvestedCapital / capital
		if pct < 0.09 || pct > 0.101 {
			t.Errorf("%s invested $%.2f (%.2f%% of equity), want about 10%%", tr.Symbol, tr.InvestedCapital, pct*100)
		}
	}
	if invested > capital {
		t.Errorf("total invested $%.2f exceeds capital $%.2f", invested, capital)
	}
}

func TestSharedAccountSecondaryDoesNotPreemptTertiary(t *testing.T) {
	entries := []StrategyPriorityEntry{
		{Strategy: rowStrategy("primary"), Priority: 0, Config: strategy.StrategyConfig{
			ID: "primary", AllocationPct: 0.65, PositionCap: 1, HoldingWindow: 8,
		}},
		{Strategy: rowStrategy("secondary"), Priority: 1, Config: strategy.StrategyConfig{
			ID: "secondary", AllocationPct: 0.65, PositionCap: 1, HoldingWindow: 8,
		}},
		{Strategy: rowStrategy("tertiary"), Priority: 2, Config: strategy.StrategyConfig{
			ID: "tertiary", AllocationPct: 0.80, PositionCap: 1, HoldingWindow: 8,
		}},
	}
	sim := NewSharedAccountSimulator(entries, 100000.0)

	sortedDates := []string{"2026-01-01", "2026-01-02", "2026-01-03"}
	barsBySymbol := map[string][]models.Bar{
		"AAA": {
			{Date: "2026-01-01", Open: 100, High: 100, Low: 100, Close: 100},
			{Date: "2026-01-02", Open: 100, High: 100, Low: 100, Close: 100},
			{Date: "2026-01-03", Open: 100, High: 100, Low: 100, Close: 100},
		},
		"BBB": {
			{Date: "2026-01-01", Open: 100, High: 100, Low: 100, Close: 100},
			{Date: "2026-01-02", Open: 100, High: 100, Low: 100, Close: 100},
			{Date: "2026-01-03", Open: 100, High: 100, Low: 100, Close: 100},
		},
	}
	signals := []models.Signal{
		{Date: "2026-01-01", Symbol: "AAA", Close: 100, BuyLimit: 100, StrategyID: "tertiary", Priority: 2, OrderType: "limit"},
		{Date: "2026-01-02", Symbol: "BBB", Close: 100, BuyLimit: 100, StrategyID: "secondary", Priority: 1, OrderType: "limit"},
	}

	_, _, closedTrades, _ := sim.Run(signals, barsBySymbol, sortedDates)

	for _, tr := range closedTrades {
		if tr.ExitReason == models.ExitReasonPreempted {
			t.Errorf("subordinate %s was preempted; only the primary may preempt", tr.StrategyID)
		}
	}
	if sim.PreemptedTradeCount != 0 {
		t.Errorf("PreemptedTradeCount = %d, want 0 (secondaries must not evict each other)", sim.PreemptedTradeCount)
	}
}
