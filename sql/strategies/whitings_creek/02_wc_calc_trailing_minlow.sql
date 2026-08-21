-- select the window of lows from (day - 3) to (day - 12)
insert into wc_trailing_minlow_slice 
select b.idx,
	min(
		b3.low,
		b4.low,
		b5.low,
		b6.low,
		b7.low,
		b8.low,
		b9.low,
		b10.low,
		b11.low,
		b12.low
	) as minlow
from backtest_start b
	join backtest_start b3 on b3.idx = b.idx - 3 and b3.symbol = b.symbol
	join backtest_start b4 on b4.idx = b.idx - 4 and b4.symbol = b.symbol
	join backtest_start b5 on b5.idx = b.idx - 5 and b5.symbol = b.symbol
	join backtest_start b6 on b6.idx = b.idx - 6 and b6.symbol = b.symbol
	join backtest_start b7 on b7.idx = b.idx - 7 and b7.symbol = b.symbol
	join backtest_start b8 on b8.idx = b.idx - 8 and b8.symbol = b.symbol
	join backtest_start b9 on b9.idx = b.idx - 9 and b9.symbol = b.symbol
	join backtest_start b10 on b10.idx = b.idx - 10 and b10.symbol = b.symbol
	join backtest_start b11 on b11.idx = b.idx - 11 and b11.symbol = b.symbol
	join backtest_start b12 on b12.idx = b.idx - 12 and b12.symbol = b.symbol
group by b.idx;
