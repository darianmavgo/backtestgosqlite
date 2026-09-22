package strategy

import (
	"math"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestTSLLDailyOneShare_GenerateSignals(t *testing.T) {
	s := &TSLLDailyOneShareStrategy{}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	bars := []models.Bar{
		{Idx: 0, Symbol: "TSLL", Date: "2025-01-02", Close: 10},
		{Idx: 1, Symbol: "TSLL", Date: "2025-01-03T00:00:00Z", Close: 12},
		{Idx: 2, Symbol: "TSLL", Date: "2025-01-06", Close: 9},
	}
	sigs := s.GenerateSignals(map[string][]models.Bar{"TSLL": bars, "AAPL": bars})
	if len(sigs) != 3 {
		t.Fatalf("expected one signal per TSLL bar, got %d", len(sigs))
	}
	if sigs[1].Date != "2025-01-03" {
		t.Errorf("date not trimmed: %s", sigs[1].Date)
	}
	if math.Abs(sigs[1].TakeProfit-12.6) > 1e-9 || math.Abs(sigs[1].StopLoss-9.6) > 1e-9 {
		t.Errorf("TP/SL = %.4f/%.4f, want 12.60/9.60", sigs[1].TakeProfit, sigs[1].StopLoss)
	}
	if sigs[0].HoldDaysOverride != 1 {
		t.Errorf("hold override = %d, want 1", sigs[0].HoldDaysOverride)
	}
	cfg := s.DefaultConfig()
	if cfg.PositionSizing != "fixed_shares" || cfg.FixedShares != 1 {
		t.Errorf("sizing = %s/%d, want fixed_shares/1", cfg.PositionSizing, cfg.FixedShares)
	}
}
