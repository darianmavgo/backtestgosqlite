CREATE TABLE entry_dates as select symbol,  max (filled_at) as entry_date from `order` where symbol in (select distinct symbol from position) group by symbol ;
