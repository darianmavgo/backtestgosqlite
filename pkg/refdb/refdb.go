// Package refdb is the reference database (refdata/strategies.db): strategy
// configs live in SQLite tables (streak, tree, markov, hold).
package refdb

import (
	"fmt"
	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"os"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/jmoiron/sqlx"
)

// DefaultPath is the reference DB location (APP_FOLDER/refdata/strategies.db).
var DefaultPath = appenv.RefDB()

const schema = `
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
	if err := upgradeHold(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("refdb hold upgrade: %w", err)
	}
	// strategy_family_param: what gridsearch can vary per family (sql/stages/family_params).
	if err := storage.RunStage(db, "family_params", nil); err != nil {
		db.Close()
		return nil, fmt.Errorf("refdb family params: %w", err)
	}
	return db, nil
}

// upgradeHold brings an older hold_strategy table up to date on open. It adds
// trailing_stop_pct and sma_reentry_period (0 = never) when they are missing and
// copies in any hold_bail_strategy rows not yet there, which is how the hold_bail
// family was merged into hold. It never drops or deletes, and does nothing on a
// database without a hold_strategy table.
func upgradeHold(db *sqlx.DB) error {
	var cols []string
	if err := db.Select(&cols, `SELECT name FROM pragma_table_info('hold_strategy')`); err != nil || len(cols) == 0 {
		return err
	}
	have := map[string]bool{}
	for _, c := range cols {
		have[c] = true
	}
	if !have["trailing_stop_pct"] {
		if _, err := db.Exec(`ALTER TABLE hold_strategy ADD COLUMN trailing_stop_pct REAL NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	if !have["sma_reentry_period"] {
		if _, err := db.Exec(`ALTER TABLE hold_strategy ADD COLUMN sma_reentry_period INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'hold_bail_strategy'`); err != nil || n == 0 {
		return err
	}
	_, err := db.Exec(`
		INSERT OR IGNORE INTO hold_strategy
			(id, name, symbol, total_return, allocation_pct, cash_yield, slippage_pct, trailing_stop_pct, sma_reentry_period)
		SELECT id, name, symbol, 0, allocation_pct, cash_yield, slippage_pct, trailing_stop_pct, sma_reentry_period
		FROM hold_bail_strategy`)
	return err
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
func StreakStrategies(db *sqlx.DB) ([]StreakStrategy, error) { return streakStrategiesWhere(db, "") }

// StreakStrategyByID returns the row whose id is exactly id.
func StreakStrategyByID(db *sqlx.DB, id string) (StreakStrategy, bool, error) {
	rows, err := streakStrategiesWhere(db, " WHERE id = ?", id)
	if err != nil || len(rows) == 0 {
		return StreakStrategy{}, false, err
	}
	return rows[0], true, nil
}

func streakStrategiesWhere(db *sqlx.DB, where string, args ...any) ([]StreakStrategy, error) {
	var out []StreakStrategy
	err := db.Select(&out, `
		SELECT id, name, signal_symbol, trade_symbol, direction, signal_days, hold_days,
		       take_profit_pct, stop_loss_pct, regime, allocation_pct, cash_yield,
		       slippage_pct, next_day_limit,
		       COALESCE(source_strategy, '') AS source_strategy,
		       COALESCE(source_label, '') AS source_label,
		       win_rate, total_trades
		FROM streak_strategy` + where + ` ORDER BY id`, args...)
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
func MarkovStrategies(db *sqlx.DB) ([]MarkovStrategy, error) { return markovStrategiesWhere(db, "") }

// MarkovStrategyByID returns the row whose id is exactly id.
func MarkovStrategyByID(db *sqlx.DB, id string) (MarkovStrategy, bool, error) {
	rows, err := markovStrategiesWhere(db, " WHERE id = ?", id)
	if err != nil || len(rows) == 0 {
		return MarkovStrategy{}, false, err
	}
	return rows[0], true, nil
}

func markovStrategiesWhere(db *sqlx.DB, where string, args ...any) ([]MarkovStrategy, error) {
	var out []MarkovStrategy
	err := db.Select(&out, `
		SELECT id, name, signal_symbol, trade_symbol, direction, target_state, hold_days,
		       take_profit_pct, stop_loss_pct, allocation_pct, cash_yield,
		       slippage_pct, next_day_limit,
		       COALESCE(source_strategy, '') AS source_strategy,
		       COALESCE(source_label, '') AS source_label,
		       win_rate, total_trades
		FROM markov_strategy` + where + ` ORDER BY id`, args...)
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
func TreeStrategies(db *sqlx.DB) ([]TreeStrategy, error) { return treeStrategiesWhere(db, "") }

// TreeStrategyByID returns the row whose id is exactly id.
func TreeStrategyByID(db *sqlx.DB, id string) (TreeStrategy, bool, error) {
	rows, err := treeStrategiesWhere(db, " WHERE id = ?", id)
	if err != nil || len(rows) == 0 {
		return TreeStrategy{}, false, err
	}
	return rows[0], true, nil
}

func treeStrategiesWhere(db *sqlx.DB, where string, args ...any) ([]TreeStrategy, error) {
	var out []TreeStrategy
	err := db.Select(&out, `
		SELECT id, name, signal_symbol, trade_symbol, direction, hold_days,
		       take_profit_pct, stop_loss_pct, allocation_pct, cash_yield,
		       slippage_pct, next_day_limit, coil_range_max, sma_bounce_min, sma_bounce_max,
		       COALESCE(source_strategy, '') AS source_strategy,
		       COALESCE(source_label, '') AS source_label,
		       win_rate, total_trades
		FROM tree_strategy` + where + ` ORDER BY id`, args...)
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

// HoldStrategy is one row of hold_strategy.
type HoldStrategy struct {
	ID            string  `db:"id"`
	Name          string  `db:"name"`
	Symbol        string  `db:"symbol"`
	TotalReturn   int     `db:"total_return"`
	AllocationPct float64 `db:"allocation_pct"`
	CashYield     float64 `db:"cash_yield"`
	SlippagePct   float64 `db:"slippage_pct"`
	// TrailingStopPct is the trailing stop as a fraction of the high since entry;
	// 0 never bails. SMAReentryPeriod is the length of the average whose close
	// above it re-enters after a bail; 0 never re-enters.
	TrailingStopPct  float64 `db:"trailing_stop_pct"`
	SMAReentryPeriod int     `db:"sma_reentry_period"`
}

// HoldStrategies returns every hold_strategy row, ordered by id.
func HoldStrategies(db *sqlx.DB) ([]HoldStrategy, error) { return holdStrategiesWhere(db, "") }

// HoldStrategyByID returns the row whose id is exactly id.
func HoldStrategyByID(db *sqlx.DB, id string) (HoldStrategy, bool, error) {
	rows, err := holdStrategiesWhere(db, " WHERE id = ?", id)
	if err != nil || len(rows) == 0 {
		return HoldStrategy{}, false, err
	}
	return rows[0], true, nil
}

func holdStrategiesWhere(db *sqlx.DB, where string, args ...any) ([]HoldStrategy, error) {
	var out []HoldStrategy
	err := db.Select(&out, `
		SELECT id, name, symbol, total_return, allocation_pct, cash_yield, slippage_pct,
		       trailing_stop_pct, sma_reentry_period
		FROM hold_strategy` + where + ` ORDER BY id`, args...)
	return out, err
}

// strategyTables are the tables CanonicalID and IDs accept.
var strategyTables = map[string]bool{
	"streak_strategy": true, "hold_strategy": true,
	"tree_strategy": true, "markov_strategy": true,
}

// ExactID returns the stored id equal to id in table. It is an indexed lookup.
func ExactID(db *sqlx.DB, table, id string) (string, bool) {
	if !strategyTables[table] {
		return "", false
	}
	var got string
	if err := db.Get(&got, "SELECT id FROM "+table+" WHERE id = ?", id); err != nil {
		return "", false
	}
	return got, true
}

// CanonicalID returns the stored id that matches id in table: exact first,
// then ignoring case, "-", "_" and spaces (the same looseness strategy.Get
// has always had). The loose match scans the table, so exact ids are cheap.
func CanonicalID(db *sqlx.DB, table, id string) (string, bool) {
	if got, ok := ExactID(db, table, id); ok {
		return got, true
	}
	if !strategyTables[table] {
		return "", false
	}
	norm := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(id))
	if norm == "" {
		return "", false
	}
	var got string
	err := db.Get(&got, "SELECT id FROM "+table+
		" WHERE replace(replace(replace(lower(id),'-',''),'_',''),' ','') = ? ORDER BY id LIMIT 1", norm)
	return got, err == nil
}

// IDs returns every id in a strategy table, ordered. A missing table returns an error.
func IDs(db *sqlx.DB, table string) ([]string, error) {
	if !strategyTables[table] {
		return nil, fmt.Errorf("refdb: %q is not a strategy table", table)
	}
	var out []string
	err := db.Select(&out, "SELECT id FROM "+table+" ORDER BY id")
	return out, err
}

// FamilyParam is one row of strategy_family_param: what a grid search may do
// with one column of a family's rows.
type FamilyParam struct {
	Family         string  `db:"family"`
	Param          string  `db:"param"`
	Kind           string  `db:"kind"`
	Role           string  `db:"role"`
	Gridsearchable bool    `db:"gridsearchable"`
	GridValues     *string `db:"grid_values"`
	WhyNot         *string `db:"why_not"`
}

// FamilyParams returns every strategy_family_param row of one family, by param.
func FamilyParams(db *sqlx.DB, family string) ([]FamilyParam, error) {
	var out []FamilyParam
	err := db.Select(&out, `SELECT family, param, kind, role, gridsearchable, grid_values, why_not
		FROM strategy_family_param WHERE family = ? ORDER BY param`, family)
	return out, err
}
