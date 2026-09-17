-- Bollinger Band Capitulation + Reversal Bounce in SQL
INSERT INTO bb_capitulation_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry)
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
FROM bb_bands_slice
WHERE prev_lower_bb IS NOT NULL
  AND prev_low < prev_lower_bb
  AND close_5d_ago IS NOT NULL
  AND prev_close < close_5d_ago * 0.96
  AND close > prev_close;
