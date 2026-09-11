package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// TrendBBOversoldStrategy implements the Trend-Gated Bollinger Oversold strategy.
type TrendBBOversoldStrategy struct {
	marketDBPath string
	calcDBPath   string
}

func init() {
	Register(&TrendBBOversoldStrategy{})
}

func (s *TrendBBOversoldStrategy) ID() string {
	return "trend-bb"
}

func (s *TrendBBOversoldStrategy) Name() string {
	return "Trend-Gated Bollinger Oversold"
}

func (s *TrendBBOversoldStrategy) Description() string {
	return "Buys oversold capitulation dips (Low < Lower BB & RSI5 < 30) strictly when asset is in a macro uptrend (Close > SMA50)."
}

func (s *TrendBBOversoldStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "trend-bb",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          1.20,   // +20% target
		StopLossPct:        0.94,   // -6% stop loss
		HoldingWindow:      10,     // 10-day max holding
		PositionCap:        5,      // Max 5 positions
		AllocationPct:      0.20,   // 20% equity per position
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *TrendBBOversoldStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *TrendBBOversoldStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// Delegate signal generation directly to the canonical SQLite pipeline
	if sqlStrat, exists := Get("trend_bb-sql"); exists {
		return sqlStrat.GenerateSignals(barsBySymbol)
	}
	pipe := NewSQLPipelineStrategy("trend_bb-pipeline", s.Name(), s.Description(), "sql/strategies/trend_bb", s.DefaultConfig())
	pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
	return pipe.GenerateSignals(barsBySymbol)
}

func (s *TrendBBOversoldStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}
