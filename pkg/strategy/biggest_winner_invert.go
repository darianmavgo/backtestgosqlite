package strategy

import "github.com/darianmavgo/backtestgosqlite/pkg/models"

// BiggestWinnerShortStrategy is the short inversion of biggest-winner: the
// same name, the same first session of the year, covered on the last session
// of that year. The position is cash-secured (collateral equals notional) and
// pays ShortBorrowAnnual on market value. It is not the same trade as buying
// the inverse ETF. That leg is BiggestWinnerInverseStrategy.
type BiggestWinnerShortStrategy struct{}

func init() {
	Register(&BiggestWinnerShortStrategy{})
}

func (s *BiggestWinnerShortStrategy) ID() string { return "biggest-winner-short" }
func (s *BiggestWinnerShortStrategy) Name() string {
	return "Biggest Winner Short"
}
func (s *BiggestWinnerShortStrategy) Description() string {
	return "Shorts the prior year's biggest winner and covers on the last session of the new year. " +
		"Cash-secured (collateral = notional) with a 1% annual borrow fee on market value. " +
		"Same ranking and calendar as biggest-winner."
}

func (s *BiggestWinnerShortStrategy) DefaultConfig() StrategyConfig {
	cfg := annualHoldConfig(s.ID(), s.Name(), s.Description())
	cfg.ShortBorrowAnnual = 0.01
	return cfg
}

func (s *BiggestWinnerShortStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *BiggestWinnerShortStrategy) SetDatabases(marketDBPath, calcDBPath string) {}

func (s *BiggestWinnerShortStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	return annualWinnerSignals(barsBySymbol, annualShort)
}

// BiggestWinnerInverseStrategy buys the matched-leverage inverse ETF of the
// prior year's biggest winner (TQQQ → SQQQ, SOXL → SOXS, SPY → SH, ...).
// A year whose winner has no pair in InverseETF, or whose pair did not trade
// on the entry date, is left in cash. Holding a 3x inverse for a year is not
// the negation of holding the 3x bull: both funds pay volatility decay.
type BiggestWinnerInverseStrategy struct{}

func init() {
	Register(&BiggestWinnerInverseStrategy{})
}

func (s *BiggestWinnerInverseStrategy) ID() string { return "biggest-winner-inverse" }
func (s *BiggestWinnerInverseStrategy) Name() string {
	return "Biggest Winner Inverse ETF"
}
func (s *BiggestWinnerInverseStrategy) Description() string {
	return "Buys the matched-leverage inverse ETF of the prior year's biggest winner and sells it on the last session of the new year. " +
		"Skips a year when that winner has no inverse ETF in the bar history, or the inverse did not trade on the entry date."
}

func (s *BiggestWinnerInverseStrategy) DefaultConfig() StrategyConfig {
	return annualHoldConfig(s.ID(), s.Name(), s.Description())
}

func (s *BiggestWinnerInverseStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *BiggestWinnerInverseStrategy) SetDatabases(marketDBPath, calcDBPath string) {}

func (s *BiggestWinnerInverseStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	return annualWinnerSignals(barsBySymbol, annualInverse)
}
