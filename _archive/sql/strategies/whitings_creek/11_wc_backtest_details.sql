-- wc_backtest_details.sql
-- Extended from original: adds maxhigh10, Win20_10d, max_gain_10d columns
-- by joining win20_10d_slice and maxhigh10_slice (both computed earlier in pipeline).

INSERT INTO wc_backtest_details
SELECT b.idx,
    b.symbol,
    substr(b.Date, 1, 10) AS date,
    round(b.Open, 2)      AS open,
    round(b.High, 2)      AS high,
    round(b.Low, 2)       AS low,
    round(b.Close, 2)     AS close,
    b.volume,
    round(b.Close, 2)     AS buylimit,
    bl.buylimitmet        AS entry,
    round(low.minlow, 2)  AS minlow4,
    round(high.maxhigh, 2) AS maxhigh4,
    round(win.Win5, 2)    AS Win5,
    round(win.Win3, 2)    AS Win3,
    (b.Close - low.minlow)          AS lowgap,
    (high.maxhigh - b.Close)        AS highgap,
    (b.Close - low.minlow) / b.Close  AS lowgappct,
    (high.maxhigh - b.Close) / b.Close AS highgappct,
    -- NEW 10-day columns
    round(h10.maxhigh10, 2)           AS maxhigh10,
    round(w20.Win20_10d, 2)           AS Win20_10d,
    round(w20.max_gain_10d, 4)        AS max_gain_10d
FROM backtest_start b
    LEFT JOIN wc_entry_slice   AS bl  ON bl.idx  = b.idx
    JOIN minlow4_slice         AS low ON low.idx  = b.idx
    JOIN maxhigh4_slice        AS high ON high.idx = b.idx
    JOIN win_slice             AS win ON win.idx  = b.idx
    -- NEW joins
    JOIN maxhigh10_slice       AS h10 ON h10.idx  = b.idx
    JOIN win20_10d_slice       AS w20 ON w20.idx  = b.idx;
