# data/

Everything under `$APP_FOLDER/data/` is machine-generated and git-ignored (`*.db`, `*.sqlite`).

- `market_history.db`: daily and intraday bars (table `backtest_start`) plus option chains. Written by `market_history` and by auto-download in `backtest` and `livescan`.
- `reports/`: every result database and HTML page: per-strategy `<id>.db`, `shared_*.db`, `gridsearch.db`, `scoreboard.db`, `strategies.db`, `walk_forward.db`, `livescan.db`, study output, and `backtest_report.html`.
- `ignore_over50MB.sh`: appends files over 50 MiB to this folder's `.gitignore`.

Reference data (strategy tables, universe) lives beside it in `refdata/`, not here. See the repository README.
