-- Why the strategy did or did not enter, for the latest five sessions. One row per session in
-- signal_trace, which a live scan reads and shows next to its ENTER or NO_SIGNAL. The entry column
-- comes from the signals table above, so this explains the decision and never makes it.
-- scored_live says the session is after trained_through, so it was scored with frozen parameters.
DROP TABLE IF EXISTS signal_trace;

CREATE TABLE signal_trace (
    symbol TEXT,
    date TEXT,
    entry INTEGER,
    reason TEXT,
    facts TEXT
);

DROP TABLE IF EXISTS hmm_trace_dates;

CREATE TABLE hmm_trace_dates AS
SELECT Date, stress_prob, prev_stress_prob, next_stress_prob
FROM hmm_stress_lag
ORDER BY Date DESC
LIMIT 5;

INSERT INTO signal_trace (symbol, date, entry, reason, facts)
SELECT
    '__TRADE_SYMBOL__',
    d.Date,
    s.entry IS NOT NULL,
    CASE
        WHEN s.entry IS NOT NULL THEN 'ENTER: stressed probability ' || printf('%.2f', d.stress_prob) || ' (' || '__RULE__' || ', threshold ' || '__STRESS_PROB__' || ')'
        WHEN d.stress_prob >= __STRESS_PROB__ THEN 'no entry: stressed probability ' || printf('%.2f', d.stress_prob) || ' is at or over ' || '__STRESS_PROB__'
        WHEN '__RULE__' = 'calm_down' THEN 'no entry: calm_down needs the session before to be stressed, it was ' || printf('%.2f', coalesce(d.prev_stress_prob, 0))
        ELSE 'no entry: the rule qualifies but there is no __TRADE_SYMBOL__ bar for this date'
    END,
    json_object(
        'model_symbol', '__SYMBOL__',
        'rule', '__RULE__',
        'threshold', __STRESS_PROB__,
        'stress_prob', round(d.stress_prob, 4),
        'prev_stress_prob', round(d.prev_stress_prob, 4),
        'next_stress_prob', round(d.next_stress_prob, 4),
        'trained_through', m.trained_through,
        'scored_live', d.Date > m.trained_through
    )
FROM hmm_trace_dates d
CROSS JOIN hmm.hmm_model_meta m
LEFT JOIN hmm_strategy_signals s ON s.date = d.Date AND s.entry = 1
WHERE m.symbol = '__SYMBOL__'
ORDER BY d.Date;
