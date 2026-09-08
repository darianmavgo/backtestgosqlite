INSERT INTO bb_stats_slice (idx, symbol, date, open, high, low, close, volume, prev_close, prev_low, close_5d_ago, sma20, sma20_sq, count20)
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
    LAG(low, 1) OVER (PARTITION BY symbol ORDER BY Date) AS prev_low,
    LAG(close, 5) OVER (PARTITION BY symbol ORDER BY Date) AS close_5d_ago,
    AVG(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 19 PRECEDING AND CURRENT ROW) AS sma20,
    AVG(close * close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 19 PRECEDING AND CURRENT ROW) AS sma20_sq,
    COUNT(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 19 PRECEDING AND CURRENT ROW) AS count20
FROM backtest_start;
