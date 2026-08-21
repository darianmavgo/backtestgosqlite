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

	// Validate ensures the strategy configuration is logically sound.
	Validate() error

	// GenerateSignals evaluates historical bars across all symbols and returns chronological entry signals.
	GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal
}

// StrategyConfig encapsulates the operational parameters for an algorithm.
type StrategyConfig struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	Timeframe          string  `json:"timeframe,omitempty"`           // Bar timeframe, defaults to "1d"
	Benchmark          string  `json:"benchmark,omitempty"`           // Benchmark symbol, defaults to "SPY"
	PositionSizing     string  `json:"position_sizing,omitempty"`     // "fixed_pct", "fixed_shares", "fixed_dollar", "kelly"
	TargetPct          float64 `json:"target_pct"`                    // Take-profit target multiplier (e.g. 1.18 for +18%)
	StopLossPct        float64 `json:"stop_loss_pct"`                 // Protective stop-loss multiplier (e.g. 0.93 for -7%)
	UseATRStop         bool    `json:"use_atr_stop,omitempty"`        // Whether to use ATR-based dynamic stop loss
	ATRStopMultiplier  float64 `json:"atr_stop_multiplier,omitempty"` // Multiplier for ATR stop (e.g. 2.0)
	UseTrailingStop    bool    `json:"use_trailing_stop,omitempty"`   // Whether to trail stop-loss from highest price
	TrailingStopPct    float64 `json:"trailing_stop_pct,omitempty"`   // Trailing stop distance percentage (e.g. 0.05 for 5%)
	HoldingWindow      int     `json:"holding_window"`                // Max holding days before time-up exit (e.g. 10)
	PositionCap        int     `json:"position_cap"`                  // Max concurrent open positions (e.g. 5)
	AllocationPct      float64 `json:"allocation_pct"`                // Portfolio equity allocation per trade (e.g. 0.20 for 20%)
	FixedShares        int     `json:"fixed_shares,omitempty"`        // Shares per position if PositionSizing == "fixed_shares"
	FixedDollar        float64 `json:"fixed_dollar,omitempty"`        // Capital per position if PositionSizing == "fixed_dollar"
	SlippagePct        float64 `json:"slippage_pct"`                  // Estimated slippage per fill (e.g. 0.0005 for 0.05%)
	CommissionPerShare float64 `json:"commission_per_share"`          // Broker/exchange commission per share (e.g. 0.0001)
}
