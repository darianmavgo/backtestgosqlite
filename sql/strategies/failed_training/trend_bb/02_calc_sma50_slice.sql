-- Calculate 50-day Simple Moving Average
INSERT INTO sma50_slice (idx, symbol, date, open, high, low, close, volume, sma50, count50)
SELECT
    coalesce(idx, rowid, 0) AS idx,
    symbol,
    substr(Date, 1, 10) AS date,
    open,
    high,
    low,
    close,
    volume,
    AVG(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 49 PRECEDING AND CURRENT ROW) AS sma50,
    COUNT(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 49 PRECEDING AND CURRENT ROW) AS count50
FROM backtest_start;
