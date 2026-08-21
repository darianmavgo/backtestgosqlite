-- Schema for SQL-based MACD Crossover Strategy
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
CREATE INDEX IF NOT EXISTS idx_macd_signals ON macd_crossover_signals (symbol, date);
