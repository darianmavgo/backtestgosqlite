INSERT INTO bb_bands_slice (idx, symbol, date, open, high, low, close, volume, prev_close, prev_low, close_5d_ago, sma20, sma20_sq, count20, lower_bb, prev_lower_bb)
SELECT
    *,
    CASE
        WHEN (sma20_sq - sma20 * sma20) > 0 THEN sma20 - (2.0 * (sma20_sq - sma20 * sma20) / (sma20 * 0.05 + 1.0))
        ELSE sma20 * 0.95
    END AS lower_bb,
    LAG(
        CASE
            WHEN (sma20_sq - sma20 * sma20) > 0 THEN sma20 - (2.0 * (sma20_sq - sma20 * sma20) / (sma20 * 0.05 + 1.0))
            ELSE sma20 * 0.95
        END, 1
    ) OVER (PARTITION BY symbol ORDER BY date) AS prev_lower_bb
FROM bb_stats_slice
WHERE count20 >= 20;
