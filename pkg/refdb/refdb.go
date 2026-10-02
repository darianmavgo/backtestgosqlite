// Package refdb is the reference database (APP_REF/settings.db): ticker
// universes and per-ETF decision-tree configs live in SQLite tables.
package refdb

import (
	"fmt"
	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"os"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/jmoiron/sqlx"
)

// DefaultPath is the reference DB location (APP_FOLDER/APP_REF/settings.db).
var DefaultPath = appenv.RefDB()

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
-- Full decision-tree fit results for every ETF (archive/reference). Only
-- etf_dt_strategies below drives strategy registration.
CREATE TABLE IF NOT EXISTS etf_dt_strategies_all (
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
-- One runnable streak strategy per row. pkg/streak_strategy registers each
-- row. take_profit_pct and stop_loss_pct are fractional offsets (0.08 = 8%),
-- matching gridsearch_results, not StrategyConfig.StopLossPct's multiplier.
CREATE TABLE IF NOT EXISTS streak_strategy (
	id               TEXT PRIMARY KEY,
	name             TEXT NOT NULL,
	signal_symbol    TEXT NOT NULL,
	trade_symbol     TEXT NOT NULL,
	direction        TEXT NOT NULL,
	signal_days      INTEGER NOT NULL,
	hold_days        INTEGER NOT NULL,
	take_profit_pct  REAL NOT NULL,
	stop_loss_pct    REAL NOT NULL,
	regime           TEXT NOT NULL,
	allocation_pct   REAL NOT NULL,
	cash_yield       REAL NOT NULL,
	slippage_pct     REAL NOT NULL,
	next_day_limit   INTEGER NOT NULL,
	source_strategy  TEXT,
	source_label     TEXT,
	win_rate         REAL,
	total_trades     INTEGER
);
-- One runnable markov strategy per row, similar to streak_strategy.
CREATE TABLE IF NOT EXISTS tree_strategy (
	id               TEXT PRIMARY KEY,
	name             TEXT NOT NULL,
	signal_symbol    TEXT NOT NULL,
	trade_symbol     TEXT NOT NULL,
	direction        TEXT NOT NULL,
	hold_days        INTEGER NOT NULL,
	take_profit_pct  REAL NOT NULL,
	stop_loss_pct    REAL NOT NULL,
	allocation_pct   REAL NOT NULL,
	cash_yield       REAL NOT NULL,
	slippage_pct     REAL NOT NULL,
	next_day_limit   INTEGER NOT NULL,
	coil_range_max   REAL NOT NULL,
	sma_bounce_min   REAL NOT NULL,
	sma_bounce_max   REAL NOT NULL,
	source_strategy  TEXT,
	source_label     TEXT,
	win_rate         REAL,
	total_trades     INTEGER
);

CREATE TABLE IF NOT EXISTS markov_strategy (
	id               TEXT PRIMARY KEY,
	name             TEXT NOT NULL,
	signal_symbol    TEXT NOT NULL,
	trade_symbol     TEXT NOT NULL,
	direction        TEXT NOT NULL,
	target_state     TEXT NOT NULL,
	hold_days        INTEGER NOT NULL,
	take_profit_pct  REAL NOT NULL,
	stop_loss_pct    REAL NOT NULL,
	allocation_pct   REAL NOT NULL,
	cash_yield       REAL NOT NULL,
	slippage_pct     REAL NOT NULL,
	next_day_limit   INTEGER NOT NULL,
	source_strategy  TEXT,
	source_label     TEXT,
	win_rate         REAL,
	total_trades     INTEGER
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

// StreakStrategy is one row of streak_strategy: the symbol watched for the
// streak, the symbol bought, and the grid-searched exit parameters.
// TakeProfitPct and StopLossPct are fractional offsets (0.08 = 8%).
type StreakStrategy struct {
	ID             string   `db:"id"`
	Name           string   `db:"name"`
	SignalSymbol   string   `db:"signal_symbol"`
	TradeSymbol    string   `db:"trade_symbol"`
	Direction      string   `db:"direction"`
	SignalDays     int      `db:"signal_days"`
	HoldDays       int      `db:"hold_days"`
	TakeProfitPct  float64  `db:"take_profit_pct"`
	StopLossPct    float64  `db:"stop_loss_pct"`
	Regime         string   `db:"regime"`
	AllocationPct  float64  `db:"allocation_pct"`
	CashYield      float64  `db:"cash_yield"`
	SlippagePct    float64  `db:"slippage_pct"`
	NextDayLimit   int      `db:"next_day_limit"`
	SourceStrategy string   `db:"source_strategy"`
	SourceLabel    string   `db:"source_label"`
	WinRate        *float64 `db:"win_rate"`
	TotalTrades    *int     `db:"total_trades"`
}

// StreakStrategies returns every streak_strategy row, ordered by id.
// A missing table returns an error; callers that register strategies treat
// that as an empty list.
func StreakStrategies(db *sqlx.DB) ([]StreakStrategy, error) {
	var out []StreakStrategy
	err := db.Select(&out, `
		SELECT id, name, signal_symbol, trade_symbol, direction, signal_days, hold_days,
		       take_profit_pct, stop_loss_pct, regime, allocation_pct, cash_yield,
		       slippage_pct, next_day_limit,
		       COALESCE(source_strategy, '') AS source_strategy,
		       COALESCE(source_label, '') AS source_label,
		       win_rate, total_trades
		FROM streak_strategy
		ORDER BY id`)
	return out, err
}

// UpsertStreakStrategies inserts or replaces each row by id.
func UpsertStreakStrategies(db *sqlx.DB, rows []StreakStrategy) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO streak_strategy (
			id, name, signal_symbol, trade_symbol, direction, signal_days, hold_days,
			take_profit_pct, stop_loss_pct, regime, allocation_pct, cash_yield,
			slippage_pct, next_day_limit, source_strategy, source_label, win_rate, total_trades
		) VALUES (
			:id, :name, :signal_symbol, :trade_symbol, :direction, :signal_days, :hold_days,
			:take_profit_pct, :stop_loss_pct, :regime, :allocation_pct, :cash_yield,
			:slippage_pct, :next_day_limit, :source_strategy, :source_label, :win_rate, :total_trades
		)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			signal_symbol = excluded.signal_symbol,
			trade_symbol = excluded.trade_symbol,
			direction = excluded.direction,
			signal_days = excluded.signal_days,
			hold_days = excluded.hold_days,
			take_profit_pct = excluded.take_profit_pct,
			stop_loss_pct = excluded.stop_loss_pct,
			regime = excluded.regime,
			allocation_pct = excluded.allocation_pct,
			cash_yield = excluded.cash_yield,
			slippage_pct = excluded.slippage_pct,
			next_day_limit = excluded.next_day_limit,
			source_strategy = excluded.source_strategy,
			source_label = excluded.source_label,
			win_rate = excluded.win_rate,
			total_trades = excluded.total_trades`
	for _, r := range rows {
		if _, err := tx.NamedExec(q, r); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// MarkovStrategy is one row of markov_strategy.
type MarkovStrategy struct {
	ID             string   `db:"id"`
	Name           string   `db:"name"`
	SignalSymbol   string   `db:"signal_symbol"`
	TradeSymbol    string   `db:"trade_symbol"`
	Direction      string   `db:"direction"`
	TargetState    string   `db:"target_state"`
	HoldDays       int      `db:"hold_days"`
	TakeProfitPct  float64  `db:"take_profit_pct"`
	StopLossPct    float64  `db:"stop_loss_pct"`
	AllocationPct  float64  `db:"allocation_pct"`
	CashYield      float64  `db:"cash_yield"`
	SlippagePct    float64  `db:"slippage_pct"`
	NextDayLimit   int      `db:"next_day_limit"`
	SourceStrategy string   `db:"source_strategy"`
	SourceLabel    string   `db:"source_label"`
	WinRate        *float64 `db:"win_rate"`
	TotalTrades    *int     `db:"total_trades"`
}

// MarkovStrategies returns every markov_strategy row, ordered by id.
func MarkovStrategies(db *sqlx.DB) ([]MarkovStrategy, error) {
	var out []MarkovStrategy
	err := db.Select(&out, `
		SELECT id, name, signal_symbol, trade_symbol, direction, target_state, hold_days,
		       take_profit_pct, stop_loss_pct, allocation_pct, cash_yield,
		       slippage_pct, next_day_limit,
		       COALESCE(source_strategy, '') AS source_strategy,
		       COALESCE(source_label, '') AS source_label,
		       win_rate, total_trades
		FROM markov_strategy
		ORDER BY id`)
	return out, err
}

// UpsertMarkovStrategies inserts or replaces each row by id.
func UpsertMarkovStrategies(db *sqlx.DB, rows []MarkovStrategy) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	const q = `
CREATE TABLE IF NOT EXISTS tree_strategy (
	id               TEXT PRIMARY KEY,
	name             TEXT NOT NULL,
	signal_symbol    TEXT NOT NULL,
	trade_symbol     TEXT NOT NULL,
	direction        TEXT NOT NULL,
	hold_days        INTEGER NOT NULL,
	take_profit_pct  REAL NOT NULL,
	stop_loss_pct    REAL NOT NULL,
	allocation_pct   REAL NOT NULL,
	cash_yield       REAL NOT NULL,
	slippage_pct     REAL NOT NULL,
	next_day_limit   INTEGER NOT NULL,
	coil_range_max   REAL NOT NULL,
	sma_bounce_min   REAL NOT NULL,
	sma_bounce_max   REAL NOT NULL,
	source_strategy  TEXT,
	source_label     TEXT,
	win_rate         REAL,
	total_trades     INTEGER
);

		INSERT INTO markov_strategy (
			id, name, signal_symbol, trade_symbol, direction, target_state, hold_days,
			take_profit_pct, stop_loss_pct, allocation_pct, cash_yield,
			slippage_pct, next_day_limit, source_strategy, source_label, win_rate, total_trades
		) VALUES (
			:id, :name, :signal_symbol, :trade_symbol, :direction, :target_state, :hold_days,
			:take_profit_pct, :stop_loss_pct, :allocation_pct, :cash_yield,
			:slippage_pct, :next_day_limit, :source_strategy, :source_label, :win_rate, :total_trades
		)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			signal_symbol = excluded.signal_symbol,
			trade_symbol = excluded.trade_symbol,
			direction = excluded.direction,
			target_state = excluded.target_state,
			hold_days = excluded.hold_days,
			take_profit_pct = excluded.take_profit_pct,
			stop_loss_pct = excluded.stop_loss_pct,
			allocation_pct = excluded.allocation_pct,
			cash_yield = excluded.cash_yield,
			slippage_pct = excluded.slippage_pct,
			next_day_limit = excluded.next_day_limit,
			source_strategy = excluded.source_strategy,
			source_label = excluded.source_label,
			win_rate = excluded.win_rate,
			total_trades = excluded.total_trades`
	for _, r := range rows {
		if _, err := tx.NamedExec(q, r); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// TreeStrategy is one row of tree_strategy.
type TreeStrategy struct {
	ID             string   `db:"id"`
	Name           string   `db:"name"`
	SignalSymbol   string   `db:"signal_symbol"`
	TradeSymbol    string   `db:"trade_symbol"`
	Direction      string   `db:"direction"`
	HoldDays       int      `db:"hold_days"`
	TakeProfitPct  float64  `db:"take_profit_pct"`
	StopLossPct    float64  `db:"stop_loss_pct"`
	AllocationPct  float64  `db:"allocation_pct"`
	CashYield      float64  `db:"cash_yield"`
	SlippagePct    float64  `db:"slippage_pct"`
	NextDayLimit   int      `db:"next_day_limit"`
	CoilRangeMax   float64  `db:"coil_range_max"`
	SMABounceMin   float64  `db:"sma_bounce_min"`
	SMABounceMax   float64  `db:"sma_bounce_max"`
	SourceStrategy string   `db:"source_strategy"`
	SourceLabel    string   `db:"source_label"`
	WinRate        *float64 `db:"win_rate"`
	TotalTrades    *int     `db:"total_trades"`
}

// TreeStrategies returns every tree_strategy row, ordered by id.
func TreeStrategies(db *sqlx.DB) ([]TreeStrategy, error) {
	var out []TreeStrategy
	err := db.Select(&out, `
		SELECT id, name, signal_symbol, trade_symbol, direction, hold_days,
		       take_profit_pct, stop_loss_pct, allocation_pct, cash_yield,
		       slippage_pct, next_day_limit, coil_range_max, sma_bounce_min, sma_bounce_max,
		       COALESCE(source_strategy, '') AS source_strategy,
		       COALESCE(source_label, '') AS source_label,
		       win_rate, total_trades
		FROM tree_strategy
		ORDER BY id`)
	return out, err
}

// UpsertTreeStrategies inserts or replaces each row by id.
func UpsertTreeStrategies(db *sqlx.DB, rows []TreeStrategy) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO tree_strategy (
			id, name, signal_symbol, trade_symbol, direction, hold_days,
			take_profit_pct, stop_loss_pct, allocation_pct, cash_yield,
			slippage_pct, next_day_limit, coil_range_max, sma_bounce_min, sma_bounce_max,
			source_strategy, source_label, win_rate, total_trades
		) VALUES (
			:id, :name, :signal_symbol, :trade_symbol, :direction, :hold_days,
			:take_profit_pct, :stop_loss_pct, :allocation_pct, :cash_yield,
			:slippage_pct, :next_day_limit, :coil_range_max, :sma_bounce_min, :sma_bounce_max,
			:source_strategy, :source_label, :win_rate, :total_trades
		)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			signal_symbol = excluded.signal_symbol,
			trade_symbol = excluded.trade_symbol,
			direction = excluded.direction,
			hold_days = excluded.hold_days,
			take_profit_pct = excluded.take_profit_pct,
			stop_loss_pct = excluded.stop_loss_pct,
			allocation_pct = excluded.allocation_pct,
			cash_yield = excluded.cash_yield,
			slippage_pct = excluded.slippage_pct,
			next_day_limit = excluded.next_day_limit,
			coil_range_max = excluded.coil_range_max,
			sma_bounce_min = excluded.sma_bounce_min,
			sma_bounce_max = excluded.sma_bounce_max,
			source_strategy = excluded.source_strategy,
			source_label = excluded.source_label,
			win_rate = excluded.win_rate,
			total_trades = excluded.total_trades`
	for _, r := range rows {
		if _, err := tx.NamedExec(q, r); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
