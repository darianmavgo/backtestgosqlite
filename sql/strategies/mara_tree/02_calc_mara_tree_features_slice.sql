-- Calculate MARA's 200-day SMA and 14-day ATR (simple, unsmoothed average of
-- true range, matching TreeBounceSignals' Go reference implementation in
-- pkg/strategy/mara_tree.go) via window functions instead of per-row rescans.
WITH base AS (
    SELECT
        coalesce(idx, rowid, 0) AS idx,
        symbol,
        substr(Date, 1, 10) AS date,
        open, high, low, close, volume,
        LAG(close, 1) OVER (PARTITION BY symbol ORDER BY Date) AS prev_close,
        ROW_NUMBER() OVER (PARTITION BY symbol ORDER BY Date) AS rn
    FROM backtest_start
    WHERE symbol = 'MARA' AND length(Date) = 10
),
tr AS (
    SELECT
        *,
        max(high - low, abs(high - prev_close), abs(low - prev_close)) AS true_range
    FROM base
    WHERE prev_close IS NOT NULL
),
windowed AS (
    SELECT
        *,
        AVG(close) OVER w200 AS sma200,
        AVG(true_range) OVER w14 AS atr14
    FROM tr
    WINDOW
        w200 AS (PARTITION BY symbol ORDER BY date ROWS BETWEEN 199 PRECEDING AND CURRENT ROW),
        w14 AS (PARTITION BY symbol ORDER BY date ROWS BETWEEN 13 PRECEDING AND CURRENT ROW)
)
INSERT INTO mara_tree_features_slice (idx, symbol, date, open, high, low, close, volume, sma200, price_vs_sma200, atr14, range_vs_atr14)
SELECT
    idx, symbol, date, open, high, low, close, volume,
    sma200,
    (close - sma200) / sma200 * 100.0 AS price_vs_sma200,
    atr14,
    CASE WHEN atr14 > 0 THEN (high - low) / atr14 ELSE 1.0 END AS range_vs_atr14
FROM windowed
WHERE rn >= 201;
