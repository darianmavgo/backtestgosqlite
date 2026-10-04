-- Enter on the first bar. When __SMA_PERIOD__ is above 0, also enter on every
-- bar after the average is full whose close is above it. A trailing stop, if the
-- row has one, is the simulator's exit and nothing here sells.
INSERT INTO hold_strategy_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss, allocation_pct_override)
SELECT
    idx, symbol, date, open, high, low, close, volume,
    close,
    1,
    'LONG',
    CASE WHEN __SMA_PERIOD__ > 0 THEN 'Close>SMA__SMA_PERIOD__' ELSE 'All Regimes' END,
    0,
    0.0,
    0.0,
    0.0
FROM hold_strategy_bars
WHERE rn = 1 OR (__SMA_PERIOD__ > 0 AND rn > __SMA_PERIOD__ AND close > sma)
ORDER BY date;
