-- Schema for SQL-based Millwharf Dynamic Volatility Breakout Strategy



DROP TABLE IF EXISTS ranked_slice;
CREATE TABLE ranked_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    prev_close REAL,
    high6d REAL
);

DROP TABLE IF EXISTS streaks_slice;
CREATE TABLE streaks_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    prev_close REAL,
    high6d REAL,
    reset_flag INTEGER,
    grp INTEGER,
    streak INTEGER,
    peak_close REAL
);

DROP TABLE IF EXISTS qualifying_slice;
CREATE TABLE qualifying_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    week_id TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    high6d REAL,
    streak INTEGER,
    drop_pct REAL,
    take_profit REAL,
    weekly_rank INTEGER
);

DROP TABLE IF EXISTS millwharf_signals;
CREATE TABLE millwharf_signals (
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
CREATE INDEX IF NOT EXISTS idx_millwharf_signals ON millwharf_signals (symbol, date);
