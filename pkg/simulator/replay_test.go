package simulator

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

func replayCfg() strategy.StrategyConfig {
	return strategy.StrategyConfig{
		NextDayLimitEntry: true,
		HoldingWindow:     1,
		TakeProfitPct:     10, // keep the target out of reach
		StopLossPct:       0.5,
	}
}

func bar(date string, open, high, low, close float64) models.Bar {
	return models.Bar{Symbol: "TSLL", Date: date, Open: open, High: high, Low: low, Close: close, Volume: 1}
}

func TestReplayRoundTripFillsNextSessionAndExitsOnTime(t *testing.T) {
	sig := models.Signal{Symbol: "TSLL", Date: "2026-09-24", Close: 10, BuyLimit: 10, OrderType: "limit"}
	bars := []models.Bar{
		bar("2026-09-24", 10, 10.2, 9.8, 10),
		bar("2026-09-25", 10, 10.2, 9.5, 10.1),
		bar("2026-09-28", 10.1, 10.3, 10, 10.2),
	}
	got := ReplayRoundTrip(sig, bars, replayCfg())
	if got.Outcome != RoundTripClosed {
		t.Fatalf("outcome %s", got.Outcome)
	}
	if got.Trade.EntryDate != "2026-09-25" || got.Trade.EntryPrice != 10 {
		t.Fatalf("entry %s @ %v", got.Trade.EntryDate, got.Trade.EntryPrice)
	}
	if got.Trade.ExitDate != "2026-09-28" || got.Trade.ExitReason != models.ExitReasonTimeUp {
		t.Fatalf("exit %s %s", got.Trade.ExitDate, got.Trade.ExitReason)
	}
}

func TestReplayRoundTripUnfilledWhenLimitNeverTrades(t *testing.T) {
	sig := models.Signal{Symbol: "TSLL", Date: "2026-09-24", Close: 10, BuyLimit: 10}
	bars := []models.Bar{
		bar("2026-09-24", 10, 10, 10, 10),
		bar("2026-09-25", 11, 12, 10.5, 11.5),
	}
	got := ReplayRoundTrip(sig, bars, replayCfg())
	if got.Outcome != RoundTripUnfilled {
		t.Fatalf("outcome %s, trade %+v", got.Outcome, got.Trade)
	}
}

func TestReplayRoundTripFillsAtTheOpenWhenGappedThrough(t *testing.T) {
	sig := models.Signal{Symbol: "TSLL", Date: "2026-09-24", Close: 10, BuyLimit: 10}
	bars := []models.Bar{
		bar("2026-09-24", 10, 10, 10, 10),
		bar("2026-09-25", 9, 9.5, 8.8, 9.2),
		bar("2026-09-28", 9.2, 9.4, 9, 9.3),
	}
	got := ReplayRoundTrip(sig, bars, replayCfg())
	if got.Outcome != RoundTripClosed || got.Trade.EntryPrice != 9 {
		t.Fatalf("got %+v", got)
	}
}

func TestReplayRoundTripStaysOpenWhenBarsEnd(t *testing.T) {
	sig := models.Signal{Symbol: "TSLL", Date: "2026-09-24", Close: 10, BuyLimit: 10}
	bars := []models.Bar{
		bar("2026-09-24", 10, 10, 10, 10),
		bar("2026-09-25", 10, 10.2, 9.5, 10.1),
	}
	got := ReplayRoundTrip(sig, bars, replayCfg())
	if got.Outcome != RoundTripOpen || got.Trade.EntryPrice != 10 {
		t.Fatalf("got %+v", got)
	}
}
