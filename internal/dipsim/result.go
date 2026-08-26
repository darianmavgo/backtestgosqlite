package dipsim

import "github.com/darianmavgo/backtestgosqlite/internal/models"

// ExecutedTrade records a single completed round-trip trade.
type ExecutedTrade struct {
	TradeNum   int     `json:"trade_num"`
	Direction  string  `json:"direction"` // "LONG" or "SHORT"
	Asset      string  `json:"asset"`
	SignalDate string  `json:"signal_date"`
	EntryDate  string  `json:"entry_date"`
	ExitDate   string  `json:"exit_date"`
	EntryPrice float64 `json:"entry_price"`
	ExitPrice  float64 `json:"exit_price"`
	HoldDays   int     `json:"hold_days"`
	ExitReason string  `json:"exit_reason"`
	ReturnPct  float64 `json:"return_pct"`
	NetPnL     float64 `json:"net_pnl"`
	IsWin      bool    `json:"is_win"`
}

// DipSimResult holds the complete output of a single dip simulation run.
type DipSimResult struct {
	Config       DipSimConfig           `json:"config"`
	EndingCap    float64                `json:"ending_capital"`
	NetProfit    float64                `json:"net_profit"`
	TotalReturn  float64                `json:"total_return_pct"`
	CAGR         float64                `json:"cagr"`
	MTMMaxDD     float64                `json:"max_mtm_drawdown"`
	ClosedMaxDD  float64                `json:"max_closed_drawdown"`
	CalmarRatio  float64                `json:"calmar_ratio"`
	WinRate      float64                `json:"win_rate"`
	TotalTrades  int                    `json:"total_trades"`
	Wins         int                    `json:"wins"`
	Losses       int                    `json:"losses"`
	ProfitFactor float64                `json:"profit_factor"`
	AvgHoldDays  float64                `json:"avg_hold_days"`
	Trades       []ExecutedTrade        `json:"trades"`
	DailySeries  []models.DailySnapshot `json:"daily_series"`
}

// ComboResult holds the output of a dual long/short combo simulation.
type ComboResult struct {
	LongConfig   DipSimConfig           `json:"long_config"`
	ShortConfig  DipSimConfig           `json:"short_config"`
	EndingCap    float64                `json:"ending_capital"`
	NetProfit    float64                `json:"net_profit"`
	TotalReturn  float64                `json:"total_return_pct"`
	CAGR         float64                `json:"cagr"`
	MTMMaxDD     float64                `json:"max_mtm_drawdown"`
	CalmarRatio  float64                `json:"calmar_ratio"`
	WinRate      float64                `json:"win_rate"`
	TotalTrades  int                    `json:"total_trades"`
	LongTrades   int                    `json:"long_trades"`
	ShortTrades  int                    `json:"short_trades"`
	LongWins     int                    `json:"long_wins"`
	ShortWins    int                    `json:"short_wins"`
	ProfitFactor float64                `json:"profit_factor"`
	Trades       []ExecutedTrade        `json:"trades"`
	DailySeries  []models.DailySnapshot `json:"daily_series"`
	VOOSeries    []models.DailySnapshot `json:"voo_series"`
}
