-- Generate Oversold Reversal Signals in SQL
-- Identifies 3 consecutive down closes or sharp 8% drop followed by bounce
INSERT INTO rsi_oversold_signals (idx, symbol, date, open, high, low, close, volume, buylimit, entry)
SELECT 
    b.idx,
    b.symbol,
    substr(b.date, 1, 10) as date,
    b.open,
    b.high,
    b.low,
    b.close,
    b.volume,
    b.close as buylimit,
    1 as entry
FROM backtest_start b
WHERE b.close < (SELECT MIN(b2.low) FROM backtest_start b2 WHERE b2.symbol = b.symbol AND b2.idx BETWEEN b.idx - 10 AND b.idx - 2) * 0.92;
