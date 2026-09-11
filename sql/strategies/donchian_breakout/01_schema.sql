-- Schema for SQL-based Donchian Breakout Strategy



DROP TABLE IF EXISTS donchian_slice;
CREATE TABLE donchian_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    upper_20d REAL
);

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
