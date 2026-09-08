-- Schema for SQL-based VOO TECL Combo Strategy
CREATE UNIQUE INDEX IF NOT EXISTS idx_backtest_start_unique ON backtest_start(symbol, Date);
CREATE INDEX IF NOT EXISTS idx_backtest_start_sym_date ON backtest_start(symbol, Date);

DROP TABLE IF EXISTS voo_streaks_slice;
CREATE TABLE voo_streaks_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    sma200 REAL,
    down_streak INTEGER,
    up_streak INTEGER
);

DROP TABLE IF EXISTS voo_tecl_combo_signals;
CREATE TABLE voo_tecl_combo_signals (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    buylimit REAL,
    entry INTEGER DEFAULT 0,
    direction TEXT,
    regime TEXT,
    hold_days_override INTEGER,
    take_profit REAL,
    stop_loss REAL
);
CREATE INDEX IF NOT EXISTS idx_voo_tecl_combo_signals ON voo_tecl_combo_signals (symbol, date);
