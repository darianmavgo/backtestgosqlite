package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

type BiggestWinnerStrategy struct{}

func init() {
	Register(&BiggestWinnerStrategy{})
}

func (s *BiggestWinnerStrategy) ID() string          { return "biggest-winner" }
func (s *BiggestWinnerStrategy) Name() string        { return "Biggest Winner Backtest" }
func (s *BiggestWinnerStrategy) Description() string { return "Buys the top performing asset of the previous period." }

func (s *BiggestWinnerStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:            "biggest-winner",
		Name:          s.Name(),
		Description:   s.Description(),
		AllocationPct: 1.0,
		PositionCap:   1,
	}
}

func (s *BiggestWinnerStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *BiggestWinnerStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	// Not used for pure Go strategies without intermediate calculation tables
}

func (s *BiggestWinnerStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var signals []models.Signal
	// TODO: Implement logic to scan previous calendar year/week, find top performer, and issue buy/sell signals.
	return signals
}
