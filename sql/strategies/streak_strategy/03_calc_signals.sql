-- Buy the trade symbol when the watch symbol's streak is long enough.
-- __STREAK_COL__ is down_streak or up_streak. __REGIME_PREDICATE__ is
-- applied to the watch bar (alias v), not the trade bar.
INSERT INTO streak_strategy_signals (
    idx, symbol, date, open, high, low, close, volume, buylimit, entry,
    direction, regime, hold_days_override, take_profit, stop_loss
)
SELECT
    coalesce(t.idx, t.rowid, 0) AS idx,
    '__TRADE_SYMBOL__' AS symbol,
    v.date,
    t.open,
    t.high,
    t.low,
    t.close,
    t.volume,
    t.close AS buylimit,
    1 AS entry,
    'LONG' AS direction,
    '__REGIME_LABEL__' AS regime,
    __HOLD_DAYS__ AS hold_days_override,
    t.close * __TAKE_PROFIT_MULT__ AS take_profit,
    t.close * __STOP_LOSS_MULT__ AS stop_loss
FROM streak_strategy_slice v
JOIN backtest_start t
  ON v.date = substr(t.Date, 1, 10)
 AND t.symbol = '__TRADE_SYMBOL__'
 AND length(t.Date) = 10
WHERE v.__STREAK_COL__ >= __DECLINE_DAYS__
  AND (__REGIME_PREDICATE__)
  AND t.close > 0;
