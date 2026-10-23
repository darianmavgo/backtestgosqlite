-- One entry per qualifying session, dated the session whose close the regime is known at. The
-- simulator buys at the next session open (StrategyConfig.NextDayOpenEntry).
--   calm_filter: the stressed probability is under the threshold.
--   calm_down:   it is under the threshold and the session before it was at or over it.
-- CROSS JOIN pins the loop order: signal rows outside, one index lookup per row in the market bars.
INSERT INTO hmm_strategy_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss)
SELECT
    coalesce(t.idx, t.rowid, 0) AS idx,
    '__TRADE_SYMBOL__' AS symbol,
    l.Date,
    t.open,
    t.high,
    t.low,
    t.close,
    t.volume,
    t.close AS buylimit,
    1 AS entry,
    '__DIRECTION__' AS direction,
    '__RULE__' AS regime,
    __HOLD_DAYS__ AS hold_days_override,
    t.close * __TAKE_PROFIT_MULT__ AS take_profit,
    t.close * __STOP_LOSS_MULT__ AS stop_loss
FROM hmm_stress_lag l
CROSS JOIN market.backtest_start t ON t.Date = l.Date AND t.symbol = '__TRADE_SYMBOL__' AND length(t.Date) = 10
WHERE l.stress_prob < __STRESS_PROB__
  AND ('__RULE__' = 'calm_filter' OR l.prev_stress_prob >= __STRESS_PROB__);
