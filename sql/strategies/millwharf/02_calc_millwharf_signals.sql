-- Millwharf Weekly Consistent Decline Strategy in SQLite SQL
-- Rules:
-- 1. Scan for consecutive declining closes with streak >= 5.
-- 2. Take profit = Min(High of last 6 days, close * 1.20).
-- 3. Every week, select the stock with the longest decline streak.

INSERT INTO millwharf_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry)
WITH ranked AS (
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
    MAX(high) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 5 PRECEDING AND CURRENT ROW) AS high6d
  FROM backtest_start
),
streaks_calc AS (
  SELECT 
    *,
    CASE WHEN close < prev_close THEN 0 ELSE 1 END AS reset_flag
  FROM ranked
),
groups AS (
  SELECT 
    *,
    SUM(reset_flag) OVER (PARTITION BY symbol ORDER BY date) AS grp
  FROM streaks_calc
),
streak_lengths AS (
  SELECT 
    *,
    COUNT(*) OVER (PARTITION BY symbol, grp ORDER BY date) AS streak,
    FIRST_VALUE(prev_close) OVER (PARTITION BY symbol, grp ORDER BY date) AS peak_close
  FROM groups
  WHERE reset_flag = 0
),
qualifying AS (
  SELECT 
    idx,
    symbol,
    date,
    strftime('%Y-%W', date) AS week_id,
    open,
    high,
    low,
    close,
    volume,
    high6d,
    streak,
    (close - peak_close) / peak_close * 100.0 AS drop_pct,
    CASE WHEN high6d < close * 1.20 THEN high6d ELSE close * 1.20 END AS take_profit,
    ROW_NUMBER() OVER (
      PARTITION BY strftime('%Y-%W', date) 
      ORDER BY streak DESC, (close - peak_close) / peak_close ASC, date ASC
    ) AS weekly_rank
  FROM streak_lengths
  WHERE streak >= 5
)
SELECT 
  idx,
  symbol,
  date,
  open,
  high,
  low,
  close,
  volume,
  close AS buylimit,
  1 AS entry
FROM qualifying
WHERE weekly_rank = 1
ORDER BY date, symbol ASC;
