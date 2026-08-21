select min(b.Low), max(b.High) from backtest_start as b 
where b.Date > '2020-12-31'
and b.Date < '2022-01-01';

--min(b.Low)	max(b.High)
--539.489990234375	1243.48999023438
-- 130% increase if perfectly timed one buy and one sell.
-- 49% if bought at start of year and just held.Adj Close
