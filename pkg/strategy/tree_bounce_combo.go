package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// TreeBounceCombo runs the mara_tree / pdd_tree / nvdl_tree decision trees concurrently
// against MARA, PDD, and NVDL, allowing up to 3 simultaneous positions (one per symbol).
// The goal is diversification: each leg fires on its own independent volatility-coil /
// SMA200-bounce signal, so the combo can be in more than one of them at once, which
// should smooth the equity curve and shorten drawdown duration relative to any single
// ticker — at the cost of needing enough idle cash for multiple concurrent legs.
//
// Per-leg TP/SL/hold are baked in per-signal (see mara_tree.go, pdd_tree.go, nvdl_tree.go)
// via TreeBounceSignals; only allocation/position-cap/cash-yield are combo-level settings.
type TreeBounceCombo struct {
	marketDBPath string
	calcDBPath   string
}

func NewTreeBounceCombo() *TreeBounceCombo {
	s := &TreeBounceCombo{}
	Register(s)
	RegisterAlias("mara_pdd_nvdl_combo", s)
	return s
}

func (s *TreeBounceCombo) ID() string { return "tree_bounce_combo" }

func (s *TreeBounceCombo) Name() string { return "MARA+PDD+NVDL Tree-Bounce Combo" }

func (s *TreeBounceCombo) Description() string {
	return "Runs the Precision 200-SMA Bounce decision tree concurrently on MARA (+5%TP/-8%SL/1d), " +
		"PDD (+5%TP/-6%SL/3d), and NVDL (+15%TP/-7%SL/5d), allowing up to 3 simultaneous positions " +
		"(30% allocation per leg) to diversify away single-ticker drawdown duration."
}

func (s *TreeBounceCombo) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *TreeBounceCombo) RequiredSymbols() []string {
	return []string{"MARA", "PDD", "NVDL"}
}

func (s *TreeBounceCombo) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "MARA",
		Timeframe:          "1d",
		PositionSizing:     "fixed_pct",
		AllocationPct:      0.30, // 30% per leg; up to 3 concurrent legs
		HoldingWindow:      5,    // fallback only; each signal carries its own HoldDaysOverride
		PositionCap:        3,    // one position per symbol, all three can be open at once
		CashYieldAnnual:    0.045,
		SlippagePct:        0.0005,
		CommissionPerShare: 0.0001,
	}
}

func (s *TreeBounceCombo) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *TreeBounceCombo) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var signals []models.Signal

	type leg struct {
		symbol     string
		pipelineID string // matching sql/strategies/<pipelineID> and <pipelineID>-sql
		tp, sl     float64
		hold       int
	}
	legs := []leg{
		{"MARA", "mara_tree", 0.05, 0.08, 1},
		{"PDD", "pdd_tree", 0.05, 0.06, 3},
		{"NVDL", "nvdl_tree", 0.15, 0.07, 5},
	}

	for _, l := range legs {
		// 1. Prefer each leg's own SQL pipeline (see mara_tree.go's
		// GenerateSignals): identical rolling-window math, just reused per-symbol.
		// The SQL pipeline reads bars from the attached market DB directly, so
		// it doesn't need this leg's symbol present in barsBySymbol.
		if s.calcDBPath != "" && s.marketDBPath != "" {
			dir := "sql/strategies/" + l.pipelineID
			if sqlStrat, exists := Get(l.pipelineID + "-sql"); exists {
				if sp, ok := sqlStrat.(*SQLPipelineStrategy); ok {
					dir = sp.PipelineDir()
				}
			}
			legCfg := StrategyConfig{TakeProfitPct: l.tp, StopLossPct: 1.0 - l.sl, HoldingWindow: l.hold}
			pipe := NewSQLPipelineStrategy(l.pipelineID+"-combo-pipeline", s.Name(), s.Description(), dir, legCfg)
			pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
			signals = append(signals, pipe.GenerateSignals(barsBySymbol)...)
			continue
		}

		// 2. Pure Go calculation fallback for in-memory backtesting and unit testing.
		if bars, ok := barsBySymbol[l.symbol]; ok {
			signals = append(signals, TreeBounceSignals(l.symbol, bars, l.tp, l.sl, l.hold)...)
		}
	}

	return signals
}

func init() {
	NewTreeBounceCombo()
}
