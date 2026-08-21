-- Schema for SQL-based RSI2 Trend Pullback Strategy
DROP TABLE IF EXISTS rsi2_trend_signals;
CREATE TABLE rsi2_trend_signals (
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
CREATE INDEX IF NOT EXISTS idx_rsi2_signals ON rsi2_trend_signals (symbol, date);
