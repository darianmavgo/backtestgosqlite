-- exit_nums.sql
-- Extended from original: uses MaxHoldDays (10 trading days) instead of 2.
-- exit_idx = rowid + MaxHoldDays + 1 because:
--   rowid is the entry day's position in trading_days
--   +1 skips entry day itself, then +10 counts the 10 holding days forward.
-- Note: the original used rowid+2 for a 3-day hold (TradingDays=3).
-- Now TradingDays=10 in config.json, so offset becomes rowid+11.

CREATE TABLE exit_nums AS
SELECT e.symbol,
       t.rowid + 11 AS exit_idx,   -- entry rowid + 1 (skip entry day) + 10 (trading days held)
       e.entry_date
FROM entry_dates AS e
JOIN trading_days AS t
WHERE t.date = date(e.entry_date);
