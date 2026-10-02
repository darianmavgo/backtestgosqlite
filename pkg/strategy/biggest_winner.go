package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

type BiggestWinnerStrategy struct {
	marketDBPath string
	calcDBPath   string
}

func init() {
	Register(&BiggestWinnerStrategy{})
}

func (s *BiggestWinnerStrategy) ID() string   { return "biggest-winner" }
func (s *BiggestWinnerStrategy) Name() string { return "Biggest Winner Backtest" }
func (s *BiggestWinnerStrategy) Description() string {
	return "Buys the top performing asset of the previous calendar year and holds it for one year."
}

func (s *BiggestWinnerStrategy) DefaultConfig() StrategyConfig {
	return annualHoldConfig(s.ID(), s.Name(), s.Description())
}

func (s *BiggestWinnerStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *BiggestWinnerStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *BiggestWinnerStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	return annualWinnerSignals(s.ID(), s.Name(), s.Description(), s.DefaultConfig(), "long", s.marketDBPath, s.calcDBPath, barsBySymbol)
}

func annualHoldConfig(id, name, desc string) StrategyConfig {
	return StrategyConfig{
		ID:            id,
		Name:          name,
		Description:   desc,
		AllocationPct: 1.0,
		PositionCap:   1,
		HoldingWindow: 252,   // Safety cap. The year-end exit signal is the real close.
		TargetPct:     999.0, // Never exit via profit target
		StopLossPct:   0.0,   // 0 multiplier disables the stop loss
		SlippagePct:   0.0005,
	}
}

// annualWinnerSignals runs the annual_winner SQL pipeline: it ranks every symbol
// by its calendar-year return (last close against first open) and, on the first
// session of the next year, trades that winner until the last session of the
// year. side is long, short, or inverse (the matched inverse ETF on that same
// session, with the year skipped when the winner has no pair or the pair did not
// trade that day, so the account stays in cash). A symbol whose first open of the
// year is 0 has no return and is not ranked.
func annualWinnerSignals(id, name, desc string, cfg StrategyConfig, side, marketDB, calcDB string, bars map[string][]models.Bar) []models.Signal {
	cfg.SQLParams = map[string]string{"SIDE": side}
	return RunPipeline(id, name, desc, "sql/strategies/annual_winner", cfg, marketDB, calcDB, "market", bars)
}
