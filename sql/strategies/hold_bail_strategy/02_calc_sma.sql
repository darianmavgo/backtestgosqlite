-- The average is taken over the bars of the window only, so it is empty for the
-- first period bars: the entry rule below ignores them.
INSERT INTO hold_bail_sma (idx, symbol, date, open, high, low, close, volume, rn, sma)
SELECT
    coalesce(idx, rowid, 0), symbol, substr(Date, 1, 10),
    open, high, low, close, volume,
    ROW_NUMBER() OVER w,
    AVG(close) OVER (ORDER BY Date ROWS BETWEEN __SMA_PRECEDING__ PRECEDING AND CURRENT ROW)
FROM backtest_start
WHERE symbol = '__TRADE_SYMBOL__' AND length(Date) = 10
  AND substr(Date, 1, 10) >= '__START_DATE__' AND substr(Date, 1, 10) <= '__END_DATE__'
WINDOW w AS (ORDER BY Date);
