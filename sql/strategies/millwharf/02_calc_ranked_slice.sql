INSERT INTO ranked_slice (idx, symbol, date, open, high, low, close, volume, prev_close, high6d)
SELECT
    coalesce(idx, rowid, 0) AS idx,
    symbol,
    substr(Date, 1, 10) AS date,
    open,
    high,
    low,
    close,
    volume,
    LAG(close, 1) OVER (PARTITION BY symbol ORDER BY Date) AS prev_close,
    MAX(high) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 5 PRECEDING AND CURRENT ROW) AS high6d
FROM backtest_start;
