-- For each year, the previous year's best return among symbols that also trade in
-- the year. Ties go to the lower symbol.
INSERT INTO aw_pick (year, symbol, ret)
SELECT year, symbol, ret
FROM (
    SELECT
        CAST(p.year AS INTEGER) + 1 AS year,
        p.symbol,
        p.ret,
        ROW_NUMBER() OVER (PARTITION BY p.year ORDER BY p.ret DESC, p.symbol) AS rk
    FROM aw_year p
    JOIN aw_year c ON c.symbol = p.symbol AND CAST(c.year AS INTEGER) = CAST(p.year AS INTEGER) + 1
    WHERE p.ret IS NOT NULL
)
WHERE rk = 1;
