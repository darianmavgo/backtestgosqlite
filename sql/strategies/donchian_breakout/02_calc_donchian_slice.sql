-- Calculate 20-Day Donchian Channel Upper Band
INSERT INTO donchian_slice (idx, symbol, date, open, high, low, close, volume, upper_20d)
SELECT
    coalesce(idx, rowid, 0) AS idx,
    symbol,
    substr(Date, 1, 10) AS date,
    open,
    high,
    low,
    close,
    volume,
    MAX(high) OVER (
        PARTITION BY symbol
        ORDER BY Date
        ROWS BETWEEN 20 PRECEDING AND 1 PRECEDING
    ) AS upper_20d
FROM backtest_start;
