package strategy

import (
	"fmt"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// WhitingsCreekStrategy implements the classical Whitings Creek reversal strategy.
type WhitingsCreekStrategy struct{}

func init() {
	Register(&WhitingsCreekStrategy{})
}

func (s *WhitingsCreekStrategy) ID() string {
	return "wc"
}

func (s *WhitingsCreekStrategy) Name() string {
	return "Whitings Creek Baseline"
}

func (s *WhitingsCreekStrategy) Description() string {
	return "Enters when price closes 10% below trailing 10-day low (Day -3 to -12) with a +20% target and -7% stop."
}

func (s *WhitingsCreekStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "wc",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          1.20,   // +20% target
		StopLossPct:        0.93,   // -7% stop loss
		HoldingWindow:      10,     // 10-day max holding
		PositionCap:        5,      // Max 5 positions
		AllocationPct:      0.20,   // 20% equity per position
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *WhitingsCreekStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *WhitingsCreekStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// Delegate signal generation directly to the canonical SQLite pipeline
	if sqlStrat, exists := Get("whitings_creek-sql"); exists {
		return sqlStrat.GenerateSignals(barsBySymbol)
	}
	pipe := NewSQLPipelineStrategy("wc-pipeline", s.Name(), s.Description(), "sql/strategies/whitings_creek", "data/wc_master_backtest.db", s.DefaultConfig())
	return pipe.GenerateSignals(barsBySymbol)
}

// GenerateSQLPipelineQueries provides SQL generation helpers for SQL-based execution of Whitings Creek.
func (s *WhitingsCreekStrategy) GenerateSQLPipelineQueries(cliffDropRatio, profitTargetPct float64) []string {
	return []string{
		fmt.Sprintf("UPDATE wc_buy_signal_slice SET entry = 1 WHERE close < %.2f * minlow;", cliffDropRatio),
		fmt.Sprintf("UPDATE win20_10d_slice SET win20_10d = 1 WHERE max_high10 >= %.2f * buylimit;", profitTargetPct),
	}
}
