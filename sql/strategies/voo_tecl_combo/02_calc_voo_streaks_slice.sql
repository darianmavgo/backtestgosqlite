-- Calculate VOO Down and Up Streaks and SMA200
WITH diffs AS (
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
        AVG(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 199 PRECEDING AND CURRENT ROW) AS sma200,
        COUNT(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 199 PRECEDING AND CURRENT ROW) AS count200
    FROM backtest_start
    WHERE symbol = 'VOO'
),
streaks AS (
    SELECT
        *,
        CASE WHEN close < prev_close THEN 1 ELSE 0 END AS is_down,
        CASE WHEN close > prev_close THEN 1 ELSE 0 END AS is_up
    FROM diffs
),
streak_groups AS (
    SELECT
        *,
        CASE WHEN is_down = 1 THEN
             SUM(CASE WHEN is_down = 0 THEN 1 ELSE 0 END) OVER (PARTITION BY symbol ORDER BY date)
        ELSE 0 END AS down_grp,
        CASE WHEN is_up = 1 THEN
             SUM(CASE WHEN is_up = 0 THEN 1 ELSE 0 END) OVER (PARTITION BY symbol ORDER BY date)
        ELSE 0 END AS up_grp
    FROM streaks
)
INSERT INTO voo_streaks_slice (idx, symbol, date, open, high, low, close, volume, sma200, down_streak, up_streak)
SELECT
    idx,
    symbol,
    date,
    open,
    high,
    low,
    close,
    volume,
    CASE WHEN count200 >= 200 THEN sma200 ELSE 0 END AS sma200,
    CASE WHEN is_down = 1 THEN COUNT(*) OVER (PARTITION BY symbol, down_grp ORDER BY date) ELSE 0 END AS down_streak,
    CASE WHEN is_up = 1 THEN COUNT(*) OVER (PARTITION BY symbol, up_grp ORDER BY date) ELSE 0 END AS up_streak
FROM streak_groups;
