package strategy

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestMARATreeStrategyRegistration(t *testing.T) {
	strat, exists := Get("mara_tree")
	if !exists {
		t.Fatalf("Strategy 'mara_tree' not registered")
	}
	if strat.ID() != "mara_tree" {
		t.Errorf("Expected ID 'mara_tree', got '%s'", strat.ID())
	}
	cfg := strat.DefaultConfig()
	if cfg.HoldingWindow != 1 {
		t.Errorf("Expected HoldingWindow 1, got %d", cfg.HoldingWindow)
	}
	if cfg.TakeProfitPct != 0.05 {
		t.Errorf("Expected TakeProfitPct 0.05, got %f", cfg.TakeProfitPct)
	}
	if cfg.StopLossPct != 0.92 {
		t.Errorf("Expected StopLossPct 0.92, got %f", cfg.StopLossPct)
	}
	if cfg.AllocationPct != 0.65 {
		t.Errorf("Expected AllocationPct 0.65, got %f", cfg.AllocationPct)
	}
}

func TestMARATreeGenerateSignals(t *testing.T) {
	strat, exists := Get("mara_tree")
	if !exists {
		t.Fatalf("Strategy 'mara_tree' not found")
	}

	// Create dummy bars: 210 bars
	bars := make([]models.Bar, 215)
	for i := range bars {
		bars[i] = models.Bar{
			Idx:    i,
			Symbol: "MARA",
			Date:   "2024-01-01",
			Open:   100.0,
			High:   101.0,
			Low:    99.0,
			Close:  100.0,
			Volume: 1000000,
		}
	}

	// Bar 210: make it an ATR compression coil (range 0.2 vs atr ~2.0)
	bars[210].High = 100.1
	bars[210].Low = 99.9
	bars[210].Close = 100.0

	barsBySymbol := map[string][]models.Bar{
		"MARA": bars,
	}

	signals := strat.GenerateSignals(barsBySymbol)
	if len(signals) == 0 {
		t.Fatalf("Expected at least 1 signal from dummy coil bar")
	}

	sig := signals[0]
	if sig.Symbol != "MARA" {
		t.Errorf("Expected Symbol MARA, got %s", sig.Symbol)
	}
	if sig.TakeProfit != 105.0 {
		t.Errorf("Expected TakeProfit 105.0, got %f", sig.TakeProfit)
	}
	if sig.StopLoss != 92.0 {
		t.Errorf("Expected StopLoss 92.0, got %f", sig.StopLoss)
	}
	if sig.HoldDaysOverride != 1 {
		t.Errorf("Expected HoldDaysOverride 1, got %d", sig.HoldDaysOverride)
	}
}
