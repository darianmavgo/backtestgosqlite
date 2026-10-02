-- Enter on the first bar, then on every bar after the average is full whose
-- close is above it.
INSERT INTO hold_bail_strategy_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss, allocation_pct_override)
SELECT
    idx, symbol, date, open, high, low, close, volume,
    close,
    1,
    'LONG',
    'Close>SMA__SMA_PERIOD__',
    0,
    0.0,
    0.0,
    0.0
FROM hold_bail_sma
WHERE rn = 1 OR (rn > __SMA_PERIOD__ AND close > sma)
ORDER BY date;
