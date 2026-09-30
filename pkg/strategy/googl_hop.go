package strategy

import (
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// GooglHopStrategy implements a trend-following buy-and-hold strategy on GOOGL with:
//   - Exit: A 7.0% trailing stop loss from the highest peak high reached since entry.
//     This replaces the brittle 3% noise-trigger that caused excessive whipsaws.
//   - Re-entry: Momentum recovery signal — requires GOOGL to close back above its
//     20-day Simple Moving Average (SMA20) after exiting, filtering out sustained
//     downtrends and buying back in when upward momentum resumes.
type GooglHopStrategy struct{}

func init() {
	s := &GooglHopStrategy{}
	Register(s)
	RegisterAlias("googl-hop", s)
}

func (s *GooglHopStrategy) ID() string { return "googl-hop" }

func (s *GooglHopStrategy) Name() string { return "GOOGL 7% Trailing Stop & SMA20 Re-Entry Hop" }

func (s *GooglHopStrategy) Description() string {
	return "Buys and holds GOOGL with 100% equity allocation using a 7.0% trailing stop loss. " +
		"Upon exit, stays in cash during pullbacks and hops back in when Close reclaims the 20-day SMA."
}

func (s *GooglHopStrategy) RequiredSymbols() []string { return []string{"GOOGL"} }

func (s *GooglHopStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                  s.ID(),
		Name:                s.Name(),
		Description:         s.Description(),
		Benchmark:           "SPY",
		PositionSizing:      "fixed_pct",
		AllocationPct:       1.0,   // 100% allocation of portfolio equity
		TargetPct:           999.0, // no arbitrary take-profit; rides entire multi-month/year bull runs
		StopLossPct:         0.001, // 7% trailing stop handles risk control
		UseTrailingStop:     true,  // activate trailing stop
		TrailingStopPct:     0.07,  // 7.0% trailing stop calibrated above single-stock daily noise
		ReentryCooldownDays: 1,     // minimum 1-day breath after exit to prevent same-day re-whipsaw
		HoldingWindow:       99999, // indefinite hold until trailing stop
		PositionCap:         1,     // single asset focus
		SlippagePct:         0.0005,
		CommissionPerShare:  0.0001,
	}
}

func (s *GooglHopStrategy) Validate() error { return ValidateConfig(s.DefaultConfig()) }

func (s *GooglHopStrategy) SetDatabases(marketDBPath, calcDBPath string) {}

func (s *GooglHopStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var bars []models.Bar
	for sym, b := range barsBySymbol {
		if strings.EqualFold(sym, "GOOGL") {
			bars = b
			break
		}
	}
	if len(bars) == 0 {
		return nil
	}

	sma20 := CalcSMA(bars, 20)

	signals := make([]models.Signal, 0, len(bars))
	for i, b := range bars {
		d := b.Date
		if len(d) >= 10 {
			d = d[:10]
		}

		// Initial entry on bar 0 to start buy-and-hold
		// Subsequent re-entries: only emit when Close > SMA20 (positive short-to-medium term momentum)
		canEnter := (i == 0)
		if i >= 20 && b.Close > sma20[i] {
			canEnter = true
		}

		if canEnter {
			signals = append(signals, models.Signal{
				Idx:        b.Idx,
				Symbol:     "GOOGL",
				Date:       d,
				Open:       b.Open,
				High:       b.High,
				Low:        b.Low,
				Close:      b.Close,
				Volume:     b.Volume,
				BuyLimit:   b.Close,
				OrderType:  "market",
				Entry:      1,
				Direction:  "LONG",
				Regime:     "Close>SMA20",
				AssetClass: "equity",
				StrategyID: s.ID(),
			})
		}
	}
	return signals
}
