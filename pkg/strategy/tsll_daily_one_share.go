package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// TSLLDailyOneShareStrategy buys exactly one share of TSLL (Direxion Daily
// TSLA Bull 2X) at every trading day's close and holds it one day, exiting on
// +5% take-profit, -20% stop-loss, or the next bar's close, whichever first.
//
// Unconditional entry: there is no signal filter, so this measures the raw
// edge (or lack of one) in a 2x leveraged single-stock ETF with a lopsided
// 5% target / 20% stop. TakeProfitPct is a fractional offset (0.05 = +5%);
// StopLossPct is a direct multiplier (0.80 = -20%), like every other strategy.
type TSLLDailyOneShareStrategy struct{}

func init() {
	s := &TSLLDailyOneShareStrategy{}
	Register(s)
	RegisterAlias("tsll_daily_one_share", s)
	RegisterAlias("tsll-daily", s)
}

func (s *TSLLDailyOneShareStrategy) ID() string { return "tsll-daily-one-share" }

func (s *TSLLDailyOneShareStrategy) Name() string { return "TSLL Daily 1-Share, 1-Day Hold" }

func (s *TSLLDailyOneShareStrategy) Description() string {
	return "Buys one share of TSLL every trading day at the close and holds one day " +
		"(+5% take-profit / -20% stop-loss / 1d max hold)."
}

func (s *TSLLDailyOneShareStrategy) RequiredSymbols() []string { return []string{"TSLL"} }

// MinHistoryBars implements MinHistoryProvider: only the latest bar is needed.
func (s *TSLLDailyOneShareStrategy) MinHistoryBars() int { return 2 }

func (s *TSLLDailyOneShareStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "TSLL",
		PositionSizing:     "fixed_shares",
		FixedShares:        1,
		AllocationPct:      1.0, // required by ValidateConfig; sizing is by FixedShares
		TargetPct:          1.05,
		TakeProfitPct:      0.05,
		StopLossPct:        0.80,
		HoldingWindow:      1,
		PositionCap:        1, // the prior day's share is closed before the next entry
		SlippagePct:        0.0,
		CommissionPerShare: 0.0,
	}
}

func (s *TSLLDailyOneShareStrategy) Validate() error { return ValidateConfig(s.DefaultConfig()) }

func (s *TSLLDailyOneShareStrategy) SetDatabases(marketDBPath, calcDBPath string) {}

func (s *TSLLDailyOneShareStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	bars := barsBySymbol["TSLL"]
	if len(bars) == 0 {
		return nil
	}
	cfg := s.DefaultConfig()

	signals := make([]models.Signal, 0, len(bars))
	for _, b := range bars {
		d := b.Date
		if len(d) >= 10 {
			d = d[:10]
		}
		signals = append(signals, models.Signal{
			Idx:              b.Idx,
			Symbol:           "TSLL",
			Date:             d,
			Open:             b.Open,
			High:             b.High,
			Low:              b.Low,
			Close:            b.Close,
			Volume:           b.Volume,
			BuyLimit:         b.Close,
			OrderType:        "market",
			Entry:            1,
			Direction:        "LONG",
			Regime:           "All Regimes",
			TakeProfit:       b.Close * (1.0 + cfg.TakeProfitPct),
			StopLoss:         b.Close * cfg.StopLossPct,
			HoldDaysOverride: cfg.HoldingWindow,
			AssetClass:       "equity",
			StrategyID:       s.ID(),
		})
	}
	return signals
}
