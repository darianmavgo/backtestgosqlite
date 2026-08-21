package strategy

import (
	"sort"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// WC4DayHoldStrategy buys on Whitings Creek decline signal and strictly exits after 4 trading days without stop-loss or profit-target.
type WC4DayHoldStrategy struct{}

func init() {
	Register(&WC4DayHoldStrategy{})
}

func (s *WC4DayHoldStrategy) ID() string {
	return "wc-4d"
}

func (s *WC4DayHoldStrategy) Name() string {
	return "Whitings Creek (4-Day Time Exit)"
}

func (s *WC4DayHoldStrategy) Description() string {
	return "Buys on Whitings Creek decline signal (Close < 0.90 * 10-day min low) and exits unconditionally 4 trading days later (no stop-loss, no profit-target)."
}

func (s *WC4DayHoldStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "wc-4d",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          99.0,   // Effectively no early profit target exit
		StopLossPct:        0.0001, // Effectively no stop loss (holds through drawdowns)
		HoldingWindow:      4,      // Exactly 4 trading days holding window
		PositionCap:        5,      // Max 5 concurrent positions
		AllocationPct:      0.20,   // 20% equity per position
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *WC4DayHoldStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *WC4DayHoldStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var signals []models.Signal

	for sym, bars := range barsBySymbol {
		if len(bars) < 15 {
			continue
		}
		for i := 12; i < len(bars); i++ {
			// Trailing minimum low from Day -3 to Day -12
			minLow := bars[i-3].Low
			for j := 4; j <= 12; j++ {
				if bars[i-j].Low < minLow {
					minLow = bars[i-j].Low
				}
			}

			// Cliff condition: Close < 0.90 * minLow
			if bars[i].Close < 0.90*minLow {
				signals = append(signals, models.Signal{
					Idx:      bars[i].Idx,
					Symbol:   sym,
					Date:     bars[i].Date,
					Open:     bars[i].Open,
					High:     bars[i].High,
					Low:      bars[i].Low,
					Close:    bars[i].Close,
					Volume:   bars[i].Volume,
					BuyLimit: bars[i].Close,
					Entry:    1,
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
