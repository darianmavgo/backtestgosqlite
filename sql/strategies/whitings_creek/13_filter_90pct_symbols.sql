-- filter_90pct_symbols.sql
-- Reads wc_summary (extended with win20_10d columns) and produces
-- backtested_win_20_10d: the actionable symbol list for the live scanner.
-- Only includes symbols where the WC signal has fired >=5 times historically
-- AND hit +20% within 10 days at least 90% of the time.
-- This table name is set in config.json as "SymbolTable".

DROP TABLE IF EXISTS backtested_win_20_10d;
CREATE TABLE backtested_win_20_10d AS
SELECT
    symbol,
    entries,
    wins_20_10d,
    win20_10d_rate,
    avg_max_gain_10d,
    avg_highgappct,
    max_highgappct
FROM wc_summary
WHERE entries        >= 5     -- minimum sample size for statistical validity
  AND win20_10d_rate >= 0.90  -- 90% confidence threshold
ORDER BY win20_10d_rate DESC, wins_20_10d DESC;
