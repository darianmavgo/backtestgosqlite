package strategy

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

func TestMillwharfRegistration(t *testing.T) {
	strat, found := Get("millwharf")
	if !found {
		t.Fatalf("Expected strategy 'millwharf' to be registered")
	}
	if strat.ID() != "millwharf" {
		t.Errorf("Expected ID 'millwharf', got '%s'", strat.ID())
	}
	cfg := strat.DefaultConfig()
	if cfg.HoldingWindow != 4 {
		t.Errorf("Expected HoldingWindow 4, got %d", cfg.HoldingWindow)
	}
	if !cfg.ExitAtMarketOpen {
		t.Errorf("Expected ExitAtMarketOpen to be true")
	}
	if cfg.StopLossPct > 0.01 {
		t.Errorf("Expected no stop loss (near 0), got %f", cfg.StopLossPct)
	}
	if err := strat.Validate(); err != nil {
		t.Errorf("Expected strategy to validate cleanly, got: %v", err)
	}
}

func TestMillwharfWeeklySignalGeneration(t *testing.T) {
	// Week 1 (2026-W01: Jan 05 - Jan 09)
	// Symbol A: 5 consecutive down days (100 -> 95 -> 90 -> 85 -> 80 -> 75)
	// Highs: 102, 98, 93, 88, 83, 78
	// High of last 6 days on Jan 09: 102
	// Close on Jan 09: 75. 75 * 1.20 = 90. TakeProfit = Min(102, 90) = 90.
	barsA := []models.Bar{
		{Idx: 0, Symbol: "A", Date: "2026-01-02", High: 102.0, Low: 99.0, Close: 100.0},
		{Idx: 1, Symbol: "A", Date: "2026-01-05", High: 98.0, Low: 94.0, Close: 95.0},
		{Idx: 2, Symbol: "A", Date: "2026-01-06", High: 93.0, Low: 89.0, Close: 90.0},
		{Idx: 3, Symbol: "A", Date: "2026-01-07", High: 88.0, Low: 84.0, Close: 85.0},
		{Idx: 4, Symbol: "A", Date: "2026-01-08", High: 83.0, Low: 79.0, Close: 80.0},
		{Idx: 5, Symbol: "A", Date: "2026-01-09", High: 78.0, Low: 74.0, Close: 75.0}, // 5 consecutive down closes
	}

	// Symbol B in same week: 4 down days only (streak < 5, should be excluded)
	barsB := []models.Bar{
		{Idx: 0, Symbol: "B", Date: "2026-01-02", High: 50.0, Low: 48.0, Close: 50.0},
		{Idx: 1, Symbol: "B", Date: "2026-01-05", High: 49.0, Low: 47.0, Close: 48.0},
		{Idx: 2, Symbol: "B", Date: "2026-01-06", High: 47.0, Low: 45.0, Close: 46.0},
		{Idx: 3, Symbol: "B", Date: "2026-01-07", High: 45.0, Low: 43.0, Close: 44.0},
		{Idx: 4, Symbol: "B", Date: "2026-01-08", High: 43.0, Low: 41.0, Close: 42.0}, // 4 down closes
		{Idx: 5, Symbol: "B", Date: "2026-01-09", High: 45.0, Low: 42.0, Close: 43.0}, // up day
	}

	barsBySymbol := map[string][]models.Bar{
		"A": barsA,
		"B": barsB,
	}

	strat := &MillwharfStrategy{
		MinStreak:          5,
		TakeProfitLookback: 6,
		MaxProfitCap:       1.20,
		HoldingWindow:      4,
	}
	signals := strat.GenerateSignals(barsBySymbol)

	if len(signals) != 1 {
		t.Fatalf("Expected exactly 1 weekly signal (for Symbol A), got %d", len(signals))
	}

	sig := signals[0]
	if sig.Symbol != "A" {
		t.Errorf("Expected signal symbol 'A', got '%s'", sig.Symbol)
	}
	if sig.Date != "2026-01-09" {
		t.Errorf("Expected signal date '2026-01-09', got '%s'", sig.Date)
	}
	if sig.Metadata["decline_streak"] != 5 {
		t.Errorf("Expected decline streak 5, got %v", sig.Metadata["decline_streak"])
	}
	// Take profit capped at +20% (75 * 1.20 = 90) because 6-day high 102 is higher
	if sig.TakeProfit != 90.0 {
		t.Errorf("Expected TakeProfit 90.0, got %f", sig.TakeProfit)
	}
}

func TestMillwharfTakeProfitLast6DaysHigh(t *testing.T) {
	// Test case where 6-day high is LESS than +20% gain
	// Day 0: 50.0 (High 51.0)
	// Day 1: 49.5 (High 50.5)
	// Day 2: 49.0 (High 49.8)
	// Day 3: 48.5 (High 49.2)
	// Day 4: 48.0 (High 48.7)
	// Day 5: 47.5 (High 48.0)
	// 6-day max high is 51.0.
	// 47.5 * 1.20 = 57.0.
	// Since 51.0 < 57.0, TakeProfit should be exactly 51.0!
	bars := []models.Bar{
		{Idx: 0, Symbol: "T", Date: "2026-01-02", High: 51.0, Low: 49.5, Close: 50.0},
		{Idx: 1, Symbol: "T", Date: "2026-01-05", High: 50.5, Low: 49.0, Close: 49.5},
		{Idx: 2, Symbol: "T", Date: "2026-01-06", High: 49.8, Low: 48.5, Close: 49.0},
		{Idx: 3, Symbol: "T", Date: "2026-01-07", High: 49.2, Low: 48.0, Close: 48.5},
		{Idx: 4, Symbol: "T", Date: "2026-01-08", High: 48.7, Low: 47.5, Close: 48.0},
		{Idx: 5, Symbol: "T", Date: "2026-01-09", High: 48.0, Low: 47.0, Close: 47.5},
	}

	barsBySymbol := map[string][]models.Bar{"T": bars}
	strat := &MillwharfStrategy{MinStreak: 5, TakeProfitLookback: 6, MaxProfitCap: 1.20, HoldingWindow: 4}
	signals := strat.GenerateSignals(barsBySymbol)

	if len(signals) != 1 {
		t.Fatalf("Expected 1 signal, got %d", len(signals))
	}
	if signals[0].TakeProfit != 51.0 {
		t.Errorf("Expected TakeProfit to be 51.0 (6-day high), got %f", signals[0].TakeProfit)
	}
}
