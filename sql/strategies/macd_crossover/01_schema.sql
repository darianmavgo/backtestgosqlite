-- Schema for SQL-based MACD Signal Line Crossover Strategy



DROP TABLE IF EXISTS fast_slow_ma_slice;
CREATE TABLE fast_slow_ma_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    fast_ma REAL,
    slow_ma REAL,
    count26 INTEGER
);

DROP TABLE IF EXISTS macd_slice;
CREATE TABLE macd_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    macd_line REAL
);

DROP TABLE IF EXISTS macd_signal_slice;
CREATE TABLE macd_signal_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    macd_line REAL,
    signal_line REAL,
    prev_macd REAL,
    prev_signal REAL
);


DROP TABLE IF EXISTS macd_crossover_signals;
CREATE TABLE macd_crossover_signals (
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
CREATE INDEX IF NOT EXISTS idx_macd_crossover_signals ON macd_crossover_signals (symbol, date);
