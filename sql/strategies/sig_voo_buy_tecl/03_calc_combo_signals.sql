-- LONG TECL when VOO down __DECLINE_DAYS__ days
INSERT INTO sig_voo_buy_tecl_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss)
SELECT
    coalesce(t.idx, t.rowid, 0) AS idx,
    'TECL' AS symbol,
    v.date,
    t.open,
    t.high,
    t.low,
    t.close,
    t.volume,
    t.close AS buylimit,
    1 AS entry,
    'LONG' AS direction,
    'All Regimes' AS regime,
    __HOLD_DAYS__ AS hold_days_override,
    t.close * __TAKE_PROFIT_MULT__ AS take_profit,
    t.close * __STOP_LOSS_MULT__ AS stop_loss
FROM voo_streaks_slice v
JOIN backtest_start t ON v.date = substr(t.Date, 1, 10) AND t.symbol = 'TECL' AND length(t.Date) = 10
WHERE v.down_streak >= __DECLINE_DAYS__;
