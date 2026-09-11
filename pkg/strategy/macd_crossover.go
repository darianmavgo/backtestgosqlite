package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// MACDCrossoverStrategy implements the classic MACD signal-line bullish crossover strategy.
type MACDCrossoverStrategy struct {
	marketDBPath string
	calcDBPath   string
}

func init() {
	Register(&MACDCrossoverStrategy{})
}

func (s *MACDCrossoverStrategy) ID() string {
	return "macd-crossover"
}

func (s *MACDCrossoverStrategy) Name() string {
	return "MACD Signal Line Crossover"
}

func (s *MACDCrossoverStrategy) Description() string {
	return "Enters when the MACD line (12, 26) crosses above the 9-period Signal line below the zero line."
}

func (s *MACDCrossoverStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "macd-crossover",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          1.15,   // +15% target
		StopLossPct:        0.95,   // -5% stop loss
		HoldingWindow:      12,     // 12-day max holding
		PositionCap:        5,      // Max 5 positions
		AllocationPct:      0.20,   // 20% equity per position
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *MACDCrossoverStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *MACDCrossoverStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// Delegate signal generation directly to the canonical SQLite pipeline
	if sqlStrat, exists := Get("macd_crossover-sql"); exists {
		return sqlStrat.GenerateSignals(barsBySymbol)
	}
	pipe := NewSQLPipelineStrategy("macd_crossover-pipeline", s.Name(), s.Description(), "sql/strategies/macd_crossover", s.DefaultConfig())
	pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
	return pipe.GenerateSignals(barsBySymbol)
}

func (s *MACDCrossoverStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}
