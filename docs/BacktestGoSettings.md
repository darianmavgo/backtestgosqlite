# backtestgo settings table (snapshot, 2026-10-13)

Kept for reference when backtestgo is retired. `backtestgosqlite` has no settings table; the ones ported are marked **(ported)** in the last column of `docs/BacktestConsolidation` Phase 3: `split_jump_ratio` (split detection) and `max_bar_volume_share` (cap an entry at a share of the hour's volume).

| key | value | meaning |
|---|---|---|
| in_sample_start | 2021-01-01 | first day of the in-sample period: models and grid searches learn from it, the backtest reports it, market_history backfills to it |
| in_sample_stop | 2025-10-09 | last in-sample session: models learn only through it (train -through) |
| oos_start | 2025-10-10 | first out-of-sample day: the leaderboard ranks strategies on oos_start to oos_stop |
| oos_stop | 2026-10-09 | last out-of-sample day, and the last day the backtest simulates (blank = the latest session) |
| initial_capital | 100000 | starting equity of every simulated strategy |
| default_allocation | 0.2 | share of equity per position when a strategy row has none (or one above 100%) |
| max_bar_volume_share | 0.05 | an entry may take at most this share of the shares traded in the hour it fills (0 = no cap): stops a small stock being bought in sizes it never traded |
| allocation_override |  | share of equity per position for every strategy of a backtest, comma separated (0.1,0.2,0.3 = one run of each); blank = each strategy keeps its own allocation_pct |
| crisis_stop | 0.2 | stop distance for a strategy whose row has no stop_loss_pct (live pipeline: 0.20) |
| split_jump_ratio | 1.8 | a close-to-close move between hourly bars beyond this ratio (or under its inverse) is treated as an unadjusted split |
| market_db | data/market_history_hourly.db | hourly market history: the SQLite (WAL) market database; calculation files attach it read-only |
| markov_db | data/markov_models.db | trained Markov models, a SQLite file (in-sample only: train markov -model-db data/markov_models.db) |
| tree_db | data/tree_models.db | trained decision trees, a SQLite file (in-sample only: train tree -model-db data/tree_models.db) |
| train_batch | 200 | symbols per pass of the Markov training SQL |
| market_feed | sip | Alpaca feed: sip (paid, includes extended hours) or iex (free) |
| market_adjustment | split | Alpaca price adjustment: raw, split, dividend or all |
| market_workers | 10 | symbols downloaded at once (at most 10) |
| backtest_fetch_market | true | a backtest of one strategy first downloads the market history that strategy needs (like market_history -strategy); false = use the market DB as it is |
| gridsearch_sort | sharpe | the in-sample measure gridsearch picks variants by: sharpe, cagr, return, maxdd or winrate |
| gridsearch_min_trades | 20 | a variant needs this many in-sample trades to be picked |
| gridsearch_max_variants | 500 | variants tried per strategy row; a bigger grid is sampled |
| gridsearch_top | 3 | variants shown per strategy row |
