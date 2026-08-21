-- Schema for SQL-based BB Capitulation Strategy
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
CREATE INDEX IF NOT EXISTS idx_bb_cap_signals ON bb_capitulation_signals (symbol, date);
