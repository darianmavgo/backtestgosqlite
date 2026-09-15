-- Schema for SQL-based VOO TECL Combo Strategy



DROP TABLE IF EXISTS voo_streaks_slice;
CREATE TABLE voo_streaks_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    sma200 REAL,
    down_streak INTEGER,
    up_streak INTEGER
);

DROP TABLE IF EXISTS sig_voo_buy_tecl_signals;
CREATE TABLE sig_voo_buy_tecl_signals (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    buylimit REAL,
    entry INTEGER DEFAULT 0,
    direction TEXT,
    regime TEXT,
    hold_days_override INTEGER,
    take_profit REAL,
    stop_loss REAL
);
CREATE INDEX IF NOT EXISTS idx_sig_voo_buy_tecl_signals ON sig_voo_buy_tecl_signals (symbol, date);
