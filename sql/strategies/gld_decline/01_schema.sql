-- Schema for GLD Decline Strategy Pipeline
CREATE TABLE IF NOT EXISTS gld_streaks_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    down_streak INTEGER,
    up_streak INTEGER
);
CREATE INDEX IF NOT EXISTS idx_gld_streaks_date ON gld_streaks_slice(date);

CREATE TABLE IF NOT EXISTS gld_decline_signals (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    buylimit REAL,
    entry INTEGER,
    direction TEXT,
    regime TEXT,
    hold_days_override INTEGER,
    take_profit REAL,
    stop_loss REAL
);
CREATE INDEX IF NOT EXISTS idx_gld_signals_date ON gld_decline_signals(date);
