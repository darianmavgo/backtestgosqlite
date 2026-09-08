package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// RSI2TrendStrategy implements the Connors RSI(2) Trend Pullback strategy.
type RSI2TrendStrategy struct{}

func init() {
	Register(&RSI2TrendStrategy{})
}

func (s *RSI2TrendStrategy) ID() string {
	return "rsi2"
}

func (s *RSI2TrendStrategy) Name() string {
	return "Connors RSI(2) Trend Pullback"
}

func (s *RSI2TrendStrategy) Description() string {
	return "Buys deep 2-period RSI oversold (< 10) pullbacks when asset is trading above its 50-day moving average."
}

func (s *RSI2TrendStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "rsi2",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          1.10,   // +10% target
		StopLossPct:        0.94,   // -6% stop loss
		HoldingWindow:      6,      // 6-day max holding
		PositionCap:        5,      // Max 5 positions
		AllocationPct:      0.20,   // 20% equity per position
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *RSI2TrendStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *RSI2TrendStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// Delegate signal generation directly to the canonical SQLite pipeline
	if sqlStrat, exists := Get("rsi2_trend-sql"); exists {
		return sqlStrat.GenerateSignals(barsBySymbol)
	}
	pipe := NewSQLPipelineStrategy("rsi2_trend-pipeline", s.Name(), s.Description(), "sql/strategies/rsi2_trend", "data/market_history.db", s.DefaultConfig())
	return pipe.GenerateSignals(barsBySymbol)
}
