package storage

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

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
		CREATE INDEX IF NOT EXISTS idx_%s_sym_date ON %s(symbol, Date);
	`, tableName, tableName, tableName)
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
	if err := EnsureBarTable(db, tableName); err != nil {
		return err
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := fmt.Sprintf(`
		INSERT INTO %s (symbol, Date, timeframe, asset_class, open, high, low, close, "Adj Close", volume)
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
