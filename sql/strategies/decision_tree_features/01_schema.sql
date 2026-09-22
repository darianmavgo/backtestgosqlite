-- Schema for the generic CloudForest decision-tree feature pipeline.
-- Shared by every ETFDecisionTreeStrategy instance (pkg/strategy/etf_decision_tree.go)
-- -- __SYMBOL__ is substituted per-instance from StrategyConfig.Benchmark, so one
-- pipeline directory serves all dt_<symbol> strategies instead of one per ticker.
-- Mirrors the 13-feature set computeDecisionTreeSamples() computes in Go
-- (pkg/strategy/decisiontree.go) via nested per-row rescans, done here as one
-- window-function pass per symbol instead. Tree *fitting* (CloudForest) stays
-- in Go -- this table is only the feature/label extraction stage.
-- NOTE: pipeline .sql files are split on the statement terminator character by
-- the Go runner, so no comment in this directory may contain one.
DROP TABLE IF EXISTS decision_tree_features_slice;
CREATE TABLE decision_tree_features_slice (
    date TEXT,
    close REAL,
    next_return REAL,
    is_gain5 INTEGER,
    is_drop5 INTEGER,
    return_1d REAL,
    return_3d REAL,
    return_5d REAL,
    return_10d REAL,
    rsi14 REAL,
    price_vs_sma20 REAL,
    price_vs_sma50 REAL,
    price_vs_sma200 REAL,
    sma20_vs_50 REAL,
    vol_ratio20 REAL,
    range_vs_atr14 REAL,
    close_near_high REAL,
    consecutive_down INTEGER
);
CREATE INDEX IF NOT EXISTS idx_dtf_date ON decision_tree_features_slice(date);
