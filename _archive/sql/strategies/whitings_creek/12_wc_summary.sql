-- wc_summary.sql
-- Extended from original: adds win20_10d_rate and avg_max_gain_10d columns.
-- This summary drives filter_90pct_symbols.sql which produces backtested_win_20_10d.

DROP TABLE IF EXISTS wc_summary;
CREATE TABLE wc_summary AS
SELECT
    symbol,
    count(entry)           AS entries,
    sum(win3)              AS sum_win3,
    sum(win5)              AS sum_win5,
    avg(highgappct)        AS avg_highgappct,
    max(highgappct)        AS max_highgappct,
    avg(lowgappct)         AS avg_lowgappct,
    min(lowgappct)         AS min_lowgappct,
    -- NEW 10-day win columns
    sum(Win20_10d)         AS wins_20_10d,
    ROUND(
        CAST(sum(Win20_10d) AS REAL) / NULLIF(count(entry), 0),
        4
    )                      AS win20_10d_rate,
    avg(max_gain_10d)      AS avg_max_gain_10d,
    max(max_gain_10d)      AS max_max_gain_10d
FROM wc_backtest_details
WHERE entry = 1
GROUP BY symbol;