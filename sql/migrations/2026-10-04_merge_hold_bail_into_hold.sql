-- NOT YET RUN. Merges the hold_bail family into hold. A hold row with
-- trailing_stop_pct = 0 and sma_reentry_period = 0 never bails and never
-- re-enters, which is plain buy and hold. Back up refdata/strategies.db, then:
--   sqlite3 refdata/strategies.db < sql/migrations/2026-10-04_merge_hold_bail_into_hold.sql
-- The code reads the two new columns, so hold_strategy fails to load until this is applied.
BEGIN;
ALTER TABLE hold_strategy ADD COLUMN trailing_stop_pct REAL NOT NULL DEFAULT 0;
ALTER TABLE hold_strategy ADD COLUMN sma_reentry_period INTEGER NOT NULL DEFAULT 0;
INSERT INTO hold_strategy (id, name, symbol, total_return, allocation_pct, cash_yield, slippage_pct, trailing_stop_pct, sma_reentry_period)
SELECT id, name, symbol, 0, allocation_pct, cash_yield, slippage_pct, trailing_stop_pct, sma_reentry_period
FROM hold_bail_strategy;
-- Once the rows are checked, retire the old family:
--   DROP TABLE hold_bail_strategy;
--   DELETE FROM strategy_family_param WHERE family = 'hold_bail';
COMMIT;
