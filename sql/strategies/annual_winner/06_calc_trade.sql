-- What is traded each year and on which session. Long and short trade the winner
-- itself on its first session of the year. Inverse trades the matched inverse ETF
-- on that same date, and a year with no pair, or no bar for it that day, is skipped.
INSERT INTO aw_trade (year, trade_symbol, entry_date)
SELECT p.year, p.symbol, w.first_date
FROM aw_pick p
JOIN aw_year w ON w.symbol = p.symbol AND w.year = CAST(p.year AS TEXT)
WHERE '__SIDE__' <> 'inverse'
UNION ALL
SELECT p.year, i.inverse, w.first_date
FROM aw_pick p
JOIN aw_year w ON w.symbol = p.symbol AND w.year = CAST(p.year AS TEXT)
JOIN aw_inverse i ON i.symbol = p.symbol
JOIN aw_bar b ON b.symbol = i.inverse AND b.date = w.first_date
WHERE '__SIDE__' = 'inverse';
