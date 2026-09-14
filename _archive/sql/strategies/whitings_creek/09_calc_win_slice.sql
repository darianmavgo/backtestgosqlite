insert into win_slice  
select b.idx, (h.maxhigh > (b.Close * 1.05)) as Win5, (h.maxhigh > (b.Close * 1.03)) as Win3
from  backtest_start as b 
join maxhigh4_slice as h
on h.idx = b.idx;
