-- Schema for the QQQ up+volume-up -> TQQQ strategy pipeline.
DROP TABLE IF EXISTS sig_qqq_up1_buy_tqqq_dates_slice;
CREATE TABLE sig_qqq_up1_buy_tqqq_dates_slice (
    date TEXT,
    up_and_vol_up INTEGER
);
CREATE INDEX IF NOT EXISTS idx_sig_qqq_up1_buy_tqqq_dates ON sig_qqq_up1_buy_tqqq_dates_slice(date);

DROP TABLE IF EXISTS sig_qqq_up1_buy_tqqq_signals;
CREATE TABLE sig_qqq_up1_buy_tqqq_signals (
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
CREATE INDEX IF NOT EXISTS idx_sig_qqq_up1_buy_tqqq_signals ON sig_qqq_up1_buy_tqqq_signals(symbol, date);
