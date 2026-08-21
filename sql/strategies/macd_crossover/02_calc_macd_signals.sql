-- MACD Momentum Crossover Strategy in SQL
INSERT INTO macd_crossover_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry)
WITH ma_calc AS (
    SELECT 
        coalesce(idx, rowid, 0) AS idx,
        symbol,
        substr(Date, 1, 10) AS date,
        open,
        high,
        low,
        close,
        volume,
        AVG(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 11 PRECEDING AND CURRENT ROW) AS fast_ma,
        AVG(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 25 PRECEDING AND CURRENT ROW) AS slow_ma,
        COUNT(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 25 PRECEDING AND CURRENT ROW) AS count26
    FROM backtest_start
),
macd_diff AS (
    SELECT 
        idx,
        symbol,
        date,
        open,
        high,
        low,
        close,
        volume,
        (fast_ma - slow_ma) AS macd_line
    FROM ma_calc
    WHERE count26 >= 26
),
macd_series AS (
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
    FROM macd_diff
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
