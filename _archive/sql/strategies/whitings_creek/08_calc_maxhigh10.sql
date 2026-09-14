-- calc_maxhigh10.sql
-- Mirror of calc_maxhigh4.sql extended to 10 trading days forward.
-- For each row, finds the highest high price in the following 10 trading days.
-- Uses per-symbol idx+N joins (same pattern as calc_maxhigh4) to stay
-- within the same symbol's data. idx is global row order within backtest_start.
-- NOTE: d1 = Day 1 after signal (next day open), d10 = Day 10 close day.

INSERT INTO maxhigh10_slice
SELECT d.idx,
    max(d1.high, d2.high, d3.high, d4.high, d5.high,
        d6.high, d7.high, d8.high, d9.high, d10.high) AS maxhigh10
FROM backtest_start AS d
JOIN backtest_start AS d1  ON d1.idx  = d.idx + 1  AND d1.symbol  = d.symbol
JOIN backtest_start AS d2  ON d2.idx  = d.idx + 2  AND d2.symbol  = d.symbol
JOIN backtest_start AS d3  ON d3.idx  = d.idx + 3  AND d3.symbol  = d.symbol
JOIN backtest_start AS d4  ON d4.idx  = d.idx + 4  AND d4.symbol  = d.symbol
JOIN backtest_start AS d5  ON d5.idx  = d.idx + 5  AND d5.symbol  = d.symbol
JOIN backtest_start AS d6  ON d6.idx  = d.idx + 6  AND d6.symbol  = d.symbol
JOIN backtest_start AS d7  ON d7.idx  = d.idx + 7  AND d7.symbol  = d.symbol
JOIN backtest_start AS d8  ON d8.idx  = d.idx + 8  AND d8.symbol  = d.symbol
JOIN backtest_start AS d9  ON d9.idx  = d.idx + 9  AND d9.symbol  = d.symbol
JOIN backtest_start AS d10 ON d10.idx = d.idx + 10 AND d10.symbol = d.symbol
GROUP BY d.idx;
