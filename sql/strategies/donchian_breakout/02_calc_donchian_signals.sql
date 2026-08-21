-- 20-Day Donchian Channel Breakout Strategy in pure SQL
INSERT INTO donchian_breakout_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry)
WITH donchian_calc AS (
    SELECT 
        coalesce(idx, rowid, 0) AS idx,
        symbol,
        substr(Date, 1, 10) AS date,
        open,
        high,
        low,
        close,
        volume,
        MAX(high) OVER (
            PARTITION BY symbol 
            ORDER BY Date 
            ROWS BETWEEN 20 PRECEDING AND 1 PRECEDING
        ) AS upper_20d
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
FROM donchian_calc
WHERE upper_20d IS NOT NULL AND close > upper_20d;
