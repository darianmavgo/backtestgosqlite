package storage

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

var validTableRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// ValidateTableName ensures the table name contains only safe SQL identifier characters.
func ValidateTableName(name string) error {
	if name == "" {
		return nil
	}
	if !validTableRegex.MatchString(name) {
		return fmt.Errorf("invalid table name %q: must contain only alphanumeric characters and underscores", name)
	}
	return nil
}

// OpenSQLite opens or creates a SQLite database connection with optimized PRAGMAs.
func OpenSQLite(dbPath string) (*sqlx.DB, error) {
	db, err := sqlx.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db at %s: %w", dbPath, err)
	}

	// Performance optimizations for local analytical backtesting
	_, _ = db.Exec(`
		PRAGMA journal_mode = WAL;
		PRAGMA synchronous = NORMAL;
		PRAGMA temp_store = MEMORY;
		PRAGMA cache_size = -64000;
	`)

	return db, nil
}

// ExecuteSQLFile reads and executes a SQL script file.
func ExecuteSQLFile(db *sqlx.DB, filePath string) error {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("error reading %s: %w", filePath, err)
	}

	script := string(content)
	_, err = db.Exec(script)
	if err != nil {
		return fmt.Errorf("exec error in %s: %w", filepath.Base(filePath), err)
	}
	return nil
}

// FetchSummaryRows queries `wc_summary` for all symbols with trades.
func FetchSummaryRows(db *sqlx.DB, limit int) ([]models.SummaryRow, error) {
	query := `
		SELECT symbol, entries, sum_win3, sum_win5, wins_20_10d, win20_10d_rate, 
		       round(avg_max_gain_10d, 4) as avg_max_gain_10d, 
		       round(max_max_gain_10d, 4) as max_max_gain_10d,
		       round(avg_highgappct, 4) as avg_highgappct
		FROM wc_summary 
		WHERE entries > 0 
		ORDER BY win20_10d_rate DESC, entries DESC
	`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	var rows []models.SummaryRow
	err := db.Select(&rows, query)
	return rows, err
}

// FetchHighConfidenceSymbols queries `backtested_win_20_10d` for live candidates.
func FetchHighConfidenceSymbols(db *sqlx.DB) ([]models.SummaryRow, error) {
	query := `
		SELECT symbol, entries, wins_20_10d, win20_10d_rate, 
		       round(avg_max_gain_10d, 4) as avg_max_gain_10d,
		       round(avg_highgappct, 4) as avg_highgappct
		FROM backtested_win_20_10d
		ORDER BY win20_10d_rate DESC, wins_20_10d DESC;
	`
	var rows []models.SummaryRow
	err := db.Select(&rows, query)
	if err != nil {
		// Table might not exist if pipeline hasn't run yet
		return nil, err
	}
	return rows, nil
}

// FetchDetailedSignals queries `wc_backtest_details` for all entry signals.
func FetchDetailedSignals(db *sqlx.DB) ([]models.Signal, error) {
	query := `
		SELECT idx, symbol, date, open, high, low, close, volume, buylimit, entry
		FROM wc_backtest_details
		WHERE entry = 1
		ORDER BY date ASC, idx ASC;
	`
	var signals []models.Signal
	err := db.Select(&signals, query)
	return signals, err
}

// EnsureBarTable creates the standard bar table schema if it does not exist.
func EnsureBarTable(db *sqlx.DB, tableName string) error {
	if tableName == "" {
		tableName = "backtest_start"
	}
	if err := ValidateTableName(tableName); err != nil {
		return err
	}
	schema := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			idx INTEGER,
			Date DATETIME,
			timeframe TEXT DEFAULT '1d',
			asset_class TEXT DEFAULT 'equity',
			open FLOAT,
			high FLOAT,
			low FLOAT,
			close FLOAT,
			"Adj Close" FLOAT,
			volume BIGINT,
			symbol TEXT
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_%s_unique ON %s(symbol, Date);
		CREATE INDEX IF NOT EXISTS idx_%s_sym_date ON %s(symbol, Date);
	`, tableName, tableName, tableName, tableName, tableName)
	_, err := db.Exec(schema)
	return err
}

// UpsertBars inserts a slice of bars into the database using a transaction.
func UpsertBars(db *sqlx.DB, tableName string, bars []models.Bar) error {
	if len(bars) == 0 {
		return nil
	}
	if tableName == "" {
		tableName = "backtest_start"
	}
	if err := ValidateTableName(tableName); err != nil {
		return err
	}
	if err := EnsureBarTable(db, tableName); err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (symbol, Date, timeframe, asset_class, open, high, low, close, "Adj Close", volume)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, tableName)
	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, b := range bars {
		tf := b.Timeframe
		if tf == "" {
			tf = "1d"
		}
		ac := b.AssetClass
		if ac == "" {
			ac = "equity"
		}
		adj := b.AdjClose
		if adj == 0 {
			adj = b.Close
		}
		_, err := stmt.Exec(b.Symbol, b.Date, tf, ac, b.Open, b.High, b.Low, b.Close, adj, b.Volume)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// FetchBars fetches bars with optional symbol filtering and date range.
func FetchBars(db *sqlx.DB, tableName string, symbols []string, startDate, endDate string) (map[string][]models.Bar, []string, error) {
	if tableName == "" {
		tableName = "backtest_start"
	}
	if err := ValidateTableName(tableName); err != nil {
		return nil, nil, err
	}

	query := fmt.Sprintf(`
		SELECT coalesce(idx, rowid, 0) as idx, symbol, substr(Date, 1, 10) as Date, open, high, low, close, volume
		FROM %s
		WHERE 1=1
	`, tableName)

	var args []interface{}
	if len(symbols) > 0 {
		placeholders := make([]string, len(symbols))
		for i, s := range symbols {
			placeholders[i] = "?"
			args = append(args, s)
		}
		query += fmt.Sprintf(" AND symbol IN (%s)", strings.Join(placeholders, ","))
	}
	if startDate != "" {
		query += " AND substr(Date, 1, 10) >= ?"
		args = append(args, startDate)
	}
	if endDate != "" {
		query += " AND substr(Date, 1, 10) <= ?"
		args = append(args, endDate)
	}

	query += " ORDER BY Date ASC, symbol ASC;"

	var allBars []models.Bar
	err := db.Select(&allBars, query, args...)
	if err != nil {
		return nil, nil, err
	}

	bySymbol := make(map[string][]models.Bar)
	datesSeen := make(map[string]bool)
	var dates []string

	for _, b := range allBars {
		bySymbol[b.Symbol] = append(bySymbol[b.Symbol], b)
		if !datesSeen[b.Date] {
			datesSeen[b.Date] = true
			dates = append(dates, b.Date)
		}
	}

	return bySymbol, dates, nil
}

// FetchBenchmarkBars loads benchmark bars indexed by date (e.g. SPY).
func FetchBenchmarkBars(db *sqlx.DB, tableName, benchmarkSymbol string) (map[string]models.Bar, error) {
	if tableName == "" {
		tableName = "backtest_start"
	}
	if err := ValidateTableName(tableName); err != nil {
		return nil, err
	}
	if benchmarkSymbol == "" {
		benchmarkSymbol = "SPY"
	}

	query := fmt.Sprintf(`
		SELECT coalesce(idx, rowid, 0) as idx, symbol, substr(Date, 1, 10) as Date, open, high, low, close, volume
		FROM %s
		WHERE symbol = ?
		ORDER BY Date ASC;
	`, tableName)

	var bars []models.Bar
	err := db.Select(&bars, query, benchmarkSymbol)
	if err != nil {
		return nil, err
	}

	byDate := make(map[string]models.Bar)
	for _, b := range bars {
		byDate[b.Date] = b
	}
	return byDate, nil
}

// FetchAllBarsChronological loads all historical bars indexed by symbol and date.
func FetchAllBarsChronological(db *sqlx.DB) (map[string][]models.Bar, []string, error) {
	return FetchBars(db, "backtest_start", nil, "", "")
}

// EnsureTradeTable creates the institutional trades table schema.
func EnsureTradeTable(db *sqlx.DB) error {
	schema := `
		CREATE TABLE IF NOT EXISTS trades (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			strategy_id TEXT,
			symbol TEXT,
			order_type TEXT,
			entry_idx INTEGER,
			entry_date TEXT,
			entry_price REAL,
			target_price REAL,
			stop_loss_price REAL,
			exit_date TEXT,
			exit_price REAL,
			exit_reason TEXT,
			shares INTEGER,
			invested_capital REAL,
			gross_pnl REAL,
			net_pnl REAL,
			return_pct REAL,
			hold_days INTEGER,
			commission_paid REAL,
			mae_pct REAL,
			mfe_pct REAL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_trades_strat_sym ON trades(strategy_id, symbol, entry_date);
	`
	_, err := db.Exec(schema)
	return err
}

// SavePendingSignals creates a shared mailbox DB for external execution bots and inserts pending signals.
func SavePendingSignals(dbPath string, signals []models.Signal) error {
	if len(signals) == 0 {
		return nil
	}

	db, err := sqlx.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return err
	}
	defer db.Close()

	schema := `
	CREATE TABLE IF NOT EXISTS signals (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		symbol TEXT NOT NULL,
		date TEXT NOT NULL,
		buy_limit REAL NOT NULL,
		order_type TEXT NOT NULL,
		status TEXT DEFAULT 'PENDING'
	);
	`
	if _, err := db.Exec(schema); err != nil {
		return err
	}

	tx, err := db.Beginx()
	if err != nil {
		return err
	}

	stmt, err := tx.Preparex(`
		INSERT INTO signals (symbol, date, buy_limit, order_type, status)
		VALUES (?, ?, ?, ?, 'PENDING')
	`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, s := range signals {
		oType := s.OrderType
		if oType == "" {
			oType = "limit"
		}
		if _, err := stmt.Exec(s.Symbol, s.Date, s.BuyLimit, oType); err != nil {
			tx.Rollback()
			return err
		}
	}

	return tx.Commit()
}

// SaveTrades persists completed simulation trades to the database.
func SaveTrades(db *sqlx.DB, strategyID string, trades []models.Trade) error {
	if len(trades) == 0 {
		return nil
	}
	if err := EnsureTradeTable(db); err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO trades (
			strategy_id, symbol, order_type, entry_idx, entry_date, entry_price,
			target_price, stop_loss_price, exit_date, exit_price, exit_reason,
			shares, invested_capital, gross_pnl, net_pnl, return_pct, hold_days,
			commission_paid, mae_pct, mfe_pct
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range trades {
		_, err := stmt.Exec(
			strategyID, t.Symbol, t.OrderType, t.EntryIdx, t.EntryDate, t.EntryPrice,
			t.TargetPrice, t.StopLossPrice, t.ExitDate, t.ExitPrice, string(t.ExitReason),
			t.Shares, t.InvestedCapital, t.GrossPnL, t.NetPnL, t.ReturnPct, t.HoldDays,
			t.CommissionPaid, t.MaxAdverseExcursion, t.MaxFavorableExcursion,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// FetchTradeSummaryStats computes core trade performance aggregates natively in SQLite.
func FetchTradeSummaryStats(db *sqlx.DB, strategyID string) (models.PerformanceReport, error) {
	var report models.PerformanceReport
	if err := EnsureTradeTable(db); err != nil {
		return report, err
	}

	query := `
		SELECT 
			COUNT(*) AS total_trades,
			COALESCE(SUM(CASE WHEN net_pnl > 0 THEN 1 ELSE 0 END), 0) AS winning_trades,
			COALESCE(SUM(CASE WHEN net_pnl <= 0 THEN 1 ELSE 0 END), 0) AS losing_trades,
			COALESCE(ROUND(CAST(SUM(CASE WHEN net_pnl > 0 THEN 1 ELSE 0 END) AS REAL) / NULLIF(COUNT(*), 0), 4), 0) AS win_rate,
			COALESCE(ROUND(SUM(net_pnl), 2), 0) AS net_profit,
			COALESCE(ROUND(SUM(commission_paid), 2), 0) AS total_commission_paid,
			COALESCE(ROUND(AVG(hold_days), 1), 0) AS avg_holding_days,
			COALESCE(ROUND(AVG(return_pct), 4), 0) AS avg_trade_return_pct,
			COALESCE(ROUND(AVG(CASE WHEN net_pnl > 0 THEN net_pnl ELSE NULL END), 2), 0) AS avg_win_amount,
			COALESCE(ROUND(AVG(CASE WHEN net_pnl < 0 THEN ABS(net_pnl) ELSE NULL END), 2), 0) AS avg_loss_amount,
			COALESCE(ROUND(SUM(CASE WHEN net_pnl > 0 THEN net_pnl ELSE 0 END) / 
				NULLIF(SUM(CASE WHEN net_pnl < 0 THEN ABS(net_pnl) ELSE 0 END), 0), 4), 0) AS profit_factor,
			COALESCE(ROUND(AVG(mae_pct), 4), 0) AS avg_mae,
			COALESCE(ROUND(AVG(mfe_pct), 4), 0) AS avg_mfe
		FROM trades
		WHERE strategy_id = ?;
	`
	type sqlStats struct {
		TotalTrades         int     `db:"total_trades"`
		WinningTrades       int     `db:"winning_trades"`
		LosingTrades        int     `db:"losing_trades"`
		WinRate             float64 `db:"win_rate"`
		NetProfit           float64 `db:"net_profit"`
		TotalCommissionPaid float64 `db:"total_commission_paid"`
		AvgHoldingDays      float64 `db:"avg_holding_days"`
		AvgTradeReturnPct   float64 `db:"avg_trade_return_pct"`
		AvgWinAmount        float64 `db:"avg_win_amount"`
		AvgLossAmount       float64 `db:"avg_loss_amount"`
		ProfitFactor        float64 `db:"profit_factor"`
		AvgMAE              float64 `db:"avg_mae"`
		AvgMFE              float64 `db:"avg_mfe"`
	}

	var stats sqlStats
	if err := db.Get(&stats, query, strategyID); err != nil {
		return report, err
	}

	report.TotalTrades = stats.TotalTrades
	report.WinningTrades = stats.WinningTrades
	report.LosingTrades = stats.LosingTrades
	report.WinRate = stats.WinRate
	report.NetProfit = stats.NetProfit
	report.TotalCommissionPaid = stats.TotalCommissionPaid
	report.AvgHoldingDays = stats.AvgHoldingDays
	report.AvgTradeReturnPct = stats.AvgTradeReturnPct
	report.AvgWinAmount = stats.AvgWinAmount
	report.AvgLossAmount = stats.AvgLossAmount
	report.ProfitFactor = stats.ProfitFactor
	report.AvgMAE = stats.AvgMAE
	report.AvgMFE = stats.AvgMFE

	if report.AvgLossAmount > 0 {
		report.PayoffRatio = report.AvgWinAmount / report.AvgLossAmount
	}

	return report, nil
}

// FetchDipBars retrieves sorted daily OHLCV bars for a symbol, formatted for dip simulations.
// This eliminates the 19 copies of the same SELECT query across cmd/ files.
func FetchDipBars(db *sqlx.DB, symbol string) ([]models.BarData, error) {
	query := `
		SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume
		FROM backtest_start
		WHERE symbol = ?
		ORDER BY substr(Date, 1, 10) ASC;
	`
	var bars []models.BarData
	err := db.Select(&bars, query, symbol)
	return bars, err
}

// FetchSignalBars retrieves bars with SMA200 and SMA50 window functions pre-computed via SQL.
// This eliminates the 6 copies of the same window-function query across cmd/ files.
func FetchSignalBars(db *sqlx.DB, symbol string) ([]models.SignalBar, error) {
	query := `
		SELECT 
			substr(Date, 1, 10) AS date,
			close,
			AVG(close) OVER (ORDER BY substr(Date, 1, 10) ROWS BETWEEN 199 PRECEDING AND CURRENT ROW) AS sma200,
			AVG(close) OVER (ORDER BY substr(Date, 1, 10) ROWS BETWEEN 49 PRECEDING AND CURRENT ROW) AS sma50
		FROM backtest_start
		WHERE symbol = ?
		ORDER BY substr(Date, 1, 10) ASC;
	`
	var bars []models.SignalBar
	err := db.Select(&bars, query, symbol)
	return bars, err
}
