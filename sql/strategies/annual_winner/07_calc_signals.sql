-- An entry on the entry date and an exit on the traded symbol's last session of
-- the year, unless that is the entry date. entry is 1 to open and -1 to close.
-- Exits sort before entries on the same date.
INSERT INTO annual_winner_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss, allocation_pct_override)
SELECT
    b.idx, b.symbol, b.date, b.open, b.high, b.low, b.close, b.volume, b.close,
    1,
    CASE WHEN '__SIDE__' = 'short' THEN 'SHORT' ELSE 'LONG' END,
    'All Regimes', 0, 0.0, 0.0, 0.0
FROM aw_trade t
JOIN aw_bar b ON b.symbol = t.trade_symbol AND b.date = t.entry_date
UNION ALL
SELECT
    b.idx, b.symbol, b.date, b.open, b.high, b.low, b.close, b.volume, b.close,
    -1,
    CASE WHEN '__SIDE__' = 'short' THEN 'SHORT' ELSE 'LONG' END,
    'All Regimes', 0, 0.0, 0.0, 0.0
FROM aw_trade t
JOIN aw_bar b ON b.symbol = t.trade_symbol AND b.year = CAST(t.year AS TEXT) AND b.rn_last = 1
WHERE b.date <> t.entry_date;
