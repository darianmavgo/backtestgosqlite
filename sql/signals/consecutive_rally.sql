-- consecutive_rally.sql
-- Detects dates where the signal symbol closed UP N consecutive days.
-- Returns: signal_date column with dates where a consecutive rally is detected.

SELECT date AS signal_date FROM (
    SELECT 
        substr(Date, 1, 10) AS date,
        close,
        LAG(close, 1) OVER w AS prev1,
        LAG(close, 2) OVER w AS prev2,
        LAG(close, 3) OVER w AS prev3
    FROM backtest_start
    WHERE symbol = :signal_symbol
    WINDOW w AS (ORDER BY substr(Date, 1, 10))
)
WHERE close > prev1 AND prev1 > prev2 AND prev2 > prev3
ORDER BY date;
