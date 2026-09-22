-- BUY TQQQ on every date QQQ closed up on higher volume than the prior session.
INSERT INTO sig_qqq_up1_buy_tqqq_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss)
SELECT
    coalesce(t.idx, t.rowid, 0) AS idx,
    'TQQQ' AS symbol,
    d.date,
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
FROM sig_qqq_up1_buy_tqqq_dates_slice d
JOIN backtest_start t ON d.date = substr(t.Date, 1, 10) AND t.symbol = 'TQQQ' AND length(t.Date) = 10
WHERE d.up_and_vol_up = 1 AND t.close > 0;
