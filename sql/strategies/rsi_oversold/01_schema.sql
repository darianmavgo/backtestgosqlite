-- Schema for SQL-based RSI Oversold Strategy
DROP TABLE IF EXISTS rsi_oversold_signals;
CREATE TABLE rsi_oversold_signals (
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
CREATE INDEX IF NOT EXISTS idx_rsi_signals ON rsi_oversold_signals (symbol, date);
