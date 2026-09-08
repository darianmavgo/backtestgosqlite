-- Calculate 5-day RSI
WITH rsi_gains_losses AS (
    SELECT
        idx,
        symbol,
        date,
        open,
        high,
        low,
        close,
        volume,
        CASE WHEN close > LAG(close, 1) OVER (PARTITION BY symbol ORDER BY date) THEN close - LAG(close, 1) OVER (PARTITION BY symbol ORDER BY date) ELSE 0 END AS gain,
        CASE WHEN close < LAG(close, 1) OVER (PARTITION BY symbol ORDER BY date) THEN LAG(close, 1) OVER (PARTITION BY symbol ORDER BY date) - close ELSE 0 END AS loss
    FROM bb_slice
),
rsi_avgs AS (
    SELECT
        *,
        AVG(gain) OVER (PARTITION BY symbol ORDER BY date ROWS BETWEEN 4 PRECEDING AND CURRENT ROW) as avg_gain,
        AVG(loss) OVER (PARTITION BY symbol ORDER BY date ROWS BETWEEN 4 PRECEDING AND CURRENT ROW) as avg_loss,
        COUNT(close) OVER (PARTITION BY symbol ORDER BY date ROWS BETWEEN 4 PRECEDING AND CURRENT ROW) as count5
    FROM rsi_gains_losses
)
INSERT INTO rsi5_slice (idx, symbol, date, open, high, low, close, volume, rsi5)
SELECT
    idx,
    symbol,
    date,
    open,
    high,
    low,
    close,
    volume,
    CASE
        WHEN count5 < 5 THEN NULL
        WHEN avg_loss = 0 THEN 100.0
        ELSE 100.0 - (100.0 / (1.0 + (avg_gain / avg_loss)))
    END AS rsi5
FROM rsi_avgs;
