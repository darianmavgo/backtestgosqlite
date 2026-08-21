package strategy

import (
	"sort"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// BBCapitulationStrategy implements the Bollinger Band Capitulation + Reversal Bounce strategy.
type BBCapitulationStrategy struct{}

func init() {
	Register(&BBCapitulationStrategy{})
}

func (s *BBCapitulationStrategy) ID() string {
	return "bb-capitulation"
}

func (s *BBCapitulationStrategy) Name() string {
	return "BB-Capitulation + Reversal Bounce"
}

func (s *BBCapitulationStrategy) Description() string {
	return "Enters on positive reversal confirmation candle after piercing Lower Bollinger Band (20, 2.0) with RSI(5) < 30."
}

func (s *BBCapitulationStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "bb-capitulation",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          1.18,   // +18% take-profit target
		StopLossPct:        0.93,   // -7% protective stop loss
		HoldingWindow:      10,     // 10-day max holding
		PositionCap:        5,      // Max 5 positions
		AllocationPct:      0.20,   // 20% equity per position
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *BBCapitulationStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var signals []models.Signal

	for sym, bars := range barsBySymbol {
		if len(bars) < 25 {
			continue
		}
		bb := CalcBollinger(bars, 20, 2.0)
		rsi5 := CalcRSI(bars, 5)

		for i := 22; i < len(bars); i++ {
			// Day T-1: Low pierced Lower Bollinger Band AND RSI(5) < 30 (Capitulation)
			// Day T: Reversal green confirmation candle (Close[T] > Close[T-1])
			if bars[i-1].Low < bb.Lower[i-1] && rsi5[i-1] < 30 && bars[i].Close > bars[i-1].Close {
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
