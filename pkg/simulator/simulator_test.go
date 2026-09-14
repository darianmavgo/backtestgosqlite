package simulator

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

func TestEvaluateTradeOutcome(t *testing.T) {
	// Target +20% / Stop -7% — arbitrary fixture values, not tied to any
	// particular registered strategy's config (previously borrowed "wc"'s
	// DefaultConfig(), which broke when that strategy was archived).
	config := strategy.StrategyConfig{TargetPct: 1.20, StopLossPct: 0.93}

	futureBarsWinning := []models.Bar{
		{Date: "2023-01-02", Open: 100, High: 105, Low: 98, Close: 104},
		{Date: "2023-01-03", Open: 105, High: 122, Low: 103, Close: 121}, // Hits 122 >= 120
	}

	tradeWin := EvaluateTradeOutcome(config, "TQQQ", 1, "2023-01-01", 100.0, futureBarsWinning)
	if tradeWin.ExitReason != models.ExitReasonProfitTarget {
		t.Errorf("expected profit target exit, got %s", tradeWin.ExitReason)
	}
	if tradeWin.HoldDays != 2 {
		t.Errorf("expected hold days 2, got %d", tradeWin.HoldDays)
	}

	futureBarsStopped := []models.Bar{
		{Date: "2023-01-02", Open: 100, High: 102, Low: 91, Close: 92}, // Low 91 <= 93
	}

	tradeLoss := EvaluateTradeOutcome(config, "SOXL", 2, "2023-01-01", 100.0, futureBarsStopped)
	if tradeLoss.ExitReason != models.ExitReasonStopLoss {
		t.Errorf("expected stop loss exit, got %s", tradeLoss.ExitReason)
	}
	if tradeLoss.HoldDays != 1 {
		t.Errorf("expected hold days 1, got %d", tradeLoss.HoldDays)
	}
}
