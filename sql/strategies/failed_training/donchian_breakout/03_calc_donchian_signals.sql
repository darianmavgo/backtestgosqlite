-- 20-Day Donchian Channel Breakout Signals
INSERT INTO donchian_breakout_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry)
SELECT
    idx,
    symbol,
    date,
    open,
    high,
    low,
    close,
    volume,
    close AS buylimit,
    1 AS entry
FROM donchian_slice
WHERE upper_20d IS NOT NULL AND close > upper_20d;
