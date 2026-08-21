INSERT INTO wc_entry_slice  
select  day.idx, day.close > day2.low as buylimitmet
from backtest_start as day
join backtest_start as day2
on day.idx = day2.idx - 1 AND day.symbol = day2.symbol
join wc_buy_signal_slice 
on wc_buy_signal_slice.idx = day.idx
where wc_buy_signal_slice.buy_signal = 1;
