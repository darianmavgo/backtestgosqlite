package simulator

import (
	"math"

	"github.com/darianmavgo/backtestgosqlite/internal/strategy"
)

// PositionSizer calculates share allocation for a new position.
type PositionSizer interface {
	CalculateShares(accountCash, totalEquity, entryPrice float64, cfg strategy.StrategyConfig) int
}

// FixedPctSizer allocates a fixed percentage of total portfolio equity.
type FixedPctSizer struct{}

func (s *FixedPctSizer) CalculateShares(accountCash, totalEquity, entryPrice float64, cfg strategy.StrategyConfig) int {
	if entryPrice <= 0 || totalEquity <= 0 {
		return 0
	}
	allocPct := cfg.AllocationPct
	if allocPct <= 0 || allocPct > 1.0 {
		allocPct = 0.20
	}
	targetAlloc := totalEquity * allocPct
	if targetAlloc > accountCash {
		targetAlloc = accountCash * 0.95 // Buffer for fees/slippage
	}
	if targetAlloc <= 0 {
		return 0
	}
	return int(targetAlloc / entryPrice)
}

// FixedDollarSizer allocates a fixed dollar amount per position.
type FixedDollarSizer struct{}

func (s *FixedDollarSizer) CalculateShares(accountCash, totalEquity, entryPrice float64, cfg strategy.StrategyConfig) int {
	if entryPrice <= 0 || cfg.FixedDollar <= 0 {
		return 0
	}
	targetAlloc := cfg.FixedDollar
	if targetAlloc > accountCash {
		targetAlloc = accountCash * 0.95
	}
	if targetAlloc <= 0 {
		return 0
	}
	return int(targetAlloc / entryPrice)
}

// FixedSharesSizer allocates a constant number of shares.
type FixedSharesSizer struct{}

func (s *FixedSharesSizer) CalculateShares(accountCash, totalEquity, entryPrice float64, cfg strategy.StrategyConfig) int {
	if entryPrice <= 0 || cfg.FixedShares <= 0 {
		return 0
	}
	shares := cfg.FixedShares
	cost := float64(shares) * entryPrice
	if cost > accountCash {
		shares = int((accountCash * 0.95) / entryPrice)
	}
	return int(math.Max(0, float64(shares)))
}

// KellySizer implements half-Kelly criterion based on win rate and payoff ratio.
type KellySizer struct {
	WinRate     float64
	PayoffRatio float64
}

func (s *KellySizer) CalculateShares(accountCash, totalEquity, entryPrice float64, cfg strategy.StrategyConfig) int {
	if entryPrice <= 0 || totalEquity <= 0 {
		return 0
	}
	winRate := s.WinRate
	payoff := s.PayoffRatio
	if winRate <= 0 {
		winRate = 0.50
	}
	if payoff <= 0 {
		payoff = 1.50
	}

	// Full Kelly fraction f* = (p * (b + 1) - 1) / b
	kelly := (winRate*(payoff+1.0) - 1.0) / payoff
	// Use half-Kelly for conservatism, bounded between 5% and 25%
	fraction := math.Max(0.05, math.Min(0.25, kelly*0.5))

	targetAlloc := totalEquity * fraction
	if targetAlloc > accountCash {
		targetAlloc = accountCash * 0.95
	}
	return int(targetAlloc / entryPrice)
}

// GetSizer returns the appropriate PositionSizer based on configuration.
func GetSizer(cfg strategy.StrategyConfig) PositionSizer {
	switch cfg.PositionSizing {
	case "fixed_dollar":
		return &FixedDollarSizer{}
	case "fixed_shares":
		return &FixedSharesSizer{}
	case "kelly":
		return &KellySizer{}
	default:
		return &FixedPctSizer{}
	}
}
