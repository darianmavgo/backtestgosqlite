-- Generate Buy Signals on GLD when declining 2 days in a row
INSERT INTO gld_decline_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss)
SELECT
    idx,
    'GLD' AS symbol,
    date,
    open,
    high,
    low,
    close,
    volume,
    close AS buylimit,
    1 AS entry,
    'LONG' AS direction,
    'All Regimes' AS regime,
    12 AS hold_days_override,
    close * 1.08 AS take_profit,
    close * 0.98 AS stop_loss
FROM gld_streaks_slice
WHERE down_streak >= 2;
