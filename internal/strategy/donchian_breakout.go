package strategy

import (
	"sort"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// DonchianBreakoutStrategy implements the classic 20-day Donchian Channel breakout strategy (Turtle Trading style).
type DonchianBreakoutStrategy struct{}

func init() {
	Register(&DonchianBreakoutStrategy{})
}

func (s *DonchianBreakoutStrategy) ID() string {
	return "donchian-breakout"
}

func (s *DonchianBreakoutStrategy) Name() string {
	return "Donchian 20-Day Momentum Breakout"
}

func (s *DonchianBreakoutStrategy) Description() string {
	return "Trend-following breakout: buys when price exceeds the 20-day high with trailing stop loss protection."
}

func (s *DonchianBreakoutStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "donchian-breakout",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          1.25,   // +25% target
		StopLossPct:        0.92,   // -8% initial stop loss
		UseTrailingStop:    true,   // Trail stops from peaks
		TrailingStopPct:    0.06,   // 6% trailing stop
		HoldingWindow:      20,     // 20-day holding horizon
		PositionCap:        5,      // Max 5 positions
		AllocationPct:      0.20,   // 20% equity per position
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *DonchianBreakoutStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *DonchianBreakoutStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var signals []models.Signal

	for sym, bars := range barsBySymbol {
		if len(bars) < 25 {
			continue
		}
		dc := CalcDonchian(bars, 20)

		for i := 20; i < len(bars); i++ {
			// Day T-1 high was upper channel; Day T close breaks above previous day's upper band
			if bars[i].Close > dc.Upper[i-1] {
				signals = append(signals, models.Signal{
					Idx:       bars[i].Idx,
					Symbol:    sym,
					Date:      bars[i].Date,
					Open:      bars[i].Open,
					High:      bars[i].High,
					Low:       bars[i].Low,
					Close:     bars[i].Close,
					Volume:    bars[i].Volume,
					BuyLimit:  bars[i].Close,
					OrderType: "market",
					Entry:     1,
				})
			}
		}
	}

	sort.Slice(signals, func(i, j int) bool {
		if signals[i].Date == signals[j].Date {
			return signals[i].Symbol < signals[j].Symbol
		}
		return signals[i].Date < signals[j].Date
	})

	return signals
}
