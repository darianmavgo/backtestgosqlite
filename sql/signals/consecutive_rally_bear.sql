-- consecutive_rally_bear.sql
-- Detects dates where the signal symbol has 3 consecutive UP closes
-- AND the current close is BELOW its 200-day simple moving average (bear regime).
-- This captures dead-cat bounce rallies in a downtrend.

SELECT date AS signal_date FROM (
    SELECT 
        substr(Date, 1, 10) AS date,
        close,
        LAG(close, 1) OVER w AS prev1,
        LAG(close, 2) OVER w AS prev2,
        LAG(close, 3) OVER w AS prev3,
        AVG(close) OVER (ORDER BY substr(Date, 1, 10) ROWS BETWEEN 199 PRECEDING AND CURRENT ROW) AS sma200
    FROM backtest_start
    WHERE symbol = :signal_symbol
    WINDOW w AS (ORDER BY substr(Date, 1, 10))
)
WHERE close > prev1 AND prev1 > prev2 AND prev2 > prev3
  AND close < sma200
ORDER BY date;
