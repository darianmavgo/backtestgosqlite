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
INSERT INTO bb_slice (idx, symbol, date, open, high, low, close, volume, sma20, sma20_sq, count20, is_lower_bb_broken)
SELECT
    *,
    CASE
        WHEN sma20 > low AND 4.0 * MAX(0.0, sma20_sq - sma20 * sma20) < (sma20 - low) * (sma20 - low) THEN 1
        ELSE 0
    END AS is_lower_bb_broken
FROM bb_stats;
