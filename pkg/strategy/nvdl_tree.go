package strategy

import (
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// NVDLTreeStrategy applies the "Precision 200-SMA Re-test Bounce" decision tree
// (originally discovered for MARA, see mara_tree.go) to NVDL (GraniteShares 2x Long
// NVDA Daily). cmd/ticker_scan found this to be the single highest-resilience-score
// candidate across the full ticker universe once TP/SL/hold were tuned: 38.66% CAGR
// with only a 58-day max drawdown duration (vs MARA's 188 days), on 36 trades.
//
// Entry Logic (identical decision tree to mara_tree, tuned per-symbol):
//   - Volatility Compression Coil: Day's range <= 0.45 * 14-day ATR
//   - Precision 200-SMA Bounce: Price is re-testing the 200-day SMA (-0.68% to +3.38% vs SMA200)
//
// Exit Logic: +15% Take-Profit, -7% Stop-Loss, 5-day holding window.
type NVDLTreeStrategy struct {
	marketDBPath string
	calcDBPath   string
}

func NewNVDLTreeStrategy() *NVDLTreeStrategy {
	s := &NVDLTreeStrategy{}
	Register(s)
	return s
}

func (s *NVDLTreeStrategy) ID() string { return "nvdl_tree" }

func (s *NVDLTreeStrategy) Name() string { return "NVDL Decision Tree (Precision 200-SMA Bounce)" }

func (s *NVDLTreeStrategy) Description() string {
	return "Long NVDL on the same CloudForest Decision Tree as mara_tree: Volatility Coil (Range <= 0.45 ATR14) " +
		"or 200-SMA Bounce Re-test (-0.68% to +3.38%). +15% Take-Profit, -7% Stop-Loss, 5-day hold."
}

func (s *NVDLTreeStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *NVDLTreeStrategy) RequiredSymbols() []string {
	return []string{"NVDL"}
}

func (s *NVDLTreeStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "NVDL",
		Timeframe:          "1d",
		PositionSizing:     "fixed_pct",
		AllocationPct:      0.65,
		TargetPct:          1.15,
		TakeProfitPct:      0.15,
		StopLossPct:        0.93, // -7% stop-loss floor multiplier (entry * 0.93)
		HoldingWindow:      5,
		PositionCap:        1,
		CashYieldAnnual:    0.045,
		SlippagePct:        0.0005,
		CommissionPerShare: 0.0001,
	}
}

func (s *NVDLTreeStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *NVDLTreeStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// 1. Prefer the SQL pipeline (sql/strategies/nvdl_tree): see mara_tree.go's
	// GenerateSignals for why the SMA200/ATR14 rolling-window math belongs in SQL.
	if s.calcDBPath != "" && s.marketDBPath != "" {
		dir := "sql/strategies/nvdl_tree"
		if sqlStrat, exists := Get("nvdl_tree-sql"); exists {
			if sp, ok := sqlStrat.(*SQLPipelineStrategy); ok {
				dir = sp.PipelineDir()
			}
		}
		pipe := NewSQLPipelineStrategy("nvdl_tree-pipeline", s.Name(), s.Description(), dir, s.DefaultConfig())
		pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
		return pipe.GenerateSignals(barsBySymbol)
	}

	// 2. Pure Go calculation fallback for in-memory backtesting and unit testing.
	var bars []models.Bar
	var ok bool
	for sym, b := range barsBySymbol {
		if strings.EqualFold(sym, "NVDL") {
			bars = b
			ok = true
			break
		}
	}
	if !ok {
		return nil
	}
	return TreeBounceSignals("NVDL", bars, 0.15, 0.07, 5)
}

func (s *NVDLTreeStrategy) ParameterSpace() ParameterSpace {
	cfg := s.DefaultConfig()
	return ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      []string{"NVDL"},
		SignalSymbol: "NVDL",
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
			StopLoss:   0.07,
			Allocation: cfg.AllocationPct,
			Regime:     "All Regimes",
		},
	}
}

func init() {
	NewNVDLTreeStrategy()
}
