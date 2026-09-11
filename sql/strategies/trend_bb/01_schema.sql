-- Schema for SQL-based Trend-Gated Bollinger Oversold Strategy



DROP TABLE IF EXISTS sma50_slice;
CREATE TABLE sma50_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    sma50 REAL,
    count50 INTEGER
);

DROP TABLE IF EXISTS bb_slice;
CREATE TABLE bb_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    sma20 REAL,
    sma20_sq REAL,
    count20 INTEGER,
    lower_bb REAL
);

DROP TABLE IF EXISTS rsi5_slice;
CREATE TABLE rsi5_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    rsi5 REAL
);

DROP TABLE IF EXISTS trend_bb_signals;
CREATE TABLE trend_bb_signals (
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
CREATE INDEX IF NOT EXISTS idx_trend_bb_signals ON trend_bb_signals (symbol, date);
