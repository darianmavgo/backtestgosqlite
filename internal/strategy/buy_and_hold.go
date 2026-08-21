package strategy

import (
	"sort"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// BuyAndHoldStrategy buys on the first available trading day and holds across the entire backtest window.
type BuyAndHoldStrategy struct{}

func init() {
	Register(&BuyAndHoldStrategy{})
}

func (s *BuyAndHoldStrategy) ID() string {
	return "buy-and-hold"
}

func (s *BuyAndHoldStrategy) Name() string {
	return "Buy and Hold (Passive Benchmark Baseline)"
}

func (s *BuyAndHoldStrategy) Description() string {
	return "Buys on the first available historical bar and holds through the entire backtest horizon without stops or profit targets."
}

func (s *BuyAndHoldStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "buy-and-hold",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          999.0,  // Never exit via profit target
		StopLossPct:        0.0001, // Never exit via stop loss
		HoldingWindow:      99999,  // Never exit via time limit
		PositionCap:        10,     // Allow holding all target universe assets
		AllocationPct:      0.10,   // Equal-weight allocation across portfolio
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *BuyAndHoldStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *BuyAndHoldStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var signals []models.Signal

	for sym, bars := range barsBySymbol {
		if len(bars) == 0 {
			continue
		}
		// Signal on the very first bar
		firstBar := bars[0]
		signals = append(signals, models.Signal{
			Idx:       firstBar.Idx,
			Symbol:    sym,
			Date:      firstBar.Date,
			Open:      firstBar.Open,
			High:      firstBar.High,
			Low:       firstBar.Low,
			Close:     firstBar.Close,
			Volume:    firstBar.Volume,
			BuyLimit:  firstBar.Close,
			OrderType: "market",
			Entry:     1,
		})
	}

	sort.Slice(signals, func(i, j int) bool {
		if signals[i].Date == signals[j].Date {
			return signals[i].Symbol < signals[j].Symbol
		}
		return signals[i].Date < signals[j].Date
	})

	return signals
}
