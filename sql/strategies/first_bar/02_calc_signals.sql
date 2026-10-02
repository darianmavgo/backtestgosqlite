-- __TOTAL_RETURN__ is 1 when the run simulates on dividend-adjusted prices, so the
-- signal is priced on the same basis as the bars.
INSERT INTO first_bar_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry, direction, regime, hold_days_override, take_profit, stop_loss, allocation_pct_override)
SELECT
    coalesce(idx, rowid, 0),
    symbol,
    substr(Date, 1, 10),
    open * CASE WHEN __TOTAL_RETURN__ = 1 AND "Adj Close" > 0 AND close > 0 THEN "Adj Close" / close ELSE 1.0 END,
    high * CASE WHEN __TOTAL_RETURN__ = 1 AND "Adj Close" > 0 AND close > 0 THEN "Adj Close" / close ELSE 1.0 END,
    low * CASE WHEN __TOTAL_RETURN__ = 1 AND "Adj Close" > 0 AND close > 0 THEN "Adj Close" / close ELSE 1.0 END,
    close * CASE WHEN __TOTAL_RETURN__ = 1 AND "Adj Close" > 0 AND close > 0 THEN "Adj Close" / close ELSE 1.0 END,
    volume,
    close * CASE WHEN __TOTAL_RETURN__ = 1 AND "Adj Close" > 0 AND close > 0 THEN "Adj Close" / close ELSE 1.0 END,
    1,
    'LONG',
    'All Regimes',
    0,
    0.0,
    0.0,
    0.0
FROM backtest_start
WHERE symbol = '__TRADE_SYMBOL__' AND length(Date) = 10
  AND substr(Date, 1, 10) >= '__START_DATE__' AND substr(Date, 1, 10) <= '__END_DATE__'
ORDER BY Date
LIMIT 1;
