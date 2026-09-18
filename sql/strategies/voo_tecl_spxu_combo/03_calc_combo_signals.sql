-- LONG TECL when VOO down __DECLINE_DAYS__ days AND TECL down __DECLINE_DAYS__ days
INSERT INTO voo_tecl_spxu_combo_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss)
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
FROM voo_tecl_streaks_slice v
JOIN voo_tecl_streaks_slice t ON v.date = t.date AND t.symbol = 'TECL'
WHERE v.symbol = 'VOO'
  AND v.down_streak >= __DECLINE_DAYS__
  AND t.down_streak >= __DECLINE_DAYS__;

-- SHORT SPXU when VOO up __DECLINE_DAYS__ days AND VOO < SMA200
-- Also ensure we don't insert a SHORT if a LONG was already inserted for that date (LONG takes priority)
INSERT INTO voo_tecl_spxu_combo_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss)
SELECT
    coalesce(s.idx, s.rowid, 0) AS idx,
    'SPXU' AS symbol,
    v.date,
    s.open,
    s.high,
    s.low,
    s.close,
    s.volume,
    s.close AS buylimit,
    1 AS entry,
    'SHORT' AS direction,
    CASE WHEN v.sma200 > 0 THEN 'VOO<SMA200' ELSE 'All Regimes' END AS regime,
    __SHORT_HOLD_DAYS__ AS hold_days_override,
    s.close * __SHORT_TAKE_PROFIT_MULT__ AS take_profit,
    s.close * __SHORT_STOP_LOSS_MULT__ AS stop_loss
FROM voo_tecl_streaks_slice v
JOIN backtest_start s ON v.date = substr(s.Date, 1, 10) AND s.symbol = 'SPXU' AND length(s.Date) = 10
WHERE v.symbol = 'VOO'
  AND v.up_streak >= __DECLINE_DAYS__
  AND (v.sma200 <= 0 OR v.close < v.sma200)
  AND NOT EXISTS (
      SELECT 1 FROM voo_tecl_spxu_combo_signals prev
      WHERE prev.date = v.date AND prev.direction = 'LONG'
  );
