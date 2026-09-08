package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// BBCapitulationStrategy implements the Bollinger Band Capitulation + Reversal Bounce strategy.
type BBCapitulationStrategy struct{}

func init() {
	Register(&BBCapitulationStrategy{})
}

func (s *BBCapitulationStrategy) ID() string {
	return "bb-capitulation"
}

func (s *BBCapitulationStrategy) Name() string {
	return "BB-Capitulation + Reversal Bounce"
}

func (s *BBCapitulationStrategy) Description() string {
	return "Enters on positive reversal confirmation candle after piercing Lower Bollinger Band (20, 2.0) with RSI(5) < 30."
}

func (s *BBCapitulationStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "bb-capitulation",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          1.18,   // +18% take-profit target
		StopLossPct:        0.93,   // -7% protective stop loss
		HoldingWindow:      10,     // 10-day max holding
		PositionCap:        5,      // Max 5 positions
		AllocationPct:      0.20,   // 20% equity per position
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *BBCapitulationStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *BBCapitulationStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// Delegate signal generation directly to the canonical SQLite pipeline
	if sqlStrat, exists := Get("bb_capitulation-sql"); exists {
		return sqlStrat.GenerateSignals(barsBySymbol)
	}
	pipe := NewSQLPipelineStrategy("bb_capitulation-pipeline", s.Name(), s.Description(), "sql/strategies/bb_capitulation", "data/market_history.db", s.DefaultConfig())
	return pipe.GenerateSignals(barsBySymbol)
}
