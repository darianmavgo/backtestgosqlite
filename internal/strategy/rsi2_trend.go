package strategy

import (
	"sort"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// RSI2TrendStrategy implements the Connors RSI(2) Trend Pullback strategy.
type RSI2TrendStrategy struct{}

func init() {
	Register(&RSI2TrendStrategy{})
}

func (s *RSI2TrendStrategy) ID() string {
	return "rsi2"
}

func (s *RSI2TrendStrategy) Name() string {
	return "Connors RSI(2) Trend Pullback"
}

func (s *RSI2TrendStrategy) Description() string {
	return "Buys deep 2-period RSI oversold (< 10) pullbacks when asset is trading above its 50-day moving average."
}

func (s *RSI2TrendStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "rsi2",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          1.10,   // +10% target
		StopLossPct:        0.94,   // -6% stop loss
		HoldingWindow:      6,      // 6-day max holding
		PositionCap:        5,      // Max 5 positions
		AllocationPct:      0.20,   // 20% equity per position
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *RSI2TrendStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *RSI2TrendStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var signals []models.Signal

	for sym, bars := range barsBySymbol {
		if len(bars) < 55 {
			continue
		}
		sma50 := CalcSMA(bars, 50)
		rsi2 := CalcRSI(bars, 2)

		for i := 50; i < len(bars); i++ {
			if bars[i].Close > sma50[i] && rsi2[i] < 10.0 && rsi2[i] > 0 {
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
