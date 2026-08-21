-- make sure d.high is 1 day after buy day.
insert into minlow4_slice 
select d.idx, min(d1.low, d2.low, d3.low, d4.low) as minlow
from backtest_start as d
join backtest_start as d1
on d1.idx = d.idx+1 AND d1.symbol = d.symbol
join backtest_start as d2
on d2.idx = d.idx+2 AND d2.symbol = d.symbol
join backtest_start as d3
on d3.idx = d.idx+3 AND d3.symbol = d.symbol
join backtest_start as d4
on d4.idx = d.idx+4 AND d4.symbol = d.symbol
group by d.idx;