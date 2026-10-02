-- Window bars with their place in the symbol's year, from the first bar and from the last.
INSERT INTO aw_bar (idx, symbol, year, date, open, high, low, close, volume, rn_first, rn_last)
SELECT
    coalesce(idx, rowid, 0), symbol, substr(Date, 1, 4), substr(Date, 1, 10),
    open, high, low, close, volume,
    ROW_NUMBER() OVER (PARTITION BY symbol, substr(Date, 1, 4) ORDER BY Date),
    ROW_NUMBER() OVER (PARTITION BY symbol, substr(Date, 1, 4) ORDER BY Date DESC)
FROM backtest_start
WHERE length(Date) = 10 AND substr(Date, 1, 10) >= '__START_DATE__' AND substr(Date, 1, 10) <= '__END_DATE__';
