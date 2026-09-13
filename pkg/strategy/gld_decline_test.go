package strategy

import (
	"math"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestGLDDecline_GenerateSignals_PureGo(t *testing.T) {
	s := NewGLDDeclineStrategy()

	// 5 days of bars: 3 consecutive drops on days 2, 3, 4
	mockBars := []models.Bar{
		{Idx: 0, Symbol: "GLD", Date: "2023-01-03", Close: 170.00, Open: 170.00, High: 171.00, Low: 169.00, Volume: 1000},
		{Idx: 1, Symbol: "GLD", Date: "2023-01-04", Close: 168.00, Open: 169.00, High: 169.50, Low: 167.50, Volume: 1000}, // Drop 1
		{Idx: 2, Symbol: "GLD", Date: "2023-01-05", Close: 166.00, Open: 167.00, High: 167.50, Low: 165.50, Volume: 1000}, // Drop 2
		{Idx: 3, Symbol: "GLD", Date: "2023-01-06", Close: 164.00, Open: 165.00, High: 165.50, Low: 163.50, Volume: 1000}, // Drop 3 -> SIGNAL
		{Idx: 4, Symbol: "GLD", Date: "2023-01-09", Close: 165.00, Open: 164.00, High: 166.00, Low: 164.00, Volume: 1000}, // Up
	}

	barsBySymbol := map[string][]models.Bar{
		"GLD": mockBars,
	}

	sigs := s.GenerateSignals(barsBySymbol)
	if len(sigs) != 1 {
		t.Fatalf("expected 1 signal on day 4 (index 3), got %d", len(sigs))
	}

	sig := sigs[0]
	if sig.Symbol != "GLD" {
		t.Errorf("expected symbol GLD, got %s", sig.Symbol)
	}
	if sig.Date != "2023-01-06" {
		t.Errorf("expected date 2023-01-06, got %s", sig.Date)
	}
	if sig.Direction != "LONG" {
		t.Errorf("expected LONG direction, got %s", sig.Direction)
	}
	expectedTP := 164.00 * 1.05
	if math.Abs(sig.TakeProfit-expectedTP) > 0.0001 {
		t.Errorf("expected TP %.2f, got %.2f", expectedTP, sig.TakeProfit)
	}
	if sig.HoldDaysOverride != 8 {
		t.Errorf("expected 8-day hold override, got %d", sig.HoldDaysOverride)
	}
	if sig.StopLoss != 0.0 {
		t.Errorf("expected 0.0 stop loss, got %.2f", sig.StopLoss)
	}
}

func TestGLDDecline_SQLPipeline(t *testing.T) {
	AutoRegisterSQLStrategies("../..", "../../data/market_history.db")
	s := NewGLDDeclineStrategy()
	s.SetDatabases("../../data/market_history.db", ":memory:")
	sigs := s.GenerateSignals(map[string][]models.Bar{})
	if len(sigs) == 0 {
		t.Fatalf("expected non-zero signals generated for GLDDecline via SQL pipeline, got 0")
	}

	for _, sig := range sigs {
		if sig.Symbol != "GLD" {
			t.Errorf("expected symbol GLD, got %s", sig.Symbol)
		}
		if sig.Direction != "LONG" {
			t.Errorf("expected LONG direction, got %s", sig.Direction)
		}
		if sig.HoldDaysOverride != 8 {
			t.Errorf("expected 8-day hold override, got %d", sig.HoldDaysOverride)
		}
	}
}
