package models

import "time"

// Bar represents a single OHLCV price bar for an asset.
type Bar struct {
	Idx        int       `db:"idx" json:"idx,omitempty"`
	Symbol     string    `db:"symbol" json:"symbol"`
	Date       string    `db:"Date" json:"date"`
	Timeframe  string    `db:"timeframe" json:"timeframe,omitempty"`   // e.g. "1d", "1h", "5m" (defaults to "1d")
	AssetClass string    `db:"asset_class" json:"asset_class,omitempty"` // e.g. "equity"
	Open       float64   `db:"open" json:"open"`
	High       float64   `db:"high" json:"high"`
	Low        float64   `db:"low" json:"low"`
	Close      float64   `db:"close" json:"close"`
	AdjClose   float64   `db:"Adj Close" json:"adj_close,omitempty"`
	Volume     int64     `db:"volume" json:"volume"`
	ParsedDt   time.Time `json:"-"`
}

// Signal represents a trade trigger detected by a strategy.
type Signal struct {
	Idx        int                `db:"idx" json:"idx"`
	Symbol     string             `db:"symbol" json:"symbol"`
	Date       string             `db:"date" json:"date"`
	Open       float64            `db:"open" json:"open"`
	High       float64            `db:"high" json:"high"`
	Low        float64            `db:"low" json:"low"`
	Close      float64            `db:"close" json:"close"`
	Volume     int64              `db:"volume" json:"volume"`
	BuyLimit   float64            `db:"buylimit" json:"buylimit"`
	Entry      int                `db:"entry" json:"entry"`
	OrderType  string             `db:"order_type" json:"order_type,omitempty"` // "limit", "market", "stop_limit" (default: "limit")
	StopLoss   float64            `db:"stop_loss" json:"stop_loss,omitempty"`
	TakeProfit float64            `db:"take_profit" json:"take_profit,omitempty"`
	Metadata   map[string]float64 `json:"metadata,omitempty"`
	AssetClass string             `db:"asset_class" json:"asset_class,omitempty"`
}

// ExitReason represents the trigger that closed a trade.
type ExitReason string

const (
	ExitReasonProfitTarget ExitReason = "PROFIT_TARGET"
	ExitReasonStopLoss     ExitReason = "STOP_LOSS"
	ExitReasonTrailingStop ExitReason = "TRAILING_STOP"
	ExitReasonATRStop      ExitReason = "ATR_STOP"
	ExitReasonTimeUp       ExitReason = "TIME_UP"
	ExitReasonEndBacktest  ExitReason = "END_OF_DATA"
)

// Trade represents an executed trade with full lifecycle metrics.
type Trade struct {
	ID                    int        `json:"id"`
	Symbol                string     `json:"symbol"`
	OrderType             string     `json:"order_type,omitempty"`
	EntryIdx              int        `json:"entry_idx"`
	EntryDate             string     `json:"entry_date"`
	EntryPrice            float64    `json:"entry_price"`
	TargetPrice           float64    `json:"target_price"`
	StopLossPrice         float64    `json:"stop_loss_price"`
	ExitDate              string     `json:"exit_date"`
	ExitPrice             float64    `json:"exit_price"`
	ExitReason            ExitReason `json:"exit_reason"`
	Shares                int        `json:"shares"`
	InvestedCapital       float64    `json:"invested_capital"`
	GrossPnL              float64    `json:"gross_pnl"`
	NetPnL                float64    `json:"net_pnl"`
	ReturnPct             float64    `json:"return_pct"`
	HoldDays              int        `json:"hold_days"`
	CommissionPaid        float64    `json:"commission_paid"`
	MaxAdverseExcursion   float64    `json:"mae_pct"` // Maximum adverse drawdown during holding period (%)
	MaxFavorableExcursion float64    `json:"mfe_pct"` // Maximum favorable unrealized gain during holding period (%)
}

// Position tracks currently held active assets in the portfolio.
type Position struct {
	Symbol            string  `json:"symbol"`
	Shares            int     `json:"shares"`
	OrderType         string  `json:"order_type,omitempty"`
	EntryPrice        float64 `json:"entry_price"`
	EntryDate         string  `json:"entry_date"`
	CurrentPrice      float64 `json:"current_price"`
	TargetPrice       float64 `json:"target_price"`
	StopLossPrice     float64 `json:"stop_loss_price"`
	TrailingStopPrice float64 `json:"trailing_stop_price,omitempty"`
	ATRStopPrice      float64 `json:"atr_stop_price,omitempty"`
	HoldDays          int     `json:"hold_days"`
	UnrealizedPnL     float64 `json:"unrealized_pnl"`
	MinLowSince       float64 `json:"min_low_since"`
	MaxHighSince      float64 `json:"max_high_since"`
}

// Account encapsulates the real-time financial ledger.
type Account struct {
	InitialCash    float64 `json:"initial_cash"`
	Cash           float64 `json:"cash"`
	PortfolioValue float64 `json:"portfolio_value"`
	TotalEquity    float64 `json:"total_equity"`
	RealizedPnL    float64 `json:"realized_pnl"`
	UnrealizedPnL  float64 `json:"unrealized_pnl"`
	TotalReturnPct float64 `json:"total_return_pct"`
	PeakEquity     float64 `json:"peak_equity"`
	CurrentDrawdown float64 `json:"current_drawdown"`
}

// DailyEquityPoint logs daily portfolio valuation for equity curve and drawdown charts.
type DailyEquityPoint struct {
	Date           string  `json:"date"`
	Cash           float64 `json:"cash"`
	PositionsValue float64 `json:"positions_value"`
	TotalEquity    float64 `json:"total_equity"`
	DailyReturn    float64 `json:"daily_return"`
	DrawdownPct    float64 `json:"drawdown_pct"`
	OpenPositions  int     `json:"open_positions"`
}

// SummaryRow represents symbol-level statistical performance from the relational screening stage.
type SummaryRow struct {
	Symbol        string  `db:"symbol" json:"symbol"`
	Entries       int     `db:"entries" json:"entries"`
	SumWin3       int     `db:"sum_win3" json:"sum_win3"`
	SumWin5       int     `db:"sum_win5" json:"sum_win5"`
	Wins2010d     int     `db:"wins_20_10d" json:"wins_20_10d"`
	Win2010dRate  float64 `db:"win20_10d_rate" json:"win20_10d_rate"`
	AvgMaxGain10d float64 `db:"avg_max_gain_10d" json:"avg_max_gain_10d"`
	MaxMaxGain10d float64 `db:"max_max_gain_10d" json:"max_max_gain_10d"`
	AvgHighGapPct float64 `db:"avg_highgappct" json:"avg_highgappct"`
	Category      string  `json:"category,omitempty"`
}

// PerformanceReport aggregates institutional quantitative performance metrics.
type PerformanceReport struct {
	StartDate               string  `json:"start_date"`
	EndDate                 string  `json:"end_date"`
	TotalTradingDays        int     `json:"total_trading_days"`
	TotalCalendarYears      float64 `json:"total_calendar_years"`
	InitialCapital          float64 `json:"initial_capital"`
	FinalEquity             float64 `json:"final_equity"`
	NetProfit               float64 `json:"net_profit"`
	TotalReturnPct          float64 `json:"total_return_pct"`
	CAGR                    float64 `json:"cagr"`
	SharpeRatio             float64 `json:"sharpe_ratio"`
	SortinoRatio            float64 `json:"sortino_ratio"`
	CalmarRatio             float64 `json:"calmar_ratio"`
	OmegaRatio              float64 `json:"omega_ratio"`
	UlcerIndex              float64 `json:"ulcer_index"`
	Alpha                   float64 `json:"alpha"`
	Beta                    float64 `json:"beta"`
	BenchmarkReturnPct      float64 `json:"benchmark_return_pct"`
	MaxDrawdownPct          float64 `json:"max_drawdown_pct"`
	MaxDrawdownDollars      float64 `json:"max_drawdown_dollars"`
	MaxDrawdownPeakEquity   float64 `json:"max_drawdown_peak_equity"`
	MaxDrawdownTroughEquity float64 `json:"max_drawdown_trough_equity"`
	MaxDrawdownPeakDate     string  `json:"max_drawdown_peak_date"`
	MaxDrawdownTroughDate   string  `json:"max_drawdown_trough_date"`
	MaxDrawdownDuration     int     `json:"max_drawdown_days"`
	TotalTrades             int     `json:"total_trades"`
	WinningTrades           int     `json:"winning_trades"`
	LosingTrades            int     `json:"losing_trades"`
	WinRate                 float64 `json:"win_rate"`
	ProfitFactor            float64 `json:"profit_factor"`
	AvgTradeReturnPct       float64 `json:"avg_trade_return_pct"`
	AvgWinAmount            float64 `json:"avg_win_amount"`
	AvgLossAmount           float64 `json:"avg_loss_amount"`
	PayoffRatio             float64 `json:"payoff_ratio"`
	AvgHoldingDays          float64 `json:"avg_holding_days"`
	AvgMAE                  float64 `json:"avg_mae"`
	AvgMFE                  float64 `json:"avg_mfe"`
	TotalCommissionPaid     float64 `json:"total_commission_paid"`
}

// BarData is the lightweight OHLCV bar type used by dip simulations.
// Replaces the 19 ad-hoc "type BarData struct" definitions across cmd/ files.
type BarData struct {
	Date     string  `db:"Date" json:"date"`
	Open     float64 `db:"open" json:"open"`
	High     float64 `db:"high" json:"high"`
	Low      float64 `db:"low" json:"low"`
	Close    float64 `db:"close" json:"close"`
	AdjClose float64 `db:"Adj Close" json:"adj_close"`
	Volume   int64   `db:"volume" json:"volume"`
}

// SignalBar carries a date, close price, and pre-computed moving averages for regime detection.
type SignalBar struct {
	Date   string  `db:"date" json:"date"`
	Close  float64 `db:"close" json:"close"`
	SMA200 float64 `db:"sma200" json:"sma200"`
	SMA50  float64 `db:"sma50" json:"sma50"`
}

// DailySnapshot records a single day's mark-to-market portfolio state for equity curves and drawdown charts.
type DailySnapshot struct {
	Date      string  `json:"date"`
	Equity    float64 `json:"equity"`
	Drawdown  float64 `json:"drawdown"`
	ActivePos string  `json:"active_pos,omitempty"`
}
