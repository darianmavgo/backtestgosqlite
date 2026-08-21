package storage

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/darianmavgo/backtestgosqlite/internal/models"
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

// FetchAllBarsChronological loads all historical bars indexed by symbol and date.
func FetchAllBarsChronological(db *sqlx.DB) (map[string][]models.Bar, []string, error) {
	query := `
		SELECT idx, symbol, substr(Date, 1, 10) as Date, open, high, low, close, volume
		FROM backtest_start
		ORDER BY Date ASC, symbol ASC;
	`
	var allBars []models.Bar
	err := db.Select(&allBars, query)
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
