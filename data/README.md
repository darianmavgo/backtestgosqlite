# Data Directory

This directory holds local SQLite databases used for backtesting, universe definition, and caching:

- `settings.db` (now in `refdata/`, see `APP_REF` in `.env`): Master configuration and symbol lists (`leveraged_etf`, `momentum_candidates`, `backtested_win_20_10d`), the ETF universes (`etf_universe`: lists `all`, `6yr`, `sweep`) and per-ETF decision-tree configs (`etf_dt_strategies`). No txt/CSV ticker lists are used.
- `wc_master_backtest.db`: Target database for 4-year backtest runs.
- `leveraged_backtest.db`: Target database for leveraged ETF experiments.

*Note: Database files (`*.db`, `*.sqlite`) are ignored by Git.*
