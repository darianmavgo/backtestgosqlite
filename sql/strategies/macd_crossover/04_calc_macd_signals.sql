-- Calculate MACD Signal Line and Crossover
WITH macd_series AS (
    SELECT 
        idx,
        symbol,
        date,
        open,
        high,
        low,
        close,
        volume,
        macd_line,
        AVG(macd_line) OVER (PARTITION BY symbol ORDER BY date ROWS BETWEEN 8 PRECEDING AND CURRENT ROW) AS signal_line,
        LAG(macd_line, 1) OVER (PARTITION BY symbol ORDER BY date) AS prev_macd
    FROM macd_slice
),
macd_cross AS (
    SELECT
        idx,
        symbol,
        date,
        open,
        high,
        low,
        close,
        volume,
        macd_line,
        signal_line,
        prev_macd,
        LAG(signal_line, 1) OVER (PARTITION BY symbol ORDER BY date) AS prev_signal
    FROM macd_series
)
INSERT INTO macd_crossover_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry)
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
FROM macd_cross
WHERE prev_macd IS NOT NULL 
  AND prev_signal IS NOT NULL
  AND prev_macd <= prev_signal
  AND macd_line > signal_line
  AND macd_line < 0;
