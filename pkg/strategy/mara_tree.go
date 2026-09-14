package strategy

import (
	"math"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// MARATreeStrategy implements the MARA Decision Tree Buy Signal strategy using the
// "Precision 200-SMA Re-test Bounce" model discovered via CloudForest.
//
// Entry Logic:
//   - LONG MARA when either:
//     1. Volatility Compression Coil: Day's range <= 0.45 * 14-day ATR
//     2. Precision 200-SMA Bounce: Price is re-testing the 200-day SMA (-0.68% to +3.38% vs SMA200)
//
// Exit Logic:
//   - +5% Take-Profit
//   - -8% Stop-Loss
//   - 1-day holding window
type MARATreeStrategy struct {
	marketDBPath string
	calcDBPath   string
}

// NewMARATreeStrategy constructs and registers the strategy.
func NewMARATreeStrategy() *MARATreeStrategy {
	s := &MARATreeStrategy{}
	Register(s)
	RegisterAlias("mara_tree", s)
	RegisterAlias("mara-tree", s)
	RegisterAlias("MARATree", s)
	RegisterAlias("maratree", s)
	return s
}

func (s *MARATreeStrategy) ID() string {
	return "mara_tree"
}

func (s *MARATreeStrategy) Name() string {
	return "MARA Decision Tree (Precision 200-SMA Bounce)"
}

func (s *MARATreeStrategy) Description() string {
	return "Long MARA on CloudForest Decision Tree: Volatility Coil (Range <= 0.45 ATR14) or 200-SMA Bounce Re-test (-0.68% to +3.38%). " +
		"+5% Take-Profit, -8% Stop-Loss, 1-day hold."
}

func (s *MARATreeStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

// RequiredSymbols declares MARA as the target asset.
func (s *MARATreeStrategy) RequiredSymbols() []string {
	return []string{"MARA"}
}

// DefaultConfig returns the operational risk and portfolio settings.
func (s *MARATreeStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "MARA",
		Timeframe:          "1d",
		PositionSizing:     "fixed_pct",
		AllocationPct:      0.65,  // 65% available capital per position
		TargetPct:          1.05,  // +5% take-profit multiplier
		TakeProfitPct:      0.05,  // +5% take-profit fractional offset
		StopLossPct:        0.92,  // -8% stop-loss floor multiplier (entry * 0.92)
		HoldingWindow:      1,     // 1 trading day hold
		PositionCap:        1,     // Single position at a time
		CashYieldAnnual:    0.045, // 4.5% idle cash T-bill yield
		SlippagePct:        0.0005,
		CommissionPerShare: 0.0001,
	}
}

func (s *MARATreeStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

// GenerateSignals evaluates historical bars for MARA and generates entry triggers.
func (s *MARATreeStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var bars []models.Bar
	var ok bool

	// Find MARA bars (handling case variations)
	for sym, b := range barsBySymbol {
		if strings.EqualFold(sym, "MARA") {
			bars = b
			ok = true
			break
		}
	}

	if !ok {
		return nil
	}

	return TreeBounceSignals("MARA", bars, 0.05, 0.08, 1)
}

// TreeBounceSignals evaluates the "Precision 200-SMA Re-test Bounce" decision tree
// (originally discovered for MARA via CloudForest) against an arbitrary symbol's bars.
// Entry fires on either a volatility compression coil (day's range <= 0.45 * ATR14) or
// a 200-SMA re-test bounce (price -0.68% to +3.38% vs SMA200).
func TreeBounceSignals(symbol string, bars []models.Bar, tpPct, slPct float64, holdDays int) []models.Signal {
	if len(bars) < 201 {
		return nil
	}

	var signals []models.Signal

	// We need 200 bars for SMA200 and 14 bars for ATR14
	for i := 200; i < len(bars); i++ {
		curr := bars[i]

		// 1. Calculate 200-day Simple Moving Average
		var sum200 float64
		for j := 0; j < 200; j++ {
			sum200 += bars[i-j].Close
		}
		sma200 := sum200 / 200.0
		priceVsSma200 := (curr.Close - sma200) / sma200 * 100.0

		// 2. Calculate 14-day Average True Range (ATR14)
		var trSum float64
		for j := i - 13; j <= i; j++ {
			tr := math.Max(
				bars[j].High-bars[j].Low,
				math.Max(
					math.Abs(bars[j].High-bars[j-1].Close),
					math.Abs(bars[j].Low-bars[j-1].Close),
				),
			)
			trSum += tr
		}
		atr14 := trSum / 14.0
		currRange := curr.High - curr.Low
		rangeRatio := 1.0
		if atr14 > 0 {
			rangeRatio = currRange / atr14
		}

		// 3. Precision 200-SMA Decision Tree Rule (Depth 3):
		// - Condition A: RangeVsATR14 <= 0.45 (Volatility Coil / Compression)
		// - Condition B: PriceVsSMA200 > -0.68% AND PriceVsSMA200 <= 3.38% (200 SMA re-test bounce)
		isBuy := false
		if rangeRatio <= 0.45 {
			isBuy = true
		} else if priceVsSma200 <= 3.38 && priceVsSma200 > -0.68 {
			isBuy = true
		}

		if isBuy {
			d := curr.Date
			if len(d) >= 10 {
				d = d[:10]
			}
			closePrice := curr.Close
			var takeProfit, stopLoss float64
			if tpPct > 0 {
				takeProfit = closePrice * (1.0 + tpPct)
			}
			if slPct > 0 {
				stopLoss = closePrice * (1.0 - slPct)
			}
			signals = append(signals, models.Signal{
				Idx:              curr.Idx,
				Symbol:           symbol,
				Date:             d,
				Open:             curr.Open,
				High:             curr.High,
				Low:              curr.Low,
				Close:            closePrice,
				Volume:           curr.Volume,
				BuyLimit:         closePrice,
				Entry:            1,
				OrderType:        "limit",
				Direction:        "LONG",
				Regime:           "All Regimes",
				TakeProfit:       takeProfit,
				StopLoss:         stopLoss,
				HoldDaysOverride: holdDays,
				AssetClass:       "equity",
				StrategyID:       "tree_bounce",
				Priority:         0,
				Metadata: map[string]float64{
					"price_vs_sma200": priceVsSma200,
					"range_vs_atr14":  rangeRatio,
				},
			})
		}
	}

	return signals
}

// ParameterSpace returns parameter search dimensions for optimization.
func (s *MARATreeStrategy) ParameterSpace() ParameterSpace {
	cfg := s.DefaultConfig()
	return ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      []string{"MARA"},
		SignalSymbol: "MARA",
		Direction:    "tree_bounce",
		SignalDays:   []int{1},
		HoldDays:     []int{1, 2, 3, 4, 5, 6, 8, 10},
		TakeProfits:  []float64{0.03, 0.04, 0.05, 0.06, 0.07, 0.08, 0.10, 0.12, 0.15},
		StopLosses:   []float64{0.03, 0.04, 0.05, 0.06, 0.07, 0.08, 0.10},
		Regimes:      []string{"All Regimes"},
		Allocations:  []float64{cfg.AllocationPct},
		CashYield:    cfg.CashYieldAnnual,
		Baseline: BaselineParams{
			SignalDays: 1,
			HoldDays:   cfg.HoldingWindow, // 1
			TakeProfit: cfg.TakeProfitPct, // 0.05
			StopLoss:   0.08,              // 0.08 (-8% stop loss)
			Allocation: cfg.AllocationPct,
			Regime:     "All Regimes",
		},
	}
}

func init() {
	NewMARATreeStrategy()
}
