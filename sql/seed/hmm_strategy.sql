-- The first two hmm strategies. Both trade QQQ from the QQQ hidden Markov model (train hmm).
-- The regimes are volatility regimes, so a rule uses the stressed regime and never a state number.
--   hmm-calm-qqq:     long while the stressed probability is under 50%. Hold 3 sessions then re-check.
--   hmm-calmdown-qqq: long when the stressed probability falls back under 50%. Hold 10 sessions.
-- Exits are starting points, not tuned. Needs `train hmm` and the daily bars (daily_bars).
-- Run: sqlite3 refdata/strategies.db < sql/seed/hmm_strategy.sql
CREATE TABLE IF NOT EXISTS hmm_strategy (
	id               TEXT PRIMARY KEY,
	name             TEXT NOT NULL,
	hmm_symbol       TEXT NOT NULL DEFAULT 'QQQ',
	trade_symbol     TEXT NOT NULL DEFAULT 'QQQ',
	rule             TEXT NOT NULL,
	stress_prob      REAL NOT NULL,
	hold_days        INTEGER NOT NULL,
	take_profit_pct  REAL NOT NULL,
	stop_loss_pct    REAL NOT NULL,
	allocation_pct   REAL NOT NULL,
	cash_yield       REAL NOT NULL,
	slippage_pct     REAL NOT NULL,
	source_strategy  TEXT,
	source_label     TEXT,
	win_rate         REAL,
	total_trades     INTEGER
);

INSERT OR REPLACE INTO hmm_strategy
    (id, name, hmm_symbol, trade_symbol, rule, stress_prob, hold_days, take_profit_pct, stop_loss_pct, allocation_pct, cash_yield, slippage_pct)
VALUES
    ('hmm-calm-qqq', 'QQQ Long While Calm (HMM)', 'QQQ', 'QQQ', 'calm_filter', 0.5, 3, 0.0, 0.04, 0.20, 0.045, 0.0005),
    ('hmm-calmdown-qqq', 'QQQ Long After Stress Eases (HMM)', 'QQQ', 'QQQ', 'calm_down', 0.5, 10, 0.0, 0.05, 0.20, 0.045, 0.0005);
