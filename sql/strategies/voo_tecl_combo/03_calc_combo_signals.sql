-- LONG TECL when VOO down 3 days
INSERT INTO voo_tecl_combo_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss)
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
    8 AS hold_days_override,
    t.close * 1.05 AS take_profit,
    0.0 AS stop_loss
FROM voo_streaks_slice v
JOIN backtest_start t ON v.date = substr(t.Date, 1, 10) AND t.symbol = 'TECL'
WHERE v.down_streak >= 3;

-- SHORT SPXU when VOO up 3 days AND VOO < SMA200
-- Also ensure we don't insert a SHORT if a LONG was already inserted for that date (LONG takes priority)
INSERT INTO voo_tecl_combo_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss)
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
    2 AS hold_days_override,
    s.close * 1.06 AS take_profit,
    s.close * 0.95 AS stop_loss
FROM voo_streaks_slice v
JOIN backtest_start s ON v.date = substr(s.Date, 1, 10) AND s.symbol = 'SPXU'
WHERE v.up_streak >= 3
  AND (v.sma200 <= 0 OR v.close < v.sma200)
  AND NOT EXISTS (
      SELECT 1 FROM voo_tecl_combo_signals prev
      WHERE prev.date = v.date AND prev.direction = 'LONG'
  );
