-- calc_win20_10d.sql
-- Mirrors calc_win_slice.sql for a 20% gain target over 10 trading days.
-- Win20_10d = 1 means the stock hit +20% above signal-day close within 10 days.
-- max_gain_10d = the actual best gain achievable in that window (useful for ranking).

INSERT INTO win20_10d_slice
SELECT b.idx,
    (h.maxhigh10 > (b.Close * 1.20)) AS Win20_10d,
    round((h.maxhigh10 - b.Close) / b.Close, 4) AS max_gain_10d
FROM backtest_start AS b
JOIN maxhigh10_slice AS h ON h.idx = b.idx;
