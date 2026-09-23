package strategy

import (
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// PDDTreeStrategy applies the "Precision 200-SMA Re-test Bounce" decision tree
// (originally discovered for MARA, see mara_tree.go) to PDD. It was surfaced by
// cmd/ticker_scan as the best-generalizing, highest-trade-count ticker beyond MARA:
// nearly as many trades (98 vs 104) with a shallower max drawdown (~8% vs ~9-11%).
//
// Entry Logic (identical decision tree to mara_tree, tuned per-symbol):
//   - Volatility Compression Coil: Day's range <= 0.45 * 14-day ATR
//   - Precision 200-SMA Bounce: Price is re-testing the 200-day SMA (-0.68% to +3.38% vs SMA200)
//
// Exit Logic: +5% Take-Profit, -6% Stop-Loss, 3-day holding window.
type PDDTreeStrategy struct {
	marketDBPath string
	calcDBPath   string
}

func NewPDDTreeStrategy() *PDDTreeStrategy {
	s := &PDDTreeStrategy{}
	Register(s)
	return s
}

func (s *PDDTreeStrategy) ID() string { return "pdd_tree" }

func (s *PDDTreeStrategy) Name() string { return "PDD Decision Tree (Precision 200-SMA Bounce)" }

func (s *PDDTreeStrategy) Description() string {
	return "Long PDD on the same CloudForest Decision Tree as mara_tree: Volatility Coil (Range <= 0.45 ATR14) " +
		"or 200-SMA Bounce Re-test (-0.68% to +3.38%). +5% Take-Profit, -6% Stop-Loss, 3-day hold."
}

func (s *PDDTreeStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *PDDTreeStrategy) RequiredSymbols() []string {
	return []string{"PDD"}
}

func (s *PDDTreeStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "PDD",
		Timeframe:          "1d",
		PositionSizing:     "fixed_pct",
		AllocationPct:      0.65,
		TargetPct:          1.05,
		TakeProfitPct:      0.05,
		StopLossPct:        0.94, // -6% stop-loss floor multiplier (entry * 0.94)
		HoldingWindow:      3,
		PositionCap:        1,
		CashYieldAnnual:    0.045,
		SlippagePct:        0.0005,
		CommissionPerShare: 0.0001,
	}
}

func (s *PDDTreeStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *PDDTreeStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// 1. Prefer the SQL pipeline (sql/strategies/pdd_tree): see mara_tree.go's
	// GenerateSignals for why the SMA200/ATR14 rolling-window math belongs in SQL.
	if s.calcDBPath != "" && s.marketDBPath != "" {
		dir := "sql/strategies/pdd_tree"
		if sqlStrat, exists := Get("pdd_tree-sql"); exists {
			if sp, ok := sqlStrat.(*SQLPipelineStrategy); ok {
				dir = sp.PipelineDir()
			}
		}
		pipe := NewSQLPipelineStrategy("pdd_tree-pipeline", s.Name(), s.Description(), dir, s.DefaultConfig())
		pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
		return pipe.GenerateSignals(barsBySymbol)
	}

	// 2. Pure Go calculation fallback for in-memory backtesting and unit testing.
	var bars []models.Bar
	var ok bool
	for sym, b := range barsBySymbol {
		if strings.EqualFold(sym, "PDD") {
			bars = b
			ok = true
			break
		}
	}
	if !ok {
		return nil
	}
	return TreeBounceSignals("PDD", bars, 0.05, 0.06, 3)
}

func (s *PDDTreeStrategy) ParameterSpace() ParameterSpace {
	cfg := s.DefaultConfig()
	return ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      []string{"PDD"},
		SignalSymbol: "PDD",
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
			HoldDays:   cfg.HoldingWindow,
			TakeProfit: cfg.TakeProfitPct,
			StopLoss:   0.06,
			Allocation: cfg.AllocationPct,
			Regime:     "All Regimes",
		},
	}
}

func init() {
	NewPDDTreeStrategy()
}
