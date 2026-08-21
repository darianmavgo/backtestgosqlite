# Data Directory

This directory holds local SQLite databases used for backtesting, universe definition, and caching:

- `settings.db`: Master configuration and symbol lists (`leveraged_etf`, `momentum_candidates`, `backtested_win_20_10d`).
- `wc_master_backtest.db`: Target database for 4-year backtest runs.
- `leveraged_backtest.db`: Target database for leveraged ETF experiments.

*Note: Database files (`*.db`, `*.sqlite`) are ignored by Git.*
