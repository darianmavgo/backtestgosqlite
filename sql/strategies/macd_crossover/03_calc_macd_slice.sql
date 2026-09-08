-- Calculate MACD Line (Fast MA - Slow MA)
INSERT INTO macd_slice (idx, symbol, date, open, high, low, close, volume, macd_line)
SELECT
    idx,
    symbol,
    date,
    open,
    high,
    low,
    close,
    volume,
    (fast_ma - slow_ma) AS macd_line
FROM fast_slow_ma_slice
WHERE count26 >= 26;
