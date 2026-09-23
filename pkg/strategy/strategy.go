package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// Strategy is the unified interface implemented by all trading strategies in Go or SQL.
type Strategy interface {
	// ID returns the unique CLI / programmatic identifier (e.g. "bb-capitulation", "rsi2", "mara_tree").
	ID() string

	// Name returns the human-readable display name.
	Name() string

	// Description returns a concise summary of the strategy logic.
	Description() string

	// DefaultConfig returns the recommended baseline portfolio and risk parameters.
	DefaultConfig() StrategyConfig

	// Validate ensures the strategy configuration is logically sound.
	Validate() error

	// SetDatabases injects the required database paths.
	// marketDBPath: Read-only source for historical data.
	// calcDBPath: Isolated database for all strategy calculations and intermediate tables.
	SetDatabases(marketDBPath, calcDBPath string)

	// GenerateSignals evaluates historical bars across all symbols and returns chronological entry signals.
	GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal
}

// RequiredSymbolsProvider is optionally implemented by strategies that require specific assets (e.g. multi-leg or combo strategies).
type RequiredSymbolsProvider interface {
	RequiredSymbols() []string
}

// TotalReturnProvider is optionally implemented by strategies that must be
// simulated on dividend-adjusted prices (buy-and-hold income ETFs). When
// UsesTotalReturn is true the runner scales each required bar's OHLC by
// AdjClose/Close before simulating, so dividends are reinvested at the
// ex-date and show up in the equity curve, and reports the price vs dividend
// split of the return. Trade prices/share counts are then on the adjusted scale.
// With reinvestDividends=false (runner.ExecuteStrategyWithDividends) the bars stay raw and the derived cash
// dividends are paid into the account instead.
type TotalReturnProvider interface {
	UsesTotalReturn() bool
}

// OverlaySpec configures a monthly covered-call overlay on a buy-and-hold underlying.
type OverlaySpec struct {
	Underlying       string
	OTMPct           float64 // target call strike as % above spot at each monthly roll
	WindowYears      int     // simulate only the trailing N years of the underlying's bars (option history is ~2y on Polygon's free tier)
	OptionSlip       float64 // $ per share given up vs the last-trade option price when selling
	OptionCommission float64 // $ per contract sold
}

// OptionOverlayProvider is implemented by strategies that hold an underlying
// and write covered calls against it. They do not emit stock signals: the
// runner simulates them with pkg/options against option history stored by
// `download -source polygon-options`, with dividends withdrawn as paid.
type OptionOverlayProvider interface {
	OverlaySpec() OverlaySpec
}

// MinHistoryProvider is optionally implemented by strategies that know the
// fewest trailing daily bars per symbol they need to detect an entry signal
// on the latest bar (indicator warmup included). Used by livescan to load and
// download only that much history. Strategies that don't implement it are
// given DefaultMinHistoryBars.
type MinHistoryProvider interface {
	MinHistoryBars() int
}

// DefaultMinHistoryBars covers SMA200-style indicators plus slack.
const DefaultMinHistoryBars = 250

// MinHistoryBarsFor returns the bars-per-symbol s needs for a live scan.
func MinHistoryBarsFor(s Strategy) int {
	if p, ok := s.(MinHistoryProvider); ok {
		if n := p.MinHistoryBars(); n > 0 {
			return n
		}
	}
	return DefaultMinHistoryBars
}

// DeclineDaysConfigurable is optionally implemented by strategies whose
// consecutive decline/rally-day window is a tunable field (see
// StrategyConfig.DeclineDays) — gld-decline, sig-voo-buy-tecl,
// voo-tecl-spxu-combo. Lets a caller (e.g. `backtest optimized`) apply a
// gridsearch-discovered decline-day window before running the strategy,
// without needing to know its concrete type.
type DeclineDaysConfigurable interface {
	SetDeclineDays(int)
}

// StrategyConfig encapsulates the operational parameters for an algorithm.
type StrategyConfig struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	Timeframe         string  `json:"timeframe,omitempty"`           // Bar timeframe, defaults to "1d"
	Benchmark         string  `json:"benchmark,omitempty"`           // Benchmark symbol, defaults to "SPY"
	PositionSizing    string  `json:"position_sizing,omitempty"`     // "fixed_pct", "fixed_shares", "fixed_dollar", "kelly"
	TargetPct         float64 `json:"target_pct"`                    // Take-profit target multiplier (e.g. 1.18 for +18%); legacy field
	TakeProfitPct     float64 `json:"take_profit_pct,omitempty"`     // Take-profit as fractional offset (e.g. 0.05 for +5%); preferred over TargetPct
	StopLossPct       float64 `json:"stop_loss_pct"`                 // Protective stop-loss multiplier applied directly to entry price (e.g. 0.93 for -7%, NOT a 0.07-style offset — see pkg/simulator's targetPrice/stopLossPrice fallback)
	UseATRStop        bool    `json:"use_atr_stop,omitempty"`        // Whether to use ATR-based dynamic stop loss
	ATRStopMultiplier float64 `json:"atr_stop_multiplier,omitempty"` // Multiplier for ATR stop (e.g. 2.0)
	UseTrailingStop   bool    `json:"use_trailing_stop,omitempty"`   // Whether to trail stop-loss from highest price
	TrailingStopPct   float64 `json:"trailing_stop_pct,omitempty"`   // Trailing stop distance percentage (e.g. 0.05 for 5%)
	HoldingWindow     int     `json:"holding_window"`                // Max holding days before time-up exit (e.g. 10)
	ExitAtMarketOpen  bool    `json:"exit_at_market_open,omitempty"` // Whether to exit at market open on time-up exit
	// NextDayLimitEntry models how the live pipeline enters: the signal is known
	// after the close, so the order is a DAY limit at the signal price placed the
	// next morning. It fills only if the next bar trades at or below that price
	// (at the open when the open is already lower), and is dropped otherwise.
	// Take-profit and stop are anchored to the limit price, as the live bracket
	// order is. Off = the legacy model of filling at the signal bar's close.
	NextDayLimitEntry  bool    `json:"next_day_limit_entry,omitempty"`
	PositionCap        int     `json:"position_cap"`           // Max concurrent open positions (e.g. 5)
	AllocationPct      float64 `json:"allocation_pct"`         // Portfolio equity allocation per trade (e.g. 0.20 for 20%)
	FixedShares        int     `json:"fixed_shares,omitempty"` // Shares per position if PositionSizing == "fixed_shares"
	FixedDollar        float64 `json:"fixed_dollar,omitempty"` // Capital per position if PositionSizing == "fixed_dollar"
	SlippagePct        float64 `json:"slippage_pct"`           // Estimated slippage per fill (e.g. 0.0005 for 0.05%)
	CommissionPerShare float64 `json:"commission_per_share"`   // Broker/exchange commission per share (e.g. 0.0001)

	// DeclineDays is the number of consecutive down-closes (or, for a short leg,
	// up-closes) required in the signal symbol before a decline/rally-streak
	// strategy enters. Only meaningful for strategies whose entry is defined by
	// a consecutive-day streak (e.g. gld-decline, sig-voo-buy-tecl,
	// voo-tecl-spxu-combo); substituted into their SQL pipeline via the
	// __DECLINE_DAYS__ placeholder by SQLPipelineStrategy. Zero/omitted for
	// every other strategy.
	DeclineDays int `json:"decline_days,omitempty"`

	// ShortTakeProfitPct/ShortStopLossPct/ShortHoldingWindow mirror
	// TakeProfitPct/StopLossPct/HoldingWindow but for a strategy's short leg
	// (e.g. sig-voo-buy-tecl's/voo-tecl-spxu-combo's SPXU short), since a single
	// StrategyConfig can't otherwise represent two different exit rules for
	// one strategy's two legs. Same conventions as their long-leg
	// counterparts: ShortTakeProfitPct is a fractional offset (0.06 for +6%),
	// ShortStopLossPct is a direct multiplier (0.95 for -5%). Substituted via
	// __SHORT_TAKE_PROFIT_MULT__ / __SHORT_STOP_LOSS_MULT__ /
	// __SHORT_HOLD_DAYS__. Zero/omitted for single-leg strategies.
	ShortTakeProfitPct float64 `json:"short_take_profit_pct,omitempty"`
	ShortStopLossPct   float64 `json:"short_stop_loss_pct,omitempty"`
	ShortHoldingWindow int     `json:"short_holding_window,omitempty"`

	// CashYieldAnnual is the annualized T-bill / money-market yield accrued on
	// uninvested cash every trading day (leftover allocation included, not only
	// fully-flat days). Set 0.045 for 4.5% APY. Applied by PortfolioSimulator
	// and SharedAccountSimulator.
	CashYieldAnnual float64 `json:"cash_yield_annual,omitempty"`
}
