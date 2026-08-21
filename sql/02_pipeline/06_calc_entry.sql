INSERT INTO entry
select b.symbol,
	b.date,
	b.close as buy_limit,
	b.close * 1.05 as sell_limit,
	b.volume,
	s.buy_signal as `found`,
	cast(2000 / b.close as int) as `qty`,
	m.minlow as `min_low`,
	b.close as `close`,
	'0.9' as `cliff_pct`
from backtest_start b
	join wc_trailing_minlow_slice m on b.idx = m.idx
	join wc_buy_signal_slice s on s.idx = b.idx
	join (
		select max(date) as dtoday
		from backtest_start
	) as m
where s.buy_signal = 1
	and m.dtoday = b.date;