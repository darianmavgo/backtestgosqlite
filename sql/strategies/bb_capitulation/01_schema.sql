-- Schema for SQL-based Bollinger Band Capitulation + Reversal Bounce Strategy
CREATE UNIQUE INDEX IF NOT EXISTS idx_backtest_start_unique ON backtest_start(symbol, Date);
CREATE INDEX IF NOT EXISTS idx_backtest_start_sym_date ON backtest_start(symbol, Date);

DROP TABLE IF EXISTS bb_stats_slice;
CREATE TABLE bb_stats_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    prev_close REAL,
    prev_low REAL,
    close_5d_ago REAL,
    sma20 REAL,
    sma20_sq REAL,
    count20 INTEGER
);

DROP TABLE IF EXISTS bb_bands_slice;
CREATE TABLE bb_bands_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    prev_close REAL,
    prev_low REAL,
    close_5d_ago REAL,
    sma20 REAL,
    sma20_sq REAL,
    count20 INTEGER,
    lower_bb REAL,
    prev_lower_bb REAL
);


DROP TABLE IF EXISTS bb_capitulation_signals;
CREATE TABLE bb_capitulation_signals (
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
CREATE INDEX IF NOT EXISTS idx_bb_capitulation_signals ON bb_capitulation_signals (symbol, date);
