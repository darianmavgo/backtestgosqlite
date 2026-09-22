-- VOO closed up from the prior close AND traded more volume than the prior
-- session -- matches UpVolumeUpDates() in pkg/strategy/sig_voo_up1_buy_tqqq.go
-- exactly, including its zero-volume guard on both sides of the comparison.
WITH diffs AS (
    SELECT
        substr(Date, 1, 10) AS date,
        close,
        volume,
        LAG(close, 1)  OVER (PARTITION BY symbol ORDER BY Date) AS prev_close,
        LAG(volume, 1) OVER (PARTITION BY symbol ORDER BY Date) AS prev_volume
    FROM backtest_start
    WHERE symbol = 'VOO' AND length(Date) = 10
)
INSERT INTO sig_voo_up1_buy_tqqq_dates_slice (date, up_and_vol_up)
SELECT
    date,
    1 AS up_and_vol_up
FROM diffs
WHERE prev_close IS NOT NULL
  AND close > prev_close
  AND prev_volume > 0
  AND volume > prev_volume;
