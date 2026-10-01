ATTACH DATABASE 'data/market_history.db' AS market;
ATTACH DATABASE 'reports/hmm_regime.db' AS hmm;

CREATE TEMP VIEW IF NOT EXISTS markov_model_regimes AS
SELECT 
    b.symbol,
    b.Date, 
    b.close,
    h.return as ret_20d,
    CASE 
        WHEN h.predicted_state = 2 THEN 1
        WHEN h.predicted_state = 0 THEN -1
        ELSE 0 
    END as regime
FROM market.backtest_start b
JOIN hmm.hmm_regime_history h 
    ON b.symbol = h.symbol 
    AND substr(b.Date, 1, 10) = substr(h.date, 1, 10)
WHERE b.timeframe = '1d';

SELECT count(*) FROM markov_model_regimes;
