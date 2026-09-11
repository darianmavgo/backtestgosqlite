package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// DonchianBreakoutStrategy implements the classic 20-day Donchian Channel breakout strategy (Turtle Trading style).
type DonchianBreakoutStrategy struct {
	marketDBPath string
	calcDBPath   string
}

func init() {
	Register(&DonchianBreakoutStrategy{})
}

func (s *DonchianBreakoutStrategy) ID() string {
	return "donchian-breakout"
}

func (s *DonchianBreakoutStrategy) Name() string {
	return "Donchian 20-Day Momentum Breakout"
}

func (s *DonchianBreakoutStrategy) Description() string {
	return "Trend-following breakout: buys when price exceeds the 20-day high with trailing stop loss protection."
}

func (s *DonchianBreakoutStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "donchian-breakout",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          1.25,   // +25% target
		StopLossPct:        0.92,   // -8% initial stop loss
		UseTrailingStop:    true,   // Trail stops from peaks
		TrailingStopPct:    0.06,   // 6% trailing stop
		HoldingWindow:      20,     // 20-day holding horizon
		PositionCap:        5,      // Max 5 positions
		AllocationPct:      0.20,   // 20% equity per position
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *DonchianBreakoutStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *DonchianBreakoutStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// Delegate signal generation directly to the canonical SQLite pipeline
	if sqlStrat, exists := Get("donchian_breakout-sql"); exists {
		return sqlStrat.GenerateSignals(barsBySymbol)
	}
	pipe := NewSQLPipelineStrategy("donchian_breakout-pipeline", s.Name(), s.Description(), "sql/strategies/donchian_breakout", s.DefaultConfig())
	pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
	return pipe.GenerateSignals(barsBySymbol)
}

func (s *DonchianBreakoutStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}
