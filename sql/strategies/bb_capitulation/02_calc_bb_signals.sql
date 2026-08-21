-- Bollinger Band Capitulation + Reversal Bounce in SQL
INSERT INTO bb_capitulation_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry)
WITH stats AS (
    SELECT 
        coalesce(idx, rowid, 0) AS idx,
        symbol,
        substr(Date, 1, 10) AS date,
        open,
        high,
        low,
        close,
        volume,
        LAG(close, 1) OVER (PARTITION BY symbol ORDER BY Date) AS prev_close,
        LAG(low, 1) OVER (PARTITION BY symbol ORDER BY Date) AS prev_low,
        LAG(close, 5) OVER (PARTITION BY symbol ORDER BY Date) AS close_5d_ago,
        AVG(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 19 PRECEDING AND CURRENT ROW) AS sma20,
        AVG(close * close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 19 PRECEDING AND CURRENT ROW) AS sma20_sq,
        COUNT(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 19 PRECEDING AND CURRENT ROW) AS count20
    FROM backtest_start
),
bb AS (
    SELECT 
        *,
        CASE 
            WHEN (sma20_sq - sma20 * sma20) > 0 THEN sma20 - (2.0 * (sma20_sq - sma20 * sma20) / (sma20 * 0.05 + 1.0))
            ELSE sma20 * 0.95
        END AS lower_bb,
        LAG(
            CASE 
                WHEN (sma20_sq - sma20 * sma20) > 0 THEN sma20 - (2.0 * (sma20_sq - sma20 * sma20) / (sma20 * 0.05 + 1.0))
                ELSE sma20 * 0.95
            END, 1
        ) OVER (PARTITION BY symbol ORDER BY date) AS prev_lower_bb
    FROM stats
    WHERE count20 >= 20
)
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
FROM bb
WHERE prev_lower_bb IS NOT NULL
  AND prev_low < prev_lower_bb
  AND close_5d_ago IS NOT NULL
  AND prev_close < close_5d_ago * 0.96
  AND close > prev_close;
