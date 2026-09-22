-- Computes the same 13 features + label as computeDecisionTreeSamples() in
-- pkg/strategy/decisiontree.go, via window functions instead of per-row
-- rescans of up to 200 trailing bars. RSI14/ATR14 here are simple (unweighted)
-- 14-row averages, matching that Go function exactly (not Wilder smoothing).
WITH base AS (
    SELECT
        coalesce(idx, rowid, 0) AS idx,
        symbol,
        substr(Date, 1, 10) AS date,
        open, high, low, close, volume,
        LAG(close, 1)  OVER (PARTITION BY symbol ORDER BY Date) AS prev_close,
        LAG(close, 3)  OVER (PARTITION BY symbol ORDER BY Date) AS prev_close_3,
        LAG(close, 5)  OVER (PARTITION BY symbol ORDER BY Date) AS prev_close_5,
        LAG(close, 10) OVER (PARTITION BY symbol ORDER BY Date) AS prev_close_10,
        LEAD(close, 1) OVER (PARTITION BY symbol ORDER BY Date) AS next_close,
        ROW_NUMBER()   OVER (PARTITION BY symbol ORDER BY Date) AS rn
    FROM backtest_start
    WHERE symbol = '__SYMBOL__' AND length(Date) = 10
),
chg AS (
    SELECT
        *,
        close - prev_close AS chg,
        max(high - low, abs(high - prev_close), abs(low - prev_close)) AS true_range,
        CASE WHEN close < prev_close THEN 1 ELSE 0 END AS is_down
    FROM base
    WHERE prev_close IS NOT NULL
),
streak_grp AS (
    -- down_grp is 0 for non-down rows (not the running sum) so a non-down day
    -- never shares a group with the down-streak that starts right after it —
    -- see sql/strategies/gld_decline/02_calc_gld_streaks_slice.sql for the
    -- same boundary-row fix.
    SELECT
        *,
        CASE WHEN is_down = 1
             THEN SUM(CASE WHEN is_down = 0 THEN 1 ELSE 0 END) OVER (PARTITION BY symbol ORDER BY date)
             ELSE 0 END AS down_grp
    FROM chg
),
windowed AS (
    SELECT
        *,
        AVG(close)  OVER w20  AS sma20,
        AVG(close)  OVER w50  AS sma50,
        AVG(close)  OVER w200 AS sma200,
        AVG(CASE WHEN chg > 0 THEN chg ELSE 0 END)  OVER w14 AS avg_gain14,
        AVG(CASE WHEN chg < 0 THEN -chg ELSE 0 END) OVER w14 AS avg_loss14,
        AVG(volume)      OVER w20 AS vol_avg20,
        AVG(true_range)  OVER w14 AS atr14,
        CASE WHEN is_down = 1
             THEN COUNT(*) OVER (PARTITION BY symbol, down_grp ORDER BY date)
             ELSE 0 END AS down_streak_uncapped
    FROM streak_grp
    WINDOW
        w20  AS (PARTITION BY symbol ORDER BY date ROWS BETWEEN 19  PRECEDING AND CURRENT ROW),
        w50  AS (PARTITION BY symbol ORDER BY date ROWS BETWEEN 49  PRECEDING AND CURRENT ROW),
        w200 AS (PARTITION BY symbol ORDER BY date ROWS BETWEEN 199 PRECEDING AND CURRENT ROW),
        w14  AS (PARTITION BY symbol ORDER BY date ROWS BETWEEN 13  PRECEDING AND CURRENT ROW)
)
INSERT INTO decision_tree_features_slice (
    date, close, next_return, is_gain5, is_drop5,
    return_1d, return_3d, return_5d, return_10d,
    rsi14, price_vs_sma20, price_vs_sma50, price_vs_sma200, sma20_vs_50,
    vol_ratio20, range_vs_atr14, close_near_high, consecutive_down
)
SELECT
    date,
    close,
    (next_close - close) / close * 100.0 AS next_return,
    CASE WHEN (next_close - close) / close * 100.0 >= 5.0 THEN 1 ELSE 0 END AS is_gain5,
    CASE WHEN (next_close - close) / close * 100.0 <= -5.0 THEN 1 ELSE 0 END AS is_drop5,
    (close - prev_close) / prev_close * 100.0 AS return_1d,
    (close - prev_close_3)  / prev_close_3  * 100.0 AS return_3d,
    (close - prev_close_5)  / prev_close_5  * 100.0 AS return_5d,
    (close - prev_close_10) / prev_close_10 * 100.0 AS return_10d,
    CASE
        WHEN avg_loss14 > 0 THEN 100.0 - (100.0 / (1.0 + avg_gain14 / avg_loss14))
        WHEN avg_gain14 > 0 THEN 100.0
        ELSE 50.0
    END AS rsi14,
    (close - sma20)  / sma20  * 100.0 AS price_vs_sma20,
    (close - sma50)  / sma50  * 100.0 AS price_vs_sma50,
    (close - sma200) / sma200 * 100.0 AS price_vs_sma200,
    (sma20 - sma50)  / sma50  * 100.0 AS sma20_vs_50,
    CASE WHEN vol_avg20 > 0 THEN volume / vol_avg20 ELSE 1.0 END AS vol_ratio20,
    CASE WHEN atr14 > 0 THEN (high - low) / atr14 ELSE 1.0 END AS range_vs_atr14,
    CASE WHEN (high - low) > 0 THEN (close - low) / (high - low) ELSE 0.5 END AS close_near_high,
    MIN(down_streak_uncapped, 10) AS consecutive_down
FROM windowed
WHERE rn >= 201 AND next_close IS NOT NULL;
