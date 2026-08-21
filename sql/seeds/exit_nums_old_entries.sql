insert into exit_nums (symbol, exit_idx, entry_date)
select e.symbol, 1, e.entry_date from entry_dates as e
where e.entry_date < (select min(date) from trading_days);