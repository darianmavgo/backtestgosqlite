-- Connors RSI(2) Trend Pullback Strategy in SQL
INSERT INTO rsi2_trend_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry)
SELECT
    idx,
    symbol,
    date,
    open,
    high,
    low,
    close,
    volume,
    close AS buylimit,
    1 AS entry
FROM trend_calc_slice
WHERE count50 >= 50
  AND close > sma50
  AND prev_close IS NOT NULL
  AND prev2_close IS NOT NULL
  AND close < prev_close
  AND prev_close < prev2_close
  AND close < prev2_close * 0.96;
