// Package dipsim provides the unified dip-buying simulation engine.
//
// It consolidates the 12+ duplicated simulation loops from cmd/ files into a single
// configurable engine with Run(), RunCombo(), and GridSearch() entry points.
package dipsim

// DipSimConfig is a declarative specification for a single dip-buy strategy.
// Each new study requires only changing this config — no new Go code.
type DipSimConfig struct {
	// Label is a human-readable name for this configuration (e.g. "TECL 65% / 8-Day Hold").
	Label string

	// Signal Parameters
	SignalSymbol    string // "VOO" — the asset that generates entry signals
	SignalDays      int    // 3 — number of consecutive days in the streak
	SignalDirection string // "drop" or "rally"
	SignalSQLFile   string // optional path to SQL signal file (e.g. "sql/signals/consecutive_drop.sql")

	// Trade Execution
	TradeSymbol   string  // "TECL", "SPXU", etc. — the asset to buy on signal
	AllocationPct float64 // 0.65 — fraction of available cash to deploy per trade
	TakeProfitPct float64 // 0.05 — +5% profit target (0 = no target)
	StopLossPct   float64 // 0.20 — -20% disaster guard (0 = no stop)
	MaxHoldDays   int     // 8 — time barrier (exit if neither TP nor SL hit)

	// Regime Gate
	RegimeFilter string // "VOO < SMA200", "VOO >= SMA200", "VOO < SMA50", "All Regimes", ""

	// Cash Management
	InitialCapital  float64 // 100000.0
	CashYieldAnnual float64 // 0.045 — 4.5% T-Bill APY on idle cash
}

// ComboConfig defines a dual long/short all-weather strategy.
type ComboConfig struct {
	LongConfig  DipSimConfig // e.g. TECL on 3-day dips
	ShortConfig DipSimConfig // e.g. SPXU on 3-day rallies in bear markets

	// Shared parameters (override individual configs)
	InitialCapital  float64
	CashYieldAnnual float64
}

// GridRanges defines the parameter sweep space for a grid search.
type GridRanges struct {
	Symbols         []string  // e.g. ["SQQQ", "SPXU", "SOXS"]
	SignalDays      []int     // e.g. [2, 3, 4, 5]
	HoldDays        []int     // e.g. [1, 2, 3, 4, 5, 6, 8, 10, 12, 15]
	TakeProfitPcts  []float64 // e.g. [0.0, 0.02, 0.03, 0.05, 0.08, 0.10]
	StopLossPcts    []float64 // e.g. [0.0, 0.03, 0.05, 0.07, 0.10]
	RegimeFilters   []string  // e.g. ["VOO < SMA200", "VOO < SMA50", "All Regimes"]
	AllocationPcts  []float64 // e.g. [0.50, 0.65] — if empty, uses base config
}
