INSERT INTO trend_calc_slice (idx, symbol, date, open, high, low, close, volume, prev_close, prev2_close, sma50, count50)
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
    LAG(close, 2) OVER (PARTITION BY symbol ORDER BY Date) AS prev2_close,
    AVG(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 49 PRECEDING AND CURRENT ROW) AS sma50,
    COUNT(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 49 PRECEDING AND CURRENT ROW) AS count50
FROM backtest_start;
