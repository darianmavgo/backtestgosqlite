-- A symbol's return for a calendar year: last close against first open.
INSERT INTO aw_year (symbol, year, first_date, first_open, last_date, last_close, ret)
SELECT
    f.symbol, f.year, f.date, f.open, l.date, l.close,
    (l.close - f.open) / f.open
FROM aw_bar f
JOIN aw_bar l ON l.symbol = f.symbol AND l.year = f.year AND l.rn_last = 1
WHERE f.rn_first = 1;
