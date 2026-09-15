# backtestgosqlite: High-Performance Algorithmic Trading & Multi-Strategy Backtesting Platform

[![Go Version](https://img.shields.io/badge/Go-1.18+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Architecture](https://img.shields.io/badge/Engine-Go%20%2B%20SQLite%20WAL-brightgreen)]()

**`backtestgosqlite`** is a high-speed, general-purpose quantitative backtesting and algorithmic trading engine engineered in **Go** and backed by **SQLite WAL (Write-Ahead Logging)**.

It pairs the raw execution speed and goroutine concurrency of compiled Go with the relational query power of SQLite to evaluate universe-wide portfolios, multi-asset strategies, and path-dependent risk analytics in milliseconds.

---

## 🏛️ System Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          1. FLEXIBLE DATA LAYER                             │
│  - Pluggable DataSource interface: CSV, Yahoo Finance Chart API, Stooq     │
│  - Multi-asset OHLCV bars indexed in SQLite WAL memory/disk tables          │
│  - Bring any CSV dataset (Polygon, Alpaca, custom feeds) zero Go required  │
└──────────────────────────────────────┬──────────────────────────────────────┘
                                       │
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                 2. UNIFIED STRATEGY ENGINE (GO & SQL)                       │
│  - 30-line Go Strategy interface with automatic CLI discovery               │
│  - Built-in Vectorized Technical Indicators (RSI, BB, MACD, Donchian, ATR)   │
│  - Pure SQL Pipeline Strategies executed sequentially from sql/strategies/  │
└──────────────────────────────────────┬──────────────────────────────────────┘
                                       │ Entry Signals & Limits
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│            3. CHRONOLOGICAL MULTI-ASSET PORTFOLIO SIMULATOR                 │
│  - Real-time Cash Ledger & Portfolio Equity Curve                           │
│  - Order Types: Market, Limit, Stop-Limit                                   │
│  - Dynamic Risk: Trailing Stops, ATR Stops, Dual-Barrier Fixed Stops        │
│  - Configurable Position Sizing: Fixed %, Fixed $, Fixed Shares, Kelly      │
│  - Concurrent multi-strategy benchmarking across Goroutines                 │
└──────────────────────────────────────┬──────────────────────────────────────┘
                                       │ Realized Trades & Equity Series
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│              4. INSTITUTIONAL QUANTITATIVE TEAR SHEET & UI                  │
│  - Metrics: Sharpe, Sortino, Calmar, Omega, Ulcer Index, Alpha & Beta       │
│  - Trade Metrics: Win Rate, Profit Factor, Payoff Ratio, MAE & MFE          │
│  - Interactive Local Web Dashboard & Standalone HTML Reports                │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 🚀 Key Capabilities

### 1. General-Purpose Multi-Asset Backtesting
* **Multiple Order Execution Models**: Supports `market` (next open/close), `limit` (intraday price crossing), and `stop_limit` orders.
* **Flexible Position Sizing**: Choose between `fixed_pct` (e.g. 20% equity), `fixed_dollar` ($10,000/trade), `fixed_shares` (100 shares), or risk-based `kelly` sizing.
* **Dynamic Stop-Loss Systems**:
  * **Trailing Stops**: Lock in unrealized gains by trailing peaks at a configurable percentage.
  * **ATR Dynamic Stops**: Protect against volatility expansion using multiples of Average True Range.
  * **Dual-Barrier Path Checking**: Walks day-by-day to simulate real-world intraday stops before profit targets.
* **Concurrent Multi-Strategy Engine**: Run multiple strategies simultaneously across Go goroutines (`./bin/backtest -strategy s1,s2,s3` or `./bin/backtest all`).

### 2. Smart Cache-First Market Data Ingestion (`cmd/download`)
* **Default Database**: Automatically manages and caches historical bars in **`data/market_history.db`**.
* **Cache-First Intelligence**: Checks `market_history.db` first for existing date coverage (`min_date`, `max_date`, bar count). If data is already present, **skips network requests entirely** for instant execution.
* **Surgical Incremental Fetching**: Downloads only the missing slices (older historical gaps or newer daily bars) from Yahoo Finance API v8 or Stooq CSV, then merges them with zero duplication.
* **Bring Your Own CSV**: Ingest any standard OHLCV CSV file (from Polygon.io, Alpaca, Interactive Brokers, or manual export) using `-csv <path>`.

### 3. Isolated Strategy SQLite Results (`cmd/backtest`)
* **Strategy-Named Databases**: Every backtest writes its complete calculations into a dedicated database named after the strategy (e.g., **`reports/bb-capitulation.db`**).
* **Automatic Suffixing**: If the database already exists, it atomically appends incrementing suffixes (`_2.db`, `_3.db`, etc.) to prevent overwriting prior backtests.
* **Complete Relational Results**: Stores all 4 calculation tables: `signals`, `trades`, `equity_curve`, and `performance_summary`.
* **Concurrent Lock-Free Writing**: When backtesting multiple strategies concurrently, each goroutine writes to its own isolated SQLite file in parallel with zero SQLite lock contention.

### 4. Built-in Technical Indicator Library
Zero external C dependencies. Pure Go vectorized indicator math in [`pkg/strategy/indicators.go`](file:///Users/darianhickman/Documents/backtestgosqlite/pkg/strategy/indicators.go):
* **Moving Averages**: `CalcSMA`, `CalcEMA`
* **Oscillators**: `CalcRSI` (Wilder's smoothing)
* **Volatility**: `CalcBollinger`, `CalcATR`, `CalcDonchian`
* **Trend & Momentum**: `CalcMACD` (MACD line, Signal line, Histogram)

### 5. Built-in Strategy Library
* **`bb-capitulation`**: Lower Bollinger Band exhaustion pierces with RSI(5) oversold confirmation.
* **`voo-tecl-combo`**: VOO regime-filtered mean reversion allocating between TECL and inverse hedging.
* **`macd-crossover`**: Classic MACD (12, 26, 9) signal-line bullish crossover.
* **`donchian-breakout`**: Turtle-style 20-day high momentum breakout with trailing stop.
* **`trend-bb`**: Macro trend-gated Bollinger dips (Close > SMA50).
* **`rsi2`**: Connors RSI(2) deep pullback strategy.
* **`voo-buy-hold`**: Passive VOO (S&P 500) buy-and-hold baseline for computing active Alpha & Beta. (Formerly `buy-and-hold` — renamed after it was found to buy an arbitrary basket of symbols rather than VOO once the ETF universe grew past a few hundred symbols; still resolvable under the old ID via alias.)
* **`dt_<symbol>`**: One auto-generated CloudForest decision-tree strategy per qualifying ETF (e.g. `dt_fxr`), produced by `cmd/etf_decision_trees` and registered automatically at startup from `data/etf_dt_strategies.csv`.

> `wc` / `wc-4d` / `whitings_creek-sql` (Whitings Creek short-term capitulation mean-reversion) were archived — unregistered and moved to [`_archive/`](file:///Users/darianhickman/Documents/backtestgosqlite/_archive) (excluded from the Go build via the leading underscore). They converged to the exact same generic-fallback gridsearch result as several other strategies with no real distinguishing signal, while taking 2-4 minutes per sweep. See `_archive/README.md` to restore.

### 6. Research, Optimization & Diagnostic Tools
Beyond the core backtest/download/livescan loop, the platform includes a set of standalone CLIs for universe discovery, parameter search, and results auditing — see the [Full Command Reference](#-full-command-reference) for every flag:
* **`cmd/gridsearch`**: Multi-core TP/SL/hold/allocation parameter sweep per strategy (or all strategies), with a shared worker pool and a persistent `reports/gridsearch.db` so repeat runs skip strategies already swept.
* **`cmd/scoreboard`**: Backtests every registered strategy against the full ETF universe and compiles a single leaderboard (`reports/scoreboard.db`); `compile`/`status` subcommands re-aggregate or check completeness without re-running backtests.
* **`cmd/etf_decision_trees`**: Fits a CloudForest decision tree per ETF and grid-sweeps its BUY predictions, feeding the `dt_<symbol>` strategy family above.
* **`cmd/etf_universe`**: Pulls the full active US ETF ticker list from Polygon.io for `cmd/download -symbols-file`.
* **`cmd/ticker_scan`**: Tests whether a hand-tuned pattern (e.g. MARA's 200-SMA bounce) generalizes across the whole symbol universe.
* **`cmd/study`**: Runs registered one-off statistical studies (e.g. Granger-causality lead/lag, 5%-gain frequency) against `market_history.db`.
* **`cmd/audit_shared`**, **`cmd/compare_annual_report`**, **`cmd/candlesticks`**, **`cmd/granger_chart`**: Post-hoc SQL/HTML diagnostics and visualizations over already-generated `reports/*.db` result databases.
* **`cmd/dataflare`**: One-line launcher for the Dataflare macOS SQLite GUI, pointed at any project database.

---

## 🔄 Complete Data Pipeline & SQLite Architecture

The platform cleanly separates market data caching from backtest calculation storage, ensuring full reproducibility, fast incremental updates, and concurrency safety:

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                               1. MARKET DATA INGESTION                                 │
│  - Remote Feeds: Yahoo Finance Chart API v8, Stooq CSV fallback                        │
│  - Local Data: Custom OHLCV CSVs (Polygon, Alpaca, IBKR) via -csv                      │
└───────────────────────────────────────────┬────────────────────────────────────────────┘
                                            │ Incremental / Cache-First
                                            ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                        2. MARKET DATA CACHE (READ-ONLY IN BACKTEST)                    │
│  📂 data/market_history.db (Table: backtest_start)                                      │
│  - Pre-queries existing MIN(Date), MAX(Date), and bar counts per symbol                │
│  - Skips network requests if requested horizon is fully cached                         │
│  - Surgically pulls only missing slices (older historical gaps or newer daily bars)    │
│  - Composite indexed lookups: idx_backtest_start_unique ON (symbol, Date)               │
└───────────────────────────────────────────┬────────────────────────────────────────────┘
                                            │ Read-Only Historical Bars
                                            ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                          3. CHRONOLOGICAL SIMULATION ENGINE                            │
│  - cmd/backtest, cmd/livescan, cmd/gridsearch, cmd/scoreboard, cmd/study,              │
│    cmd/etf_decision_trees, cmd/ticker_scan all read this same bar cache                │
│  - Evaluates Go & SQL Strategies concurrently across Goroutines                        │
│  - Computes signals, walks daily bars, manages cash ledger, trailing stops, PnL       │
└───────────────────────────────────────────┬────────────────────────────────────────────┘
                                            │ Isolated Calculations
                                            ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                    4. STRATEGY RESULTS PERSISTENCE (WRITE-ONLY)                        │
│  📂 reports/<strategy_id>.db                                                           │
│  - Automatically appends _2, _3, ... suffixes if database already exists               │
│  - Dedicated concurrent SQLite file per strategy (zero database lock contention)       │
│                                                                                        │
│  Captured Tables:                                                                      │
│  ├── signals             : Entry signals (date, symbol, order_type, price, regime)     │
│  ├── trades              : Closed trades (entry/exit $, hold days, PnL, MAE, MFE)      │
│  ├── equity_curve        : Daily portfolio time-series (equity, cash, invested, MDD)   │
│  └── performance_summary : Institutional tear sheet (CAGR, Sharpe, Sortino, Calmar)    │
└───────────────────────────────────────────┬────────────────────────────────────────────┘
                                            │ Multi-Strategy Aggregation
                                            ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                        5. REPORTING, AUDITING & VISUALIZATION                          │
│  - Console: Quantitative Tear Sheets (cmd/backtest), Leaderboards (cmd/scoreboard),    │
│    Shared-Account Audits (cmd/audit_shared)                                            │
│  - HTML: Chart.js Dashboards (reports/backtest_report.html), Gridsearch reports,        │
│    Go-ECharts Candlesticks/Granger dashboards (cmd/candlesticks, cmd/granger_chart),    │
│    Standalone-vs-Shared annual comparisons (cmd/compare_annual_report)                 │
│  - Web UI: Local browser server (make ui -> http://localhost:8085, legacy dataset)      │
│  - GUI: cmd/dataflare opens any of the above SQLite files in the Dataflare app          │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

### SQLite Database Files Reference

| Database File | Directory | Primary Role | Schema / Key Tables | Written By | Read By |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **`market_history.db`** | `data/` | **Master OHLCV Bar Cache** | `backtest_start` (OHLCV bars: `idx`, `Date`, `timeframe`, `asset_class`, `open`, `high`, `low`, `close`, `Adj Close`, `volume`, `symbol`) — see [Market Data Cache](#-market-data-cache-market_historydb) below | `cmd/download` | `cmd/backtest`, `cmd/livescan`, `cmd/gridsearch`, `cmd/scoreboard`, `cmd/study`, `cmd/etf_decision_trees`, `cmd/ticker_scan`, `cmd/candlesticks` |
| **`<strategy_id>.db`** *(e.g. `bb-capitulation.db`, `_2.db`, `_3.db`)* | `reports/` | **Isolated Backtest Run Results** | `signals` (incl. `metadata` JSON column), `trades`, `equity_curve`, `performance_summary` | `cmd/backtest` | `cmd/audit_shared`, `cmd/compare_annual_report`, external analysis, SQLite CLI |
| **`shared_<primary>_<secondary>.db`** *(e.g. `shared_voo-tecl-combo_bb-capitulation_2.db`)* | `reports/` | **Shared-Account Combo Results** — one cash ledger split across multiple strategies with priority preemption | same 4 tables as above, plus preemption bookkeeping | `cmd/backtest -shared-account` | `cmd/audit_shared`, `cmd/compare_annual_report` |
| **`livescan_signals.db`** *(optional, with `-save`)* | `reports/` | **Live Signal Log** | `live_signals` (actionable orders for today/tomorrow) | `cmd/livescan -save` | Live order execution & alerts |
| **`scoreboard.db`** | `reports/` | **Cross-Strategy Leaderboard** | Aggregated per-strategy performance rows | `cmd/scoreboard` | Console leaderboard, external analysis |
| **`gridsearch.db`** | `reports/` | **Parameter-Sweep Pipeline State** | `gridsearch_runs`, `gridsearch_results` | `cmd/gridsearch` | `cmd/gridsearch` (skip-if-done cache), external analysis |
| **`<study_id>.db`** *(e.g. `gain_5pct_frequency.db`, `march_april_voo_gld_uten.db`)* | `reports/` | **Ad-hoc Study Output** | Study-specific tables (e.g. `granger_causality`) | `cmd/study` | `cmd/granger_chart`, external analysis |
| **`settings.db`** | `data/` | **Optional Universe/Config Seed** | `leveraged_etf`, `momentum_candidates`, `backtested_win_20_10d` (seeded manually; ships empty) | Seed scripts / Admin | `cmd/download -table` |
| **`sample.db`** | `data/` | **Testing & Custom CSV Sandbox** | `backtest_start` | `cmd/download -csv` | `examples/custom_csv_backtest` |
| **`sp500_etfs_study.db`** | `data/` | **Multi-Scenario Study Matrix** | `tecl_allocation_matrix`, `compare_3x_etfs_matrix`, plus a local `backtest_start` bar cache for VOO/TECL/SPXU | `cmd/export_studies` (both reads and writes this file) | Study reports |

> `data/*.db` and `reports/*.db` are all git-ignored (see `.gitignore`) — every database above is regenerated locally by running the corresponding command, never committed.

---

## 🗄️ Market Data Cache (`market_history.db`)

`data/market_history.db` is the single read cache every backtesting/analysis tool sources bars from. It's a plain SQLite (WAL-mode) file with one table:

```sql
CREATE TABLE backtest_start (
    idx         INTEGER,        -- sequential bar index per symbol/timeframe
    Date        DATETIME,
    timeframe   TEXT DEFAULT '1d',   -- '1d', '1h', '5m', '1m', ...
    asset_class TEXT DEFAULT 'equity',
    open        FLOAT,
    high        FLOAT,
    low         FLOAT,
    close       FLOAT,
    "Adj Close" FLOAT,
    volume      BIGINT,
    symbol      TEXT
);
CREATE UNIQUE INDEX idx_backtest_start_unique   ON backtest_start(symbol, Date, timeframe);
CREATE INDEX        idx_backtest_start_sym_date ON backtest_start(symbol, Date);
CREATE INDEX        idx_backtest_start_sym_tf_date ON backtest_start(symbol, timeframe, Date);
CREATE INDEX        idx_backtest_start_sym_idx  ON backtest_start(symbol, idx);
CREATE INDEX        idx_backtest_start_idx      ON backtest_start(idx);
```

* **One row per (symbol, date, timeframe)** — the unique index makes re-downloads idempotent, which is what lets `cmd/download` merge incremental fetches with zero duplication.
* **Mixed timeframes in one table** — daily bars are the default (`timeframe='1d'`), but a handful of tools (e.g. `cmd/candlesticks`) pull intraday `'1m'` bars for specific symbols/date ranges that were downloaded separately.
* **As of this writing**, the checked-out copy holds **~2.76M rows across 1,808 symbols**, spanning **2020-09-14 to 2026-09-11** (actual per-symbol history varies from ~6 to 30+ years since Yahoo/Stooq return each ticker's full available history, not just the requested window).
* **Scoped loading, not always full-DB**: `cmd/backtest` loads only the symbols a single strategy (or shared-account pair) actually needs via `storage.FetchBars`, instead of every symbol in the table — multi-strategy batch runs still load the full table once and amortize that cost across all strategies in the batch. This matters at 1,800+ symbols: a single-strategy run that used to pay a fixed ~20-30s full-table load now runs in a couple of seconds.

---

## ⚡ Quickstart Guide

### 1. Build All Binaries
```bash
make build
```

### 2. View All Available Strategies
```bash
make list
# or:
./bin/backtest -list
```

### 3. Download & Cache Market Data (Smart Cache-First)
Download historical bars into `data/market_history.db`. The downloader automatically checks what data already exists in the database and only pulls missing dates:
```bash
# Download 5 years of history for VOO and TECL
./bin/download -symbols VOO,TECL -years 5

# Download top leveraged ETF universe from settings.db
./bin/download -table leveraged_etf -limit 50 -years 4

# Run again: instantly skips network calls if data is up-to-date!
./bin/download -symbols VOO,TECL -years 5
```

### 4. Run Custom CSV Backtest
Ingest your own OHLCV CSV file and run an immediate backtest:
```bash
make example-csv
# or:
./bin/download -csv examples/custom_csv_backtest/sample_stocks.csv -db data/sample.db
./bin/backtest -db data/sample.db -strategy donchian-breakout -capital 50000
```

### 5. Run Strategy Backtests
```bash
# Run by Strategy ID or Positional Argument (creates reports/bb-capitulation.db)
./bin/backtest bb-capitulation -capital 100000
./bin/backtest voo-tecl-combo

# Running again automatically creates reports/bb-capitulation_2.db, _3.db, etc.
./bin/backtest bb-capitulation

# Run Multiple Strategies Concurrently (writes separate SQLite databases in parallel!)
./bin/backtest -strategy bb-capitulation,trend-bb,rsi2,macd-crossover

# Run All Registered Strategies Concurrently
./bin/backtest all

# Single Symbol Filter (e.g. SOXL, AAPL, SPY)
./bin/backtest -strategy bb-capitulation -symbol SOXL -capital 100000
```

### 6. Live Signal Scanner (`cmd/livescan`)
Scan recent market history to calculate whether a position entry should happen **today** or **tomorrow**. Takes the exact same arguments as `backtest`, but operates lightning-fast by only querying recent warm bars from SQLite:
```bash
# Scan a single strategy on all universe symbols
./bin/livescan bb-capitulation -capital 100000

# Scan specific symbols with custom risk overrides
./bin/livescan -strategy bb-capitulation -symbol SOXL,TECL -capital 50000 -stoploss 0.95 -target 1.15

# Scan ALL strategies concurrently across the entire database universe
./bin/livescan all

# Output as JSON for automated execution bots or cron jobs
./bin/livescan -strategy bb-capitulation -json

# Save actionable orders to SQLite database (reports/livescan_signals.db)
./bin/livescan all -save
```

### 7. Launch the Local Web Dashboard
```bash
make ui
# Open http://localhost:8085 in your browser (or ./bin/ui -port <N> for a different port)
```
> `cmd/ui` predates the current strategy library and is still wired to the archived Whitings Creek dataset (`data/wc_master_backtest.db`), which no longer exists in a fresh checkout — its symbol-summary view will come up empty until that's repointed at `market_history.db`/`reports/*.db`. For current results, prefer `./bin/scoreboard`, the per-strategy `reports/<id>.db` files, or `reports/backtest_report.html`.

---

## 📖 Full Command Reference

Every binary lives in `cmd/<name>/main.go` and builds to `bin/<name>`. `make build` only compiles `download`, `backtest`, `livescan`, `ui`, and `study`; build the rest ad hoc with `go build -o bin/<name> ./cmd/<name>` (or `go build -o bin/ ./cmd/...` to build everything at once).

#### `cmd/download` — Market data ingestion
Cache-first OHLCV downloader; see [Quickstart §3](#3-download--cache-market-data-smart-cache-first).
| Flag | Default | Meaning |
| :--- | :--- | :--- |
| `-db` | `data/market_history.db` | Target SQLite DB for bars |
| `-settings` | `data/settings.db` | Settings DB for `-table` symbol-list lookups |
| `-source` | `yahoo` | `yahoo`, `polygon`, `stooq`, or `csv` |
| `-polygon-key` | *(env `POLYGON_API_KEY` / `.env`)* | Polygon.io API key |
| `-csv` | *(empty)* | CSV file or directory to import (with `-source csv`) |
| `-symbols` | *(empty)* | Comma-separated symbols to fetch |
| `-symbols-file` | *(empty)* | Text file, one symbol per line |
| `-table` | `leveraged_etf` | Fallback symbol-list table in `-settings` DB if no symbols given |
| `-limit` | `50` | Max symbols from `-table` (`0` = all) |
| `-years` | `4` | Years of history to fetch |
| `-start` / `-end` | *(empty)* | Explicit `YYYY-MM-DD` range, overrides `-years` |
| `-timeframe` | `1d` | `1d`, `1h`, `5m`, `1m` |
| `-target-table` | `backtest_start` | Destination table name |
| `-force` | `false` | Re-download bars even if already cached |
| `-seed-db` | `data/leveraged_backtest.db` | Legacy DB to seed from if the target DB doesn't exist yet |
| `-concurrency` | all CPU cores | Concurrent symbol fetches (network-bound) |

#### `cmd/backtest` — Strategy simulation & tear sheets
| Flag | Default | Meaning |
| :--- | :--- | :--- |
| `-db` | `data/market_history.db` | Source bars DB |
| `-table` | `backtest_start` | Bars table name |
| `-strategy` | *(empty)* | Strategy ID, comma-list, `all`, or `stratA+stratB` for a shared account |
| `-shared-account` | `false` | Run strategies in one cash account with priority preemption |
| `-primary` / `-secondary` | *(empty)* | Explicit primary/secondary IDs for `-shared-account` (secondary is comma-separated) |
| `-symbol` | *(empty)* | Restrict to one symbol |
| `-capital` | `100000` | Starting capital |
| `-max-positions`, `-stoploss`, `-target`, `-hold` | `0` / `0.0` (strategy default) | Per-run overrides of the strategy's own config |
| `-out-dir` | `reports` | Where result DBs and the HTML report are written |
| `-html` | `reports/backtest_report.html` | Interactive dashboard output path |
| `-auto-download` | `true` | Auto-fetch missing bars before running |
| `-download-years` | `5` | History window if auto-downloading |
| `-concurrency` | all CPU cores | Parallel strategies for multi-strategy/`all` runs |
| `-force` | `false` | Multi-strategy runs only: redo strategies that already have a usable result |
| `-list` | `false` | List all registered Go and SQL strategies |
| A single strategy ID can also be passed positionally: `./bin/backtest bb-capitulation`. |

#### `cmd/livescan` — Today/tomorrow signal scanner
Same strategy/override flags as `backtest` (`-db`, `-table`, `-strategy`, `-symbol`, `-capital`, `-max-positions`, `-stoploss`, `-target`, `-hold`, `-list`, `-concurrency`), plus:
| Flag | Default | Meaning |
| :--- | :--- | :--- |
| `-bars` | `250` | Recent bars per symbol to load for indicators |
| `-lookback` | `3` | Trading days of signals to display |
| `-save` | `false` | Persist scanned signals to SQLite |
| `-save-db` | `reports/livescan_signals.db` | Custom save path |
| `-json` | `false` | Emit JSON for automation/cron |

#### `cmd/ui` — Local web dashboard
`-port` (default `8085`) is the only flag. See the [Quickstart §7](#7-launch-the-local-web-dashboard) caveat — it's currently wired to a legacy dataset, not the live strategy library.

#### `cmd/gridsearch` — Parameter optimization sweep
| Flag | Default | Meaning |
| :--- | :--- | :--- |
| `-db` | `data/market_history.db` | Source bars DB |
| `-strategy` (or `-strat`, `-mode`) | *(empty)* | Strategy ID, comma-list, or `all` |
| `-list` | `false` | List registered strategies with baked-in parameters |
| `-signal`, `-symbol` | *(empty)* | Single-strategy mode overrides for signal/trade symbol |
| `-capital` | `100000` | Starting cash |
| `-alloc` | `0.65` | Allocation % (single-strategy mode) |
| `-yield` | `0.045` | Idle-cash annualized yield |
| `-min-trades` | `5` | Minimum trade count to consider a config valid |
| `-top` | `10` | Top N results shown per strategy |
| `-html` | *(empty → `reports/<strategy>_gridsearch.html`)* | Single-strategy HTML export path |
| `-no-html` | `false` | Skip per-strategy HTML export in batch mode |
| `-concurrency` | all CPU cores | Worker goroutines (shared across strategies in batch mode) |
| `-force` | `false` | Redo strategies that already have a completed sweep |
| `-include-dt` | `false` | Include auto-generated `dt_*` strategies in `-strategy all` |
| `-gridsearch-db` | `reports/gridsearch.db` | Pipeline-state and results DB |
| `-max-perms` | `20000` | Skip a strategy in batch mode if its generic grid exceeds this many permutations (`0` disables) |

Example: `./bin/gridsearch -strategy bb-capitulation` (single) or `./bin/gridsearch -strategy all -force` (batch, redo everything).

#### `cmd/scoreboard` — Cross-strategy leaderboard
Subcommands (default `run`): `./bin/scoreboard` backtests every registered strategy against the full universe (skipping ones with a usable result already, unless `-force`) then compiles a leaderboard; `./bin/scoreboard compile` re-aggregates from existing `reports/*.db` files without backtesting; `./bin/scoreboard status` just reports whether all compute is done. Flags: `-concurrency` (all CPU cores), `-force` (`false`, default mode only). Output: `reports/scoreboard.db`.

#### `cmd/study` — Ad-hoc statistical studies
| Flag | Default | Meaning |
| :--- | :--- | :--- |
| `-db` | `data/market_history.db` (or `data/leveraged_backtest.db` fallback) | Source bars DB |
| `-study` | *(empty)* | Study ID to run |
| `-out-dir` | `reports` | Output directory (`reports/<study_id>.db`) |
| `-list` | `false` | List registered studies |

Registered studies: `gain_5pct_frequency`, `daily_gain_5pct_frequency`, `mara_decision_tree`, `mu_decision_tree`, `march_april_voo_gld_uten` (Granger-causality lead/lag analysis — feeds `cmd/granger_chart`).

#### `cmd/export_studies` — SP500/ETF study matrix export
No flags; hardcoded to `data/sp500_etfs_study.db`, which it both reads (a local `backtest_start` bar cache for VOO/TECL/SPXU it maintains) and writes (`tecl_allocation_matrix`, `compare_3x_etfs_matrix`). Run with `./bin/export_studies`.

#### `cmd/etf_decision_trees` — Per-ETF decision-tree fitting
Fits a CloudForest decision tree per symbol, sweeps a TP/SL/hold grid on the tree's BUY predictions, and writes the best config per symbol to `-out` — which `pkg/strategy/etf_decision_tree.go` reads at startup to register a `dt_<symbol>` strategy per qualifying ETF.
| Flag | Default | Meaning |
| :--- | :--- | :--- |
| `-db` | `data/market_history.db` | Source bars DB |
| `-symbols-file` | `data/etf_universe_6yr.txt` | One symbol per line |
| `-out` | `data/etf_dt_strategies.csv` | Output CSV of best config per symbol |
| `-min-trades` | `15` | Minimum trade count for a config to be valid |
| `-workers` | all CPU cores | Concurrent tree-fit + grid-sweep workers |
| `-top` | `40` | Top N results printed |
| `-capital` | `100000` | Starting cash |
| `-alloc` | `0.65` | Allocation % per position |
| `-yield` | `0.045` | Idle cash yield |
| `-force` | `false` | Refit every symbol even if `-out` already has a usable result |

Example: `go run cmd/etf_decision_trees/main.go -symbols-file data/etf_universe_6yr.txt -workers 16`

#### `cmd/etf_universe` — ETF ticker discovery
Pulls every active US-listed ETF ticker from Polygon.io's reference API for use with `cmd/download -symbols-file`. Flags: `-out` (`data/etf_universe_all.txt`), `-polygon-key` (or `POLYGON_API_KEY`/`.env`), `-limit` (`1000`, page size). It only discovers the ticker universe — filtering down to symbols with enough history happens after downloading, by comparing `MIN(Date)` per symbol in `market_history.db`.

#### `cmd/ticker_scan` — Pattern generalization scan
Applies a hand-tuned pattern (the MARA "Precision 200-SMA Bounce": volatility coil + SMA200 re-test) across every candidate symbol to see how well it generalizes beyond the one ticker it was designed for.
| Flag | Default | Meaning |
| :--- | :--- | :--- |
| `-db` | `data/market_history.db` | Source bars DB |
| `-symbols` | *(empty → every symbol in DB)* | Comma-separated candidates |
| `-capital` | `100000` | Starting cash |
| `-alloc` | `0.65` | Allocation % |
| `-tp` / `-sl` | `0.05` / `0.08` | Take-profit / stop-loss % |
| `-hold` | `1` | Holding window (days) |
| `-yield` | `0.045` | Idle cash yield |
| `-min-trades` | `10` | Minimum trade count filter |
| `-optimize-top` | `3` | Sweep TP/SL/hold for this many top-ranked candidates (`0` disables) |
| `-concurrency` | all CPU cores | Concurrent symbol workers |

Example: `./bin/ticker_scan -symbols SOXL,TQQQ,COIN`

#### `cmd/compare_annual_report` — Standalone vs. shared-account comparison
Generates a standalone HTML report comparing a strategy's annual performance run solo vs. inside a shared-account combo, against a VOO benchmark. Flags: `-shared-db` (default `reports/shared_voo-tecl-combo_bb-capitulation_2.db`), `-standalone-db` (default `reports/voo-tecl-combo_4.db`), `-market-db` (`data/market_history.db`), `-html` (`reports/annual_comparison_standalone_vs_shared.html`). The defaults name a specific past run — always override `-shared-db`/`-standalone-db`/`-html` for your own strategies.

#### `cmd/audit_shared` — Shared-account SQL audit
Console diagnostic that verifies per-strategy trade/exit-reason accounting, preempted-position handling, and calendar-year performance for a shared-account result DB. Flag: `-db` (defaults to the most recently modified `reports/shared_*.db`). Example: `./bin/audit_shared -db reports/shared_voo-tecl-combo_bb-capitulation_2.db`.

#### `cmd/candlesticks` — Quick candlestick visualizer
No flags — currently hardcoded to VOO/GLD/UTEN 1-minute bars between 2025-03-01 and 2025-05-01 from `market_history.db` (`timeframe = '1m'`). Edit the constants in `cmd/candlesticks/main.go` to repoint at other symbols/ranges. Writes `reports/candlesticks_go.html`.

#### `cmd/granger_chart` — Granger-causality dashboard
No flags — hardcoded to read the `granger_causality` table from `reports/march_april_voo_gld_uten.db`, so run `./bin/study -study march_april_voo_gld_uten` first. Writes `reports/granger_causality_go.html`.

#### `cmd/dataflare` — SQLite GUI launcher
`./bin/dataflare [path_to_db]` shells out to `open -a Dataflare [db]`, launching the macOS Dataflare app against a project database (requires Dataflare installed in `/Applications`).

---

## 🛠️ Writing Your Own Strategy in Go (Under 35 Lines)

Create `pkg/strategy/my_strategy.go`:

```go
package strategy

import (
    "sort"
    "github.com/darianmavgo/backtestgosqlite/pkg/models"
)

type MyStrategy struct{}

func init() {
    Register(&MyStrategy{}) // Auto-registers in CLI & comparison suite
}

func (s *MyStrategy) ID() string          { return "my-strategy" }
func (s *MyStrategy) Name() string        { return "My Custom Strategy" }
func (s *MyStrategy) Description() string { return "Enters when RSI(14) < 30 and Close > SMA(200)." }

func (s *MyStrategy) DefaultConfig() StrategyConfig {
    return StrategyConfig{
        ID:            "my-strategy",
        Name:          s.Name(),
        Description:   s.Description(),
        TargetPct:     1.15,   // +15% profit target
        StopLossPct:   0.93,   // -7% stop loss
        HoldingWindow: 10,     // 10-day max holding
        PositionCap:   5,      // Max 5 positions
        AllocationPct: 0.20,   // 20% equity per position
        SlippagePct:   0.0005, // 0.05% slippage
    }
}

func (s *MyStrategy) Validate() error {
    return ValidateConfig(s.DefaultConfig())
}

func (s *MyStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
    var signals []models.Signal

    for sym, bars := range barsBySymbol {
        if len(bars) < 200 { continue }
        rsi := CalcRSI(bars, 14)
        sma := CalcSMA(bars, 200)

        for i := 200; i < len(bars); i++ {
            if rsi[i] < 30.0 && bars[i].Close > sma[i] {
                signals = append(signals, models.Signal{
                    Idx: bars[i].Idx, Symbol: sym, Date: bars[i].Date,
                    Close: bars[i].Close, BuyLimit: bars[i].Close,
                    OrderType: "limit", Entry: 1,
                })
            }
        }
    }

    sort.Slice(signals, func(i, j int) bool { return signals[i].Date < signals[j].Date })
    return signals
}
```

Recompile with `make build` and run `./bin/backtest -strategy my-strategy`!

See [`docs/strategies/writing_a_strategy.md`](file:///Users/darianhickman/Documents/backtestgosqlite/docs/strategies/writing_a_strategy.md) for full guide.

---

## 📊 Sample Output: Quantitative Tear Sheet

```
========================================================================================
📊 QUANTITATIVE PORTFOLIO TEAR SHEET: DONCHIAN 20-DAY MOMENTUM BREAKOUT
📅 BACKTEST TIME WINDOW: 2020-01-02 ➔ 2024-01-02 (4.0 Years | 1008 Trading Days)
========================================================================================
+----------------------------+--------------------------+----------------------------------------+
|           METRIC           |          VALUE           |          BENCHMARK / CONTEXT           |
+----------------------------+--------------------------+----------------------------------------+
| Backtest Time Window       | 2020-01-02 to 2024-01-02 | 1008 trading days (4.0 years)          |
| Initial Capital            | $100000.00               | Starting portfolio cash                |
| Ending Total Equity        | $148320.15               | Cash + open positions                  |
| Net Realized Profit        | $48320.15                | 48.32% total return                    |
| CAGR (Annualized Return)   | 10.35%                   | Compound Annual Growth Rate            |
| Sharpe Ratio (Annualized)  |                     1.15 | Risk-adjusted return vs. 0% Rf         |
| Sortino Ratio (Annualized) |                     1.82 | Downside volatility adjusted           |
| Calmar Ratio               |                     0.85 | CAGR / Max Drawdown                    |
| Omega Ratio                |                     1.38 | Gain-to-loss probability ratio         |
| Ulcer Index                |                     3.12 | Depth & duration of drawdowns          |
| 🔴 MAX DRAWDOWN (MDD %)    | 12.18%                   | Worst account decline from peak equity |
| 🔴 MAX DRAWDOWN ($ LOSS)   | -$14250.00               | Peak: $117000 ➔ Trough: $102750        |
| 🔴 MAX DRAWDOWN DATES      | 2022-04-12 ➔ 2022-09-20  | Longest drawdown duration: 112 days    |
| Total Completed Trades     |                       64 | 41 Wins / 23 Losses                    |
| Trade Win Rate             | 64.06%                   | Pct of closed trades in profit         |
| Profit Factor              |                     2.31 | Gross Profits / Gross Losses           |
| Win / Loss Payoff Ratio    |                     1.30 | Avg Win $ / Avg Loss $                 |
| Average Win                | $2140.50                 | Per winning trade                      |
| Average Loss               | $1646.50                 | Per losing trade                       |
| Average MAE (Drawdown)     | -3.42%                   | Max Adverse Excursion during trade     |
| Average MFE (Runup)        | 8.19%                    | Max Favorable Excursion during trade   |
| Average Holding Period     | 11.4 days                | Holding horizon                        |
| Total Commissions & Fees   | $12.45                   | Exchange / broker costs deducted       |
+----------------------------+--------------------------+----------------------------------------+
```

---

## 📁 Repository Structure

```
backtestgosqlite/
├── Makefile                          # Root automation (build, list, backtest, download, livescan, ui, study, example-csv)
├── README.md                         # Main documentation
├── Comparison.md                     # Performance & architecture comparison
├── ARCHITECTURE_PROPOSALS.md         # Future architecture notes
│
├── cmd/                               # CLI executable entrypoints (one dir per binary)
│   ├── backtest/                     # Multi-strategy backtester & tear sheet CLI
│   ├── download/                     # Multi-source data loader (CSV, Yahoo, Stooq, Polygon)
│   ├── livescan/                     # Live today/tomorrow signal scanner
│   ├── ui/                           # Local web dashboard server (legacy dataset, see caveat above)
│   ├── gridsearch/                   # Multi-core parameter optimization sweep
│   ├── scoreboard/                   # Cross-strategy leaderboard compiler
│   ├── study/                        # Ad-hoc statistical study runner
│   ├── export_studies/               # SP500/ETF study matrix export
│   ├── etf_decision_trees/           # Per-ETF CloudForest decision-tree fitting
│   ├── etf_universe/                 # Polygon.io ETF ticker universe discovery
│   ├── ticker_scan/                  # Pattern-generalization scan across symbols
│   ├── compare_annual_report/        # Standalone vs. shared-account annual comparison
│   ├── audit_shared/                 # Shared-account SQL audit/diagnostic
│   ├── candlesticks/                 # Go-ECharts candlestick visualizer
│   ├── granger_chart/                # Granger-causality tutorial dashboard
│   └── dataflare/                    # Dataflare SQLite GUI launcher
│
├── pkg/                               # Modular core Go packages
│   ├── models/                       # Domain types (Bar, Signal, Position, Trade, Report)
│   ├── datasource/                   # Pluggable data layer (CSV, Yahoo, Stooq, SQLite)
│   ├── strategy/                     # Strategy registry, indicators (RSI, BB, MACD, Donchian, ATR) & all Go strategies
│   ├── simulator/                    # Portfolio ledger, execution models, sizing, concurrent runner
│   ├── analytics/                    # Performance metrics (Sharpe, Sortino, Calmar, Omega, Ulcer, Alpha/Beta) & HTML reports
│   ├── charting/                     # Reusable Chart.js HTML report components
│   ├── storage/                      # SQLite WAL helpers, bar loading, signal/trade persistence
│   ├── runner/                       # Shared strategy-execution/coverage helpers used by backtest, gridsearch & scoreboard
│   ├── signals/                      # SQL-driven entry-signal detection (sql/signals/*.sql)
│   ├── study/                        # Registered ad-hoc statistical studies (run via cmd/study)
│   └── cliutils/                     # Small shared CLI helpers (e.g. default market DB resolution)
│
├── sql/                               # SQL pipeline strategies & signals
│   ├── strategies/                   # Auto-discovered SQL strategy pipelines (one dir per strategy) + authoring README
│   └── signals/                      # Reusable SQL signal fragments (e.g. consecutive_rally.sql)
│
├── docs/                              # In-depth guides & strategy specs
│   ├── architecture.md, SQLvsGOModels.md, TechProgress.md, FinancialProgress.md
│   └── strategies/                   # Per-strategy write-ups & the "writing a strategy" tutorial
│
├── python/                            # Auxiliary Python tooling (e.g. deap_optimizer.py for genetic parameter search)
├── config/                            # Local config (config.example.json; real config.json is git-ignored)
├── data/                              # Local SQLite caches & symbol lists (all *.db git-ignored — see README.md in this dir)
├── reports/                           # Generated backtest/study/gridsearch results & HTML dashboards (git-ignored)
├── bin/                               # Compiled binaries (git-ignored)
├── _archive/                          # Retired strategies/studies excluded from the build (leading underscore)
│
└── examples/                          # Standalone runnable examples
    └── custom_csv_backtest/          # CSV ingestion and backtest walkthrough
```

---

## 🛠️ Tech Stack

- **Language & Runtime**: Go (1.18+)
- **Database**: SQLite 3 with Write-Ahead Logging (WAL)
- **Market Data Feeds**: Standard CSV Import, Yahoo Finance Chart API, Stooq
- **Broker Execution**: Alpaca Trade API Go SDK
- **Frontend**: HTML5, Vanilla CSS, Vanilla JavaScript, Chart.js, Tablewriter

---

## 🔮 Future Architecture Proposals

For future improvements regarding report organization, historical data storage, and signal sharing with external execution suites (e.g., `trading_schwab`), please see [ARCHITECTURE_PROPOSALS.md](ARCHITECTURE_PROPOSALS.md).
