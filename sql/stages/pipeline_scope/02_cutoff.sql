-- The date 12 months before the last daily bar of the symbols.
-- __SYMBOL_LIST__ is the quoted, upper-case, comma-separated symbol list.
SELECT date(max(substr(Date, 1, 10)), '-12 months')
FROM backtest_start
WHERE symbol IN (__SYMBOL_LIST__) AND length(Date) = 10
