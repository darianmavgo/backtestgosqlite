
insert into "wc_buy_signal_slice"
select b.idx, (b.Close < 0.9*w.minlow) as buy_signal 
from  backtest_start b 
join wc_trailing_minlow_slice w on b.idx = w.idx ;

