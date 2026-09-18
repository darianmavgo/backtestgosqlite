// Package refdb is the reference database (data/settings.db): ticker
// universes and per-ETF decision-tree configs live here instead of in
// text/CSV files.
package refdb

import (
	"fmt"
	"os"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/jmoiron/sqlx"
)

// DefaultPath is the reference DB location relative to the repo root.
const DefaultPath = "data/settings.db"

// Universe list names in the etf_universe table.
const (
	ListAll   = "all"   // every active US-listed ETF (cmd/etf_universe)
	List6Yr   = "6yr"   // ETFs with 6+ years of history
	ListSweep = "sweep" // liquid ETFs with bars back to 2021: the VOO-signal sweep set
)

const schema = `
CREATE TABLE IF NOT EXISTS etf_universe (
	list   TEXT NOT NULL,
	symbol TEXT NOT NULL,
	PRIMARY KEY (list, symbol)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS etf_dt_strategies (
	symbol      TEXT PRIMARY KEY,
	tp          REAL,
	sl          REAL,
	hold        INTEGER,
	cagr        REAL,
	max_dd      REAL,
	max_dd_days INTEGER,
	trades      INTEGER,
	win_rate    REAL,
	score       REAL
);
`

// Open opens (creating if needed) the reference DB and ensures its schema.
func Open(path string) (*sqlx.DB, error) {
	db, err := storage.OpenSQLite(path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("refdb schema: %w", err)
	}
	return db, nil
}

// OpenExisting opens the reference DB only if the file already exists and is
// non-empty, without creating anything. Returns nil, nil otherwise.
func OpenExisting(path string) (*sqlx.DB, error) {
	if fi, err := os.Stat(path); err != nil || fi.Size() == 0 {
		return nil, nil
	}
	return storage.OpenSQLite(path)
}

// Universe returns the symbols in a list, sorted.
func Universe(db *sqlx.DB, list string) ([]string, error) {
	var syms []string
	err := db.Select(&syms, `SELECT symbol FROM etf_universe WHERE list = ? ORDER BY symbol`, list)
	return syms, err
}

// SaveUniverse replaces a list's contents.
func SaveUniverse(db *sqlx.DB, list string, symbols []string) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM etf_universe WHERE list = ?`, list); err != nil {
		tx.Rollback()
		return err
	}
	for _, s := range symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO etf_universe (list, symbol) VALUES (?, ?)`, list, s); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// DTStrategy is one ETF's best-found decision-tree config.
type DTStrategy struct {
	Symbol    string  `db:"symbol"`
	TP        float64 `db:"tp"`
	SL        float64 `db:"sl"`
	Hold      int     `db:"hold"`
	CAGR      float64 `db:"cagr"`
	MaxDD     float64 `db:"max_dd"`
	MaxDDDays int     `db:"max_dd_days"`
	Trades    int     `db:"trades"`
	WinRate   float64 `db:"win_rate"`
	Score     float64 `db:"score"`
}

// DTStrategies returns all configs, highest score first. Missing table = none.
func DTStrategies(db *sqlx.DB) ([]DTStrategy, error) {
	var out []DTStrategy
	err := db.Select(&out, `SELECT symbol, tp, sl, hold, cagr, max_dd, max_dd_days, trades, win_rate, score
		FROM etf_dt_strategies ORDER BY score DESC, symbol`)
	return out, err
}

// SaveDTStrategies replaces the whole etf_dt_strategies table.
func SaveDTStrategies(db *sqlx.DB, rows []DTStrategy) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM etf_dt_strategies`); err != nil {
		tx.Rollback()
		return err
	}
	for _, r := range rows {
		if _, err := tx.NamedExec(`INSERT INTO etf_dt_strategies
			(symbol, tp, sl, hold, cagr, max_dd, max_dd_days, trades, win_rate, score)
			VALUES (:symbol, :tp, :sl, :hold, :cagr, :max_dd, :max_dd_days, :trades, :win_rate, :score)`, r); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
