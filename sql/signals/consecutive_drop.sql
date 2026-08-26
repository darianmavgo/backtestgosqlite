-- voo_3day_drop.sql
-- Detects dates where VOO closed down 3 consecutive days.
-- Returns: signal_date column with dates where a 3-day dip entry is triggered.
-- Usage: Parameterized — replace 'VOO' and the streak count as needed.

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
WHERE close < prev1 AND prev1 < prev2 AND prev2 < prev3
ORDER BY date;
