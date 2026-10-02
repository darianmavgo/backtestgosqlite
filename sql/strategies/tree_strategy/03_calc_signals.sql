-- Generic Tree Rule (Depth 3)
INSERT INTO tree_strategy_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss)
SELECT
    idx,
    '__SYMBOL__' AS symbol,
    date,
    open, high, low, close, volume,
    close AS buylimit,
    1 AS entry,
    'LONG' AS direction,
    'All Regimes' AS regime,
    __HOLD_DAYS__ AS hold_days_override,
    close * __TAKE_PROFIT_MULT__ AS take_profit,
    close * __STOP_LOSS_MULT__ AS stop_loss
FROM tree_features_slice
WHERE range_vs_atr14 <= __COIL_MAX__
   OR (price_vs_sma200 <= __SMA_MAX__ AND price_vs_sma200 > __SMA_MIN__);
