-- Schema for SQL-based VOO TECL SPXU Combo Strategy (VOO & TECL 3-day decline)



DROP TABLE IF EXISTS voo_tecl_streaks_slice;
CREATE TABLE voo_tecl_streaks_slice (
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
CREATE INDEX IF NOT EXISTS idx_voo_tecl_streaks_slice_sym_date ON voo_tecl_streaks_slice(symbol, date);

DROP TABLE IF EXISTS voo_tecl_spxu_combo_signals;
CREATE TABLE voo_tecl_spxu_combo_signals (
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
CREATE INDEX IF NOT EXISTS idx_voo_tecl_spxu_combo_signals ON voo_tecl_spxu_combo_signals (symbol, date);
