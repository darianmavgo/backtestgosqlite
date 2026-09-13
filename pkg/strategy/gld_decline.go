package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// GLDDeclineStrategy implements the GLD 2-Day Consecutive Decline Reversal strategy:
//
//   - LONG GLD when GLD closes down 2 consecutive days
//     (+8% take-profit, -2% stop-loss, 12-day max hold, 65% allocation)
//
// Structured similarly to the original VOO / TECL decline bounce strategy,
// but optimized for physical gold (GLD) as an uncorrelated asset.
//
// T-bill yield on idle cash is configured via DefaultConfig().CashYieldAnnual (4.5%).
type GLDDeclineStrategy struct {
	marketDBPath string
	calcDBPath   string
}

// NewGLDDeclineStrategy constructs and auto-registers the strategy.
func NewGLDDeclineStrategy() *GLDDeclineStrategy {
	s := &GLDDeclineStrategy{}
	Register(s)
	RegisterAlias("gld-decline", s)
	RegisterAlias("gld_decline", s)
	RegisterAlias("GLDDecline", s)
	RegisterAlias("glddecline", s)
	return s
}

func (s *GLDDeclineStrategy) ID() string { return "gld-decline" }

func (s *GLDDeclineStrategy) Name() string { return "GLD 2-Day Consecutive Decline Reversal" }

func (s *GLDDeclineStrategy) Description() string {
	return "Long GLD on 2-consecutive daily down-closes (+8% TP / -2% SL / 12d max hold). " +
		"65% allocation per trade with 4.5% T-bill yield on idle cash."
}

func (s *GLDDeclineStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

// RequiredSymbols returns the specific market symbols required by this strategy.
func (s *GLDDeclineStrategy) RequiredSymbols() []string {
	return []string{"GLD"}
}

// DefaultConfig returns the canonical GLD decline parameters.
func (s *GLDDeclineStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "GLD",
		AllocationPct:      0.65,
		TakeProfitPct:      0.08,  // +8% take-profit
		StopLossPct:        0.02,  // -2% stop-loss
		HoldingWindow:      12,    // 12-day max holding
		PositionCap:        1,     // One position at a time
		CashYieldAnnual:    0.045, // 4.5% idle cash APY
		SlippagePct:        0.0,
		CommissionPerShare: 0.0,
	}
}

func (s *GLDDeclineStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	if sqlStrat, exists := Get("gld_decline-sql"); exists {
		sqlStrat.SetDatabases(marketDBPath, calcDBPath)
	}
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *GLDDeclineStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// 1. Delegate signal generation directly to the canonical SQLite pipeline if registered
	if sqlStrat, exists := Get("gld_decline-sql"); exists {
		return sqlStrat.GenerateSignals(barsBySymbol)
	}
	if s.calcDBPath != "" && s.marketDBPath != "" {
		pipe := NewSQLPipelineStrategy("gld_decline-pipeline", s.Name(), s.Description(), "sql/strategies/gld_decline", s.DefaultConfig())
		pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
		return pipe.GenerateSignals(barsBySymbol)
	}

	// 2. Pure Go calculation fallback for in-memory backtesting and unit testing
	bars, ok := barsBySymbol["GLD"]
	if !ok || len(bars) < 3 {
		return nil
	}

	var signals []models.Signal
	streak := 0

	for i := 1; i < len(bars); i++ {
		if bars[i].Close < bars[i-1].Close {
			streak++
		} else {
			streak = 0
		}

		if streak >= 2 {
			d := bars[i].Date
			if len(d) >= 10 {
				d = d[:10]
			}
			closePrice := bars[i].Close
			signals = append(signals, models.Signal{
				Idx:              bars[i].Idx,
				Symbol:           "GLD",
				Date:             d,
				Open:             bars[i].Open,
				High:             bars[i].High,
				Low:              bars[i].Low,
				Close:            closePrice,
				Volume:           bars[i].Volume,
				BuyLimit:         closePrice,
				Entry:            1,
				OrderType:        "limit",
				Direction:        "LONG",
				Regime:           "All Regimes",
				TakeProfit:       closePrice * 1.08,
				StopLoss:         closePrice * 0.98,
				HoldDaysOverride: 12,
				AssetClass:       "commodity",
				StrategyID:       s.ID(),
				Priority:         0,
			})
		}
	}

	return signals
}

// ParameterSpace returns the tailored parameter search space centered around GLD decline defaults.
func (s *GLDDeclineStrategy) ParameterSpace() ParameterSpace {
	cfg := s.DefaultConfig()
	return ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      []string{"GLD"},
		SignalSymbol: "GLD",
		Direction:    "drop",
		SignalDays:   []int{2, 3, 4, 5},
		HoldDays:     []int{2, 3, 4, 5, 6, 8, 10, 12, 15},
		TakeProfits:  []float64{0.0, 0.02, 0.03, 0.04, 0.05, 0.06, 0.07, 0.08, 0.10},
		StopLosses:   []float64{0.0, 0.02, 0.03, 0.05, 0.07},
		Regimes:      []string{"All Regimes", "GLD>=SMA200", "GLD<SMA200"},
		Allocations:  []float64{cfg.AllocationPct},
		CashYield:    cfg.CashYieldAnnual,
		Baseline: BaselineParams{
			SignalDays: 2,
			HoldDays:   cfg.HoldingWindow,
			TakeProfit: cfg.TakeProfitPct,
			StopLoss:   cfg.StopLossPct,
			Allocation: cfg.AllocationPct,
			Regime:     "All Regimes",
		},
	}
}

func init() {
	NewGLDDeclineStrategy()
}
