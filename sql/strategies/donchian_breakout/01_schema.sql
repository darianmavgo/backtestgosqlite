-- Schema for SQL-based Donchian Breakout Strategy
CREATE UNIQUE INDEX IF NOT EXISTS idx_backtest_start_unique ON backtest_start(symbol, Date);
CREATE INDEX IF NOT EXISTS idx_backtest_start_sym_date ON backtest_start(symbol, Date);

DROP TABLE IF EXISTS donchian_breakout_signals;
CREATE TABLE donchian_breakout_signals (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    buylimit REAL,
    entry INTEGER DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_donchian_signals ON donchian_breakout_signals (symbol, date);
