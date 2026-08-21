CREATE TABLE time_up AS SELECT e.symbol, e.entry_date, t.date as exit_date, t.date <= date('now') as time_up
FROM exit_nums as e JOIN trading_days as t ON e.exit_idx = t.rowid;