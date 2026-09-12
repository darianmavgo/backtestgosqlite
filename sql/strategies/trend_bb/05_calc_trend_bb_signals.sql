-- Trend-Gated Bollinger Oversold Signals
INSERT INTO trend_bb_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry)
SELECT
    s.idx,
    s.symbol,
    s.date,
    s.open,
    s.high,
    s.low,
    s.close,
    s.volume,
    s.close AS buylimit,
    1 AS entry
FROM sma50_slice s
JOIN bb_slice b ON s.idx = b.idx
JOIN rsi5_slice r ON s.idx = r.idx
WHERE s.count50 >= 50
  AND s.close > s.sma50
  AND b.count20 >= 20
  AND b.is_lower_bb_broken = 1
  AND r.rsi5 IS NOT NULL
  AND r.rsi5 < 30;
