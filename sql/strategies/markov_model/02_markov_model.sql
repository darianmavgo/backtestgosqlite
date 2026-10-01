-- Phase 1: Regime Definition
CREATE TEMP VIEW IF NOT EXISTS markov_model_regimes AS
SELECT 
    symbol,
    Date, 
    close,
    (close - LAG(close, 20) OVER (PARTITION BY symbol ORDER BY Date)) / LAG(close, 20) OVER (PARTITION BY symbol ORDER BY Date) as ret_20d,
    CASE 
        WHEN ((close - LAG(close, 20) OVER (PARTITION BY symbol ORDER BY Date)) / LAG(close, 20) OVER (PARTITION BY symbol ORDER BY Date)) >= 0.05 THEN 1
        WHEN ((close - LAG(close, 20) OVER (PARTITION BY symbol ORDER BY Date)) / LAG(close, 20) OVER (PARTITION BY symbol ORDER BY Date)) <= -0.05 THEN -1
        ELSE 0 
    END as regime
FROM market.backtest_start 
WHERE timeframe = '1d';

-- Phase 2: Transition Matrix
CREATE TABLE IF NOT EXISTS markov_model_transition_matrix AS
WITH state_pairs AS (
    SELECT 
        symbol,
        regime as from_state,
        LEAD(regime) OVER (PARTITION BY symbol ORDER BY Date) as to_state
    FROM markov_model_regimes
    WHERE ret_20d IS NOT NULL
)
SELECT 
    symbol,
    from_state,
    to_state,
    CAST(COUNT(*) AS FLOAT) / SUM(COUNT(*)) OVER (PARTITION BY symbol, from_state) as probability
FROM state_pairs
WHERE to_state IS NOT NULL
GROUP BY symbol, from_state, to_state;

-- Phase 3: Signal Generation Pipeline
CREATE TABLE IF NOT EXISTS markov_model_predictions AS
SELECT 
    r.Date,
    r.symbol,
    r.regime as current_state,
    COALESCE(MAX(CASE WHEN t.to_state = 1 THEN t.probability END), 0.0) as prob_bull,
    COALESCE(MAX(CASE WHEN t.to_state = -1 THEN t.probability END), 0.0) as prob_bear,
    COALESCE(MAX(CASE WHEN t.to_state = 1 THEN t.probability END), 0.0) - COALESCE(MAX(CASE WHEN t.to_state = -1 THEN t.probability END), 0.0) as signal
FROM markov_model_regimes r
JOIN markov_model_transition_matrix t ON r.regime = t.from_state AND r.symbol = t.symbol
GROUP BY r.Date, r.symbol, r.regime
ORDER BY r.Date;
