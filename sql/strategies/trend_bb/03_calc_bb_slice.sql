-- Calculate 20-day Bollinger Bands
WITH bb_stats AS (
    SELECT
        idx,
        symbol,
        date,
        open,
        high,
        low,
        close,
        volume,
        AVG(close) OVER (PARTITION BY symbol ORDER BY date ROWS BETWEEN 19 PRECEDING AND CURRENT ROW) AS sma20,
        AVG(close * close) OVER (PARTITION BY symbol ORDER BY date ROWS BETWEEN 19 PRECEDING AND CURRENT ROW) AS sma20_sq,
        COUNT(close) OVER (PARTITION BY symbol ORDER BY date ROWS BETWEEN 19 PRECEDING AND CURRENT ROW) AS count20
    FROM sma50_slice
)
INSERT INTO bb_slice (idx, symbol, date, open, high, low, close, volume, sma20, sma20_sq, count20, lower_bb)
SELECT
    *,
    CASE
        WHEN (sma20_sq - sma20 * sma20) > 0 THEN sma20 - (2.0 * SQRT(sma20_sq - sma20 * sma20))
        ELSE sma20 * 0.95
    END AS lower_bb
FROM bb_stats;
