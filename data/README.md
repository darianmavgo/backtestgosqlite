# Data Directory

This directory holds local SQLite databases used for backtesting, universe definition, and caching:

- `settings.db` (now in `refdata/`, see `APP_REF` in `.env`): Master configuration and symbol lists (`leveraged_etf`, `momentum_candidates`, `backtested_win_20_10d`), the ETF universes (`etf_universe`: lists `all`, `6yr`, `sweep`) and per-ETF decision-tree configs (`etf_dt_strategies`). Ticker lists live in those SQLite tables.
- `market_history.db`: Daily and intraday bars (`backtest_start`). This is the database commands read and write.
- `leveraged_backtest.db`: Fallback market database when `market_history.db` is absent.

*Note: Database files (`*.db`, `*.sqlite`) are ignored by Git.*
