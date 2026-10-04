-- Every strategy tied to GOOGL, by signal or trade symbol. Run with stratlist.
SELECT id FROM streak_strategy WHERE upper(signal_symbol) = 'GOOGL' OR upper(trade_symbol) = 'GOOGL'
UNION
SELECT id FROM tree_strategy   WHERE upper(signal_symbol) = 'GOOGL' OR upper(trade_symbol) = 'GOOGL'
UNION
SELECT id FROM markov_strategy WHERE upper(signal_symbol) = 'GOOGL' OR upper(trade_symbol) = 'GOOGL'
UNION
SELECT id FROM hold_strategy   WHERE upper(symbol) = 'GOOGL'
ORDER BY 1;
