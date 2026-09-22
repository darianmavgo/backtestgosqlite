-- Precision 200-SMA Decision Tree Rule (Depth 3), same rule as TreeBounceSignals:
--   Condition A: RangeVsATR14 <= 0.45 (volatility coil / compression)
--   Condition B: PriceVsSMA200 in (-0.68%, 3.38%] (200-SMA re-test bounce)
INSERT INTO mara_tree_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss)
SELECT
    idx,
    'MARA' AS symbol,
    date,
    open, high, low, close, volume,
    close AS buylimit,
    1 AS entry,
    'LONG' AS direction,
    'All Regimes' AS regime,
    __HOLD_DAYS__ AS hold_days_override,
    close * __TAKE_PROFIT_MULT__ AS take_profit,
    close * __STOP_LOSS_MULT__ AS stop_loss
FROM mara_tree_features_slice
WHERE range_vs_atr14 <= 0.45
   OR (price_vs_sma200 <= 3.38 AND price_vs_sma200 > -0.68);
