INSERT INTO qualifying_slice (idx, symbol, date, week_id, open, high, low, close, volume, high6d, streak, drop_pct, take_profit, weekly_rank)
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
FROM streaks_slice
WHERE streak >= 5;
