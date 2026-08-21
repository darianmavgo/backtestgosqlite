package analytics

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

func TestCalculatePerformanceMetrics(t *testing.T) {
	initialCapital := 10000.0

	trades := []models.Trade{
		{NetPnL: 500, ReturnPct: 0.05, HoldDays: 5},
		{NetPnL: 1000, ReturnPct: 0.10, HoldDays: 4},
		{NetPnL: -200, ReturnPct: -0.02, HoldDays: 3},
	}

	equityPoints := []models.DailyEquityPoint{
		{Date: "2023-01-01", TotalEquity: 10000.0},
		{Date: "2023-01-02", TotalEquity: 10500.0},
		{Date: "2023-01-03", TotalEquity: 11500.0},
		{Date: "2023-01-04", TotalEquity: 11300.0},
	}

	report := CalculatePerformanceMetrics(initialCapital, trades, equityPoints)

	if report.TotalTrades != 3 {
		t.Errorf("expected 3 trades, got %d", report.TotalTrades)
	}
	if report.WinningTrades != 2 {
		t.Errorf("expected 2 winners, got %d", report.WinningTrades)
	}
	if report.LosingTrades != 1 {
		t.Errorf("expected 1 loser, got %d", report.LosingTrades)
	}
	if report.NetProfit != 1300.0 {
		t.Errorf("expected net profit 1300, got %f", report.NetProfit)
	}
	if report.WinRate < 0.66 || report.WinRate > 0.67 {
		t.Errorf("expected win rate ~0.6667, got %f", report.WinRate)
	}
	if report.ProfitFactor != 7.5 { // (500 + 1000) / 200 = 7.5
		t.Errorf("expected profit factor 7.5, got %f", report.ProfitFactor)
	}
}
