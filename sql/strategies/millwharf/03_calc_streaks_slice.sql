WITH streaks_calc AS (
  SELECT
    *,
    CASE WHEN close < prev_close THEN 0 ELSE 1 END AS reset_flag
  FROM ranked_slice
),
groups AS (
  SELECT
    *,
    SUM(reset_flag) OVER (PARTITION BY symbol ORDER BY date) AS grp
  FROM streaks_calc
)
INSERT INTO streaks_slice (idx, symbol, date, open, high, low, close, volume, prev_close, high6d, reset_flag, grp, streak, peak_close)
SELECT
    idx, symbol, date, open, high, low, close, volume, prev_close, high6d, reset_flag, grp,
    COUNT(*) OVER (PARTITION BY symbol, grp ORDER BY date) AS streak,
    FIRST_VALUE(prev_close) OVER (PARTITION BY symbol, grp ORDER BY date) AS peak_close
FROM groups
WHERE reset_flag = 0;
