package storage

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
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
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}

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

// SymbolDateCoverage contains the earliest date, latest date, and count of bars for a symbol in a table.
type SymbolDateCoverage struct {
	Symbol   string `db:"symbol"`
	MinDate  string `db:"min_date"`
	MaxDate  string `db:"max_date"`
	BarCount int    `db:"bar_count"`
}

// GetSymbolDateCoverage queries the existing date range and bar count for a symbol in tableName.
func GetSymbolDateCoverage(db *sqlx.DB, tableName, symbol string) (SymbolDateCoverage, error) {
	cov := SymbolDateCoverage{Symbol: symbol}
	if tableName == "" {
		tableName = "backtest_start"
	}
	if err := ValidateTableName(tableName); err != nil {
		return cov, err
	}

	query := fmt.Sprintf(`
		SELECT 
			coalesce(MIN(substr(Date, 1, 10)), '') as min_date,
			coalesce(MAX(substr(Date, 1, 10)), '') as max_date,
			COUNT(*) as bar_count
		FROM %s
		WHERE symbol = ?
	`, tableName)

	type row struct {
		MinDate  string `db:"min_date"`
		MaxDate  string `db:"max_date"`
		BarCount int    `db:"bar_count"`
	}
	var r row
	err := db.Get(&r, query, symbol)
	if err != nil {
		return cov, nil
	}
	cov.MinDate = r.MinDate
	cov.MaxDate = r.MaxDate
	cov.BarCount = r.BarCount
	return cov, nil
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
		SELECT coalesce(idx, rowid, 0) as idx, symbol, substr(Date, 1, 10) as Date, open, high, low, close, volume,
		AVG(close) OVER (PARTITION BY symbol ORDER BY substr(Date, 1, 10) ROWS BETWEEN 199 PRECEDING AND CURRENT ROW) AS sma200,
		AVG(close) OVER (PARTITION BY symbol ORDER BY substr(Date, 1, 10) ROWS BETWEEN 49 PRECEDING AND CURRENT ROW)  AS sma50
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

// FetchRecentBars retrieves only the most recent N bars per symbol for fast live scanning,
// while computing accurate SMA200/SMA50 across the historical series.
func FetchRecentBars(db *sqlx.DB, tableName string, symbols []string, limitPerSymbol int) (map[string][]models.Bar, []string, error) {
	if tableName == "" {
		tableName = "backtest_start"
	}
	if err := ValidateTableName(tableName); err != nil {
		return nil, nil, err
	}
	if limitPerSymbol <= 0 {
		limitPerSymbol = 250
	}

	whereClause := "WHERE 1=1"
	var args []interface{}
	if len(symbols) > 0 {
		placeholders := make([]string, len(symbols))
		for i, s := range symbols {
			placeholders[i] = "?"
			args = append(args, s)
		}
		whereClause += fmt.Sprintf(" AND symbol IN (%s)", strings.Join(placeholders, ","))
	}

	query := fmt.Sprintf(`
		SELECT idx, symbol, Date, open, high, low, close, volume, sma200, sma50
		FROM (
			SELECT 
				coalesce(idx, rowid, 0) as idx, 
				symbol, 
				substr(Date, 1, 10) as Date, 
				open, high, low, close, volume,
				AVG(close) OVER (PARTITION BY symbol ORDER BY substr(Date, 1, 10) ROWS BETWEEN 199 PRECEDING AND CURRENT ROW) AS sma200,
				AVG(close) OVER (PARTITION BY symbol ORDER BY substr(Date, 1, 10) ROWS BETWEEN 49 PRECEDING AND CURRENT ROW)  AS sma50,
				ROW_NUMBER() OVER (PARTITION BY symbol ORDER BY substr(Date, 1, 10) DESC) as rn
			FROM %s
			%s
		)
		WHERE rn <= ?
		ORDER BY Date ASC, symbol ASC;
	`, tableName, whereClause)

	args = append(args, limitPerSymbol)

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
func FetchAllBarsChronological(db *sqlx.DB, tableName string) (map[string][]models.Bar, []string, error) {
	if tableName == "" {
		tableName = "backtest_start"
	}
	return FetchBars(db, tableName, nil, "", "")
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

func EnsureSignalTable(db *sqlx.DB) error {
	schema := `
		CREATE TABLE IF NOT EXISTS signals (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			strategy_id TEXT,
			symbol TEXT,
			date TEXT,
			order_type TEXT,
			direction TEXT,
			entry_price REAL,
			take_profit REAL,
			stop_loss REAL,
			regime TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_signals_strat_sym ON signals(strategy_id, symbol, date);
	`
	_, err := db.Exec(schema)
	return err
}

func SaveSignals(db *sqlx.DB, strategyID string, signals []models.Signal) error {
	if len(signals) == 0 {
		return nil
	}
	if err := EnsureSignalTable(db); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO signals (
			strategy_id, symbol, date, order_type, direction, entry_price, take_profit, stop_loss, regime
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range signals {
		dir := s.Direction
		if dir == "" {
			dir = "LONG"
		}
		typ := s.OrderType
		if typ == "" {
			typ = "limit"
		}
		// In models.Signal, entry price is usually stored in BuyLimit or Close. BuyLimit is safer for Limit orders.
		entryPrice := s.BuyLimit
		if entryPrice == 0 {
			entryPrice = s.Close
		}

		_, err := stmt.Exec(
			strategyID, s.Symbol, s.Date, typ, dir, entryPrice, s.TakeProfit, s.StopLoss, s.Regime,
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func EnsureEquityCurveTable(db *sqlx.DB) error {
	schema := `
		CREATE TABLE IF NOT EXISTS equity_curve (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			strategy_id TEXT,
			date TEXT,
			total_equity REAL,
			cash REAL,
			invested REAL,
			drawdown_pct REAL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_equity_strat_date ON equity_curve(strategy_id, date);
	`
	_, err := db.Exec(schema)
	return err
}

func SaveEquityCurve(db *sqlx.DB, strategyID string, curve []models.DailyEquityPoint) error {
	if len(curve) == 0 {
		return nil
	}
	if err := EnsureEquityCurveTable(db); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO equity_curve (
			strategy_id, date, total_equity, cash, invested, drawdown_pct
		) VALUES (?, ?, ?, ?, ?, ?)
	`
	stmt, err := tx.Prepare(query)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range curve {
		_, err := stmt.Exec(strategyID, p.Date, p.TotalEquity, p.Cash, p.PositionsValue, p.DrawdownPct)
		if err != nil {
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

// CreateUniqueDB generates an isolated SQLite file in dir with a filename matching baseName.
// If baseName.db already exists, it tries baseName_2.db, baseName_3.db, and so on.
// The file creation is atomic (using os.O_CREATE|os.O_EXCL) to prevent race conditions during concurrent backtests.
func CreateUniqueDB(dir, baseName string) (string, *sqlx.DB, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	cleanBase := strings.ToLower(strings.TrimSpace(baseName))
	cleanBase = strings.ReplaceAll(cleanBase, " ", "_")
	cleanBase = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, cleanBase)
	if cleanBase == "" {
		cleanBase = "backtest_results"
	}

	var targetPath string
	firstPath := filepath.Join(dir, fmt.Sprintf("%s.db", cleanBase))
	f, err := os.OpenFile(firstPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
	if err == nil {
		f.Close()
		targetPath = firstPath
	} else if os.IsExist(err) {
		for i := 2; ; i++ {
			candidate := filepath.Join(dir, fmt.Sprintf("%s_%d.db", cleanBase, i))
			f, err := os.OpenFile(candidate, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0666)
			if err == nil {
				f.Close()
				targetPath = candidate
				break
			}
			if !os.IsExist(err) {
				return "", nil, fmt.Errorf("failed to create candidate db file %s: %w", candidate, err)
			}
		}
	} else {
		return "", nil, fmt.Errorf("failed to create db file %s: %w", firstPath, err)
	}

	db, err := OpenSQLite(targetPath)
	if err != nil {
		return targetPath, nil, fmt.Errorf("failed to open newly created SQLite DB %s: %w", targetPath, err)
	}

	return targetPath, db, nil
}

// EnsurePerformanceReportTable creates the schema for saving quantitative backtest performance reports.
func EnsurePerformanceReportTable(db *sqlx.DB) error {
	schema := `
		CREATE TABLE IF NOT EXISTS performance_summary (
			strategy_id TEXT PRIMARY KEY,
			start_date TEXT,
			end_date TEXT,
			total_trading_days INTEGER,
			total_calendar_years REAL,
			initial_capital REAL,
			final_equity REAL,
			net_profit REAL,
			total_return_pct REAL,
			cagr REAL,
			sharpe_ratio REAL,
			sortino_ratio REAL,
			calmar_ratio REAL,
			omega_ratio REAL,
			ulcer_index REAL,
			alpha REAL,
			beta REAL,
			benchmark_return_pct REAL,
			max_drawdown_pct REAL,
			max_drawdown_dollars REAL,
			max_drawdown_peak_equity REAL,
			max_drawdown_trough_equity REAL,
			max_drawdown_peak_date TEXT,
			max_drawdown_trough_date TEXT,
			max_drawdown_days INTEGER,
			total_trades INTEGER,
			winning_trades INTEGER,
			losing_trades INTEGER,
			win_rate REAL,
			profit_factor REAL,
			avg_trade_return_pct REAL,
			avg_win_amount REAL,
			avg_loss_amount REAL,
			payoff_ratio REAL,
			avg_holding_days REAL,
			avg_mae REAL,
			avg_mfe REAL,
			total_commission_paid REAL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`
	_, err := db.Exec(schema)
	return err
}

// SavePerformanceReport persists the quantitative performance summary tear sheet to SQLite.
func SavePerformanceReport(db *sqlx.DB, strategyID string, report models.PerformanceReport) error {
	if err := EnsurePerformanceReportTable(db); err != nil {
		return err
	}

	query := `
		INSERT OR REPLACE INTO performance_summary (
			strategy_id, start_date, end_date, total_trading_days, total_calendar_years,
			initial_capital, final_equity, net_profit, total_return_pct, cagr,
			sharpe_ratio, sortino_ratio, calmar_ratio, omega_ratio, ulcer_index,
			alpha, beta, benchmark_return_pct, max_drawdown_pct, max_drawdown_dollars,
			max_drawdown_peak_equity, max_drawdown_trough_equity,
			max_drawdown_peak_date, max_drawdown_trough_date, max_drawdown_days,
			total_trades, winning_trades, losing_trades, win_rate, profit_factor,
			avg_trade_return_pct, avg_win_amount, avg_loss_amount, payoff_ratio,
			avg_holding_days, avg_mae, avg_mfe, total_commission_paid
		) VALUES (
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?,
			?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?
		);
	`
	_, err := db.Exec(query,
		strategyID, report.StartDate, report.EndDate, report.TotalTradingDays, report.TotalCalendarYears,
		report.InitialCapital, report.FinalEquity, report.NetProfit, report.TotalReturnPct, report.CAGR,
		report.SharpeRatio, report.SortinoRatio, report.CalmarRatio, report.OmegaRatio, report.UlcerIndex,
		report.Alpha, report.Beta, report.BenchmarkReturnPct, report.MaxDrawdownPct, report.MaxDrawdownDollars,
		report.MaxDrawdownPeakEquity, report.MaxDrawdownTroughEquity,
		report.MaxDrawdownPeakDate, report.MaxDrawdownTroughDate, report.MaxDrawdownDuration,
		report.TotalTrades, report.WinningTrades, report.LosingTrades, report.WinRate, report.ProfitFactor,
		report.AvgTradeReturnPct, report.AvgWinAmount, report.AvgLossAmount, report.PayoffRatio,
		report.AvgHoldingDays, report.AvgMAE, report.AvgMFE, report.TotalCommissionPaid,
	)
	return err
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
// Deprecated: use FetchBars instead, which returns models.Bar (the unified type).
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
// Deprecated: use FetchBarsWithSMA instead, which returns models.Bar (the unified type).
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


