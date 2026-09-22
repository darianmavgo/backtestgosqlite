-- Schema for NVDL Tree-Bounce Strategy Pipeline (Precision 200-SMA Re-test Bounce)
CREATE TABLE IF NOT EXISTS nvdl_tree_features_slice (
    idx INTEGER,
    symbol TEXT,
    date TEXT,
    open REAL,
    high REAL,
    low REAL,
    close REAL,
    volume INTEGER,
    sma200 REAL,
    price_vs_sma200 REAL,
    atr14 REAL,
    range_vs_atr14 REAL
);
CREATE INDEX IF NOT EXISTS idx_nvdl_tree_features_date ON nvdl_tree_features_slice(date);

CREATE TABLE IF NOT EXISTS nvdl_tree_signals (
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
CREATE INDEX IF NOT EXISTS idx_nvdl_tree_signals_date ON nvdl_tree_signals(date);
