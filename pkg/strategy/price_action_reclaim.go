package strategy

import (
	"math"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// PriceActionReclaimStrategy implements a strategy based on "Once You Learn Price Action"
// four pillars:
// 1. Map: Find a significant support level.
// 2. Confirmation: Price drops below support and then reclaims it (closes above).
// 3. Invalidation: Stop loss placed at the support level, dynamically calculated.
// 4. Context: Broader trend must be upwards (e.g., price above 200 SMA).
type PriceActionReclaimStrategy struct {
	marketDBPath string
	calcDBPath   string
}

func init() {
	Register(&PriceActionReclaimStrategy{})
}

func (s *PriceActionReclaimStrategy) ID() string   { return "price-action-reclaim" }
func (s *PriceActionReclaimStrategy) Name() string { return "Price Action Reclaim" }
func (s *PriceActionReclaimStrategy) Description() string {
	return "Trades based on price action: Support established, broken, and reclaimed in an uptrending context."
}

func (s *PriceActionReclaimStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		PositionSizing:     "fixed_pct",
		AllocationPct:      0.10,
		TakeProfitPct:      0.10, // Default 10% TP
		HoldingWindow:      20,
		PositionCap:        1,
		SlippagePct:        0.0005,
		CommissionPerShare: 0.0001,
	}
}

func (s *PriceActionReclaimStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *PriceActionReclaimStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *PriceActionReclaimStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var signals []models.Signal


	for symbol, bars := range barsBySymbol {
		if len(bars) < 250 {
			continue // Need history for SMA200 and support window
		}

		// Lookback window for establishing support
		const lookbackStart = 25
		const lookbackEnd = 5

		for i := lookbackStart; i < len(bars); i++ {
			// Pillar 1: Map (Support Level)
			// Establish support as the minimum low in a past window, offset slightly
			// so the breakdown doesn't lower the support level itself.
			var support float64 = math.MaxFloat64
			for j := i - lookbackStart; j <= i - lookbackEnd; j++ {
				if bars[j].Low < support {
					support = bars[j].Low
				}
			}

			// Pillar 4: Context
			// Broader market context check using SMA200.
			if bars[i].SMA200 == 0 || bars[i].Close < bars[i].SMA200 {
				continue // Must be in a broader uptrend
			}

			// Pillar 2: Confirmation (Break and Reclaim)
			// Break down: the previous bar closed below support.
			// Reclaim: the current bar closes back above support.
			reclaimed := false

			if bars[i-1].Close < support && bars[i].Close > support {
				reclaimed = true
			}

			if reclaimed {
				// Pillar 3: Invalidation
				// Stop loss is placed at the support level.
				stopLossPrice := support * 0.995
				stopLossMult := stopLossPrice / bars[i].Close

				// Filter out trades with massive risk (stop loss > 15% away)
				if stopLossMult < 0.85 {
					continue
				}

				signals = append(signals, models.Signal{
					Idx:        bars[i].Idx,
					Symbol:     symbol,
					Date:       bars[i].Date,
					Open:       bars[i].Open,
					High:       bars[i].High,
					Low:        bars[i].Low,
					Close:      bars[i].Close,
					Volume:     bars[i].Volume,
					BuyLimit:   bars[i].Close,
					Entry:      1,
					StopLoss:   stopLossPrice,
					TakeProfit: 0, // let simulator compute via TakeProfitPct
					Direction:  "LONG",
					Regime:     "Price > SMA200",
					StrategyID: s.ID(),
					Priority:   0,
				})
			}
		}
	}
	return signals
}
