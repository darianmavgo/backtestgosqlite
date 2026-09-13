package simulator

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

type mockStrategy struct {
	id   string
	name string
	cfg  strategy.StrategyConfig
}

func (m *mockStrategy) ID() string                              { return m.id }
func (m *mockStrategy) Name() string                            { return m.name }
func (m *mockStrategy) Description() string                     { return m.name }
func (m *mockStrategy) DefaultConfig() strategy.StrategyConfig { return m.cfg }
func (m *mockStrategy) Validate() error                         { return nil }
func (m *mockStrategy) SetDatabases(mPath, cPath string)        {}
func (m *mockStrategy) GenerateSignals(bars map[string][]models.Bar) []models.Signal {
	return nil
}

func TestSharedAccountPreemption(t *testing.T) {
	primaryStrat := &mockStrategy{
		id:   "voo-tecl-combo",
		name: "Primary Strategy",
		cfg: strategy.StrategyConfig{
			ID:            "voo-tecl-combo",
			AllocationPct: 0.65, // Needs 65% ($65,000 on $100k equity)
			PositionCap:   1,
			HoldingWindow: 8,
		},
	}

	secondaryStrat := &mockStrategy{
		id:   "bb-capitulation",
		name: "Secondary Strategy",
		cfg: strategy.StrategyConfig{
			ID:            "bb-capitulation",
			AllocationPct: 0.20, // Needs 20% ($20,000)
			PositionCap:   5,
			HoldingWindow: 10,
		},
	}

	entries := []StrategyPriorityEntry{
		{Strategy: primaryStrat, Priority: 0, Config: primaryStrat.cfg},
		{Strategy: secondaryStrat, Priority: 1, Config: secondaryStrat.cfg},
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

	// Day 1 (2026-01-01): bb-capitulation enters 3 positions ($20k each = $60k invested, $40k cash remaining)
	// Day 2 (2026-01-02): voo-tecl-combo fires buy signal for TECL (needs 65% of $100k = $65k).
	// Since Cash is only $40k, it must PREEMPT one or more bb-capitulation positions!
	signals := []models.Signal{
		// Day 1: Secondary signals
		{Date: "2026-01-01", Symbol: "AMD", Close: 100, BuyLimit: 100, StrategyID: "bb-capitulation", Priority: 1, OrderType: "limit"},
		{Date: "2026-01-01", Symbol: "NVDA", Close: 100, BuyLimit: 100, StrategyID: "bb-capitulation", Priority: 1, OrderType: "limit"},
		{Date: "2026-01-01", Symbol: "AAPL", Close: 100, BuyLimit: 100, StrategyID: "bb-capitulation", Priority: 1, OrderType: "limit"},

		// Day 2: Primary signal (TECL)
		{Date: "2026-01-02", Symbol: "TECL", Close: 50, BuyLimit: 50, StrategyID: "voo-tecl-combo", Priority: 0, OrderType: "limit"},
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
			if tr.StrategyID != "bb-capitulation" {
				t.Errorf("expected preempted trade to belong to bb-capitulation, got %s", tr.StrategyID)
			}
			t.Logf("✅ Verified Preempted Trade: %s on %s with PnL $%.2f", tr.Symbol, tr.ExitDate, tr.NetPnL)
		}
	}

	if !foundPreempted {
		t.Errorf("expected to find trade with ExitReasonPreempted in closed trades")
	}

	// Verify TECL was entered by voo-tecl-combo
	var foundTECL bool
	for _, tr := range closedTrades {
		if tr.Symbol == "TECL" && tr.StrategyID == "voo-tecl-combo" {
			foundTECL = true
		}
	}
	if !foundTECL {
		t.Errorf("expected TECL trade to have executed for primary strategy voo-tecl-combo")
	}

	// Check per-strategy reports exist
	if _, ok := perStratReport["voo-tecl-combo"]; !ok {
		t.Errorf("missing report for voo-tecl-combo")
	}
	if _, ok := perStratReport["bb-capitulation"]; !ok {
		t.Errorf("missing report for bb-capitulation")
	}

	t.Logf("Combined Total Return: %.2f%%", report.TotalReturnPct*100)
}
