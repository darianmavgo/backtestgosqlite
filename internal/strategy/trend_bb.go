package strategy

import (
	"sort"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// TrendBBOversoldStrategy implements the Trend-Gated Bollinger Oversold strategy.
type TrendBBOversoldStrategy struct{}

func init() {
	Register(&TrendBBOversoldStrategy{})
}

func (s *TrendBBOversoldStrategy) ID() string {
	return "trend-bb"
}

func (s *TrendBBOversoldStrategy) Name() string {
	return "Trend-Gated Bollinger Oversold"
}

func (s *TrendBBOversoldStrategy) Description() string {
	return "Buys oversold capitulation dips (Low < Lower BB & RSI5 < 30) strictly when asset is in a macro uptrend (Close > SMA50)."
}

func (s *TrendBBOversoldStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "trend-bb",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          1.20,   // +20% target
		StopLossPct:        0.94,   // -6% stop loss
		HoldingWindow:      10,     // 10-day max holding
		PositionCap:        5,      // Max 5 positions
		AllocationPct:      0.20,   // 20% equity per position
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *TrendBBOversoldStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *TrendBBOversoldStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var signals []models.Signal

	for sym, bars := range barsBySymbol {
		if len(bars) < 55 {
			continue
		}
		bb := CalcBollinger(bars, 20, 2.0)
		rsi5 := CalcRSI(bars, 5)
		sma50 := CalcSMA(bars, 50)

		for i := 50; i < len(bars); i++ {
			// Trend Filter: In macro bull trend (Close > SMA50)
			// Oversold Trigger: Low pierced lower BB AND RSI(5) < 30
			if bars[i].Close > sma50[i] && bars[i].Low < bb.Lower[i] && rsi5[i] < 30 {
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
