INSERT INTO millwharf_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry)
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
FROM qualifying_slice
WHERE weekly_rank = 1
ORDER BY date, symbol ASC;
