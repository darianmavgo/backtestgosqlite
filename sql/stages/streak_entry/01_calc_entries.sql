-- One (trade symbol, streak length, regime) combination of a streak grid
-- search: every watch-symbol date whose streak is long enough and whose regime
-- holds, with the trade symbol's bar on that date. Take-profit, stop and hold
-- days depend on the grid point, not the entry, so they are applied later.
-- __STREAK_COL__ is down_streak or up_streak, and __REGIME_PREDICATE__ tests the
-- watch bar (alias v).
INSERT INTO streak_entries (trade_symbol, signal_days, regime, date, open, high, low, close, volume)
SELECT
    '__TRADE_SYMBOL__', __SIGNAL_DAYS__, '__REGIME_LABEL__', v.date,
    t.open, t.high, t.low, t.close, t.volume
FROM streak_slice v
JOIN backtest_start t
  ON substr(t.Date, 1, 10) = v.date AND t.symbol = '__TRADE_SYMBOL__' AND length(t.Date) = 10
WHERE v.__STREAK_COL__ >= __SIGNAL_DAYS__
  AND (__REGIME_PREDICATE__)
  AND t.close > 0;
