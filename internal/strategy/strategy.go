package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// Strategy is the unified interface implemented by all trading strategies in Go or SQL.
type Strategy interface {
	// ID returns the unique CLI / programmatic identifier (e.g. "bb-capitulation", "wc", "rsi2", "wc-sql").
	ID() string

	// Name returns the human-readable display name.
	Name() string

	// Description returns a concise summary of the strategy logic.
	Description() string

	// DefaultConfig returns the recommended baseline portfolio and risk parameters.
	DefaultConfig() StrategyConfig

	// GenerateSignals evaluates historical bars across all symbols and returns chronological entry signals.
	GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal
}

// StrategyConfig encapsulates the operational parameters for an algorithm.
type StrategyConfig struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	TargetPct          float64 `json:"target_pct"`          // Take-profit target multiplier (e.g. 1.18 for +18%)
	StopLossPct        float64 `json:"stop_loss_pct"`        // Protective stop-loss multiplier (e.g. 0.93 for -7%)
	HoldingWindow      int     `json:"holding_window"`      // Max holding days before time-up exit (e.g. 10)
	PositionCap        int     `json:"position_cap"`        // Max concurrent open positions (e.g. 5)
	AllocationPct      float64 `json:"allocation_pct"`      // Portfolio equity allocation per trade (e.g. 0.20 for 20%)
	SlippagePct        float64 `json:"slippage_pct"`        // Estimated slippage per fill (e.g. 0.0005 for 0.05%)
	CommissionPerShare float64 `json:"commission_per_share"` // Broker/exchange commission per share (e.g. 0.0001)
}
