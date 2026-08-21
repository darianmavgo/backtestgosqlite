-- Connors RSI(2) Trend Pullback Strategy in SQL
INSERT INTO rsi2_trend_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry)
WITH trend_calc AS (
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
        LAG(close, 2) OVER (PARTITION BY symbol ORDER BY Date) AS prev2_close,
        AVG(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 49 PRECEDING AND CURRENT ROW) AS sma50,
        COUNT(close) OVER (PARTITION BY symbol ORDER BY Date ROWS BETWEEN 49 PRECEDING AND CURRENT ROW) AS count50
    FROM backtest_start
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
FROM trend_calc
WHERE count50 >= 50
  AND close > sma50
  AND prev_close IS NOT NULL 
  AND prev2_close IS NOT NULL
  AND close < prev_close 
  AND prev_close < prev2_close
  AND close < prev2_close * 0.96;
