-- Calculate 12-day fast MA and 26-day slow MA
INSERT INTO fast_slow_ma_slice (idx, symbol, date, open, high, low, close, volume, fast_ma, slow_ma, count26)
SELECT
    coalesce(idx, rowid, 0) AS idx,
    symbol,
    substr(Date, 1, 10) AS date,
    open,
    high,
    low,
    close,
    volume,
    AVG(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 11 PRECEDING AND CURRENT ROW) AS fast_ma,
    AVG(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 25 PRECEDING AND CURRENT ROW) AS slow_ma,
    COUNT(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 25 PRECEDING AND CURRENT ROW) AS count26
FROM backtest_start;
