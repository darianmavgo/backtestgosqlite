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
	// DeclineDays is the number of consecutive GLD down-closes required to
	// enter (default: 2). TakeProfitPct/StopLossPct/HoldingWindow are the
	// strategy's exit rules (defaults +8% / -2% / 12d). All four are the
	// single source of truth for their respective values — flowing into
	// DefaultConfig(), which SQLPipelineStrategy substitutes into the SQL
	// pipeline, and into the pure-Go fallback below — instead of being
	// separately hardcoded literals that DefaultConfig() had no actual
	// effect on. TakeProfitPct is a fractional offset (0.08 = +8%);
	// StopLossPct is a direct multiplier (0.98 = -2%), matching every other
	// strategy's StopLossPct convention.
	DeclineDays   int
	TakeProfitPct float64
	StopLossPct   float64
	HoldingWindow int
	marketDBPath  string
	calcDBPath    string
}

// NewGLDDeclineStrategy constructs and auto-registers the strategy.
func NewGLDDeclineStrategy() *GLDDeclineStrategy {
	s := &GLDDeclineStrategy{DeclineDays: 2, TakeProfitPct: 0.08, StopLossPct: 0.98, HoldingWindow: 12}
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
	declineDays := s.DeclineDays
	if declineDays <= 0 {
		declineDays = 2
	}
	takeProfitPct := s.TakeProfitPct
	if takeProfitPct <= 0 {
		takeProfitPct = 0.08
	}
	stopLossPct := s.StopLossPct
	if stopLossPct <= 0 {
		stopLossPct = 0.98
	}
	holdingWindow := s.HoldingWindow
	if holdingWindow <= 0 {
		holdingWindow = 12
	}
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "GLD",
		AllocationPct:      0.65,
		TargetPct:          1.0 + takeProfitPct, // legacy-multiplier mirror of TakeProfitPct
		TakeProfitPct:      takeProfitPct,
		StopLossPct:        stopLossPct,
		HoldingWindow:      holdingWindow,
		PositionCap:        1,     // One position at a time
		CashYieldAnnual:    0.045, // 4.5% idle cash APY
		SlippagePct:        0.0,
		CommissionPerShare: 0.0,
		DeclineDays:        declineDays,
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
	// 1. Run the canonical SQLite pipeline with this instance's own config, so
	// a non-default s.DeclineDays actually takes effect. Built fresh (not the
	// registered "gld_decline-sql" singleton, which always uses its own
	// AutoRegisterSQLStrategies default) but reusing that singleton's already
	// -resolved pipeline dir when it's registered, since the literal
	// "sql/strategies/gld_decline" only resolves correctly when the process's
	// working directory is the repo root (true for the compiled binaries, not
	// for `go test` run from this package's directory).
	if s.calcDBPath != "" && s.marketDBPath != "" {
		dir := "sql/strategies/gld_decline"
		if sqlStrat, exists := Get("gld_decline-sql"); exists {
			if sp, ok := sqlStrat.(*SQLPipelineStrategy); ok {
				dir = sp.PipelineDir()
			}
		}
		pipe := NewSQLPipelineStrategy("gld_decline-pipeline", s.Name(), s.Description(), dir, s.DefaultConfig())
		pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
		return pipe.GenerateSignals(barsBySymbol)
	}

	// 2. Pure Go calculation fallback for in-memory backtesting and unit testing
	bars, ok := barsBySymbol["GLD"]
	if !ok || len(bars) < 3 {
		return nil
	}

	cfg := s.DefaultConfig()
	declineDays := cfg.DeclineDays

	var signals []models.Signal
	streak := 0

	for i := 1; i < len(bars); i++ {
		if bars[i].Close < bars[i-1].Close {
			streak++
		} else {
			streak = 0
		}

		if streak >= declineDays {
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
				TakeProfit:       closePrice * (1.0 + cfg.TakeProfitPct),
				StopLoss:         closePrice * cfg.StopLossPct,
				HoldDaysOverride: cfg.HoldingWindow,
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
			SignalDays: cfg.DeclineDays,
			HoldDays:   cfg.HoldingWindow,
			TakeProfit: cfg.TakeProfitPct,
			StopLoss:   stopLossOffset(cfg.StopLossPct), // gridsearch's grid is offsets; cfg.StopLossPct is a multiplier
			Allocation: cfg.AllocationPct,
			Regime:     "All Regimes",
		},
	}
}

// stopLossOffset converts a StrategyConfig.StopLossPct multiplier (e.g. 0.98
// for -2%) back into the fractional offset (0.02) that cmd/gridsearch's
// generic parameter grid and buildSignals() expect.
func stopLossOffset(multiplier float64) float64 {
	if multiplier <= 0 || multiplier >= 1.0 {
		return 0.0
	}
	return 1.0 - multiplier
}

func init() {
	NewGLDDeclineStrategy()
}
