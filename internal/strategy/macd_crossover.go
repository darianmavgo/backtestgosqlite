package strategy

import (
	"sort"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// MACDCrossoverStrategy implements the classic MACD signal-line bullish crossover strategy.
type MACDCrossoverStrategy struct{}

func init() {
	Register(&MACDCrossoverStrategy{})
}

func (s *MACDCrossoverStrategy) ID() string {
	return "macd-crossover"
}

func (s *MACDCrossoverStrategy) Name() string {
	return "MACD Signal Line Crossover"
}

func (s *MACDCrossoverStrategy) Description() string {
	return "Enters when the MACD line (12, 26) crosses above the 9-period Signal line below the zero line."
}

func (s *MACDCrossoverStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "macd-crossover",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          1.15,   // +15% target
		StopLossPct:        0.95,   // -5% stop loss
		HoldingWindow:      12,     // 12-day max holding
		PositionCap:        5,      // Max 5 positions
		AllocationPct:      0.20,   // 20% equity per position
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *MACDCrossoverStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *MACDCrossoverStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var signals []models.Signal

	for sym, bars := range barsBySymbol {
		if len(bars) < 40 {
			continue
		}
		macd := CalcMACD(bars, 12, 26, 9)

		for i := 36; i < len(bars); i++ {
			// MACD line crosses above Signal line while MACD line is below zero (bullish reversal momentum)
			prevCross := macd.MACD[i-1] <= macd.Signal[i-1]
			currCross := macd.MACD[i] > macd.Signal[i]
			isOversold := macd.MACD[i] < 0

			if prevCross && currCross && isOversold {
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
					OrderType: "limit",
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
