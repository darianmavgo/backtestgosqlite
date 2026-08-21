# backtestgosqlite: Automated Algorithmic Trading & 2-Tier Backtesting Engine

[![Go Version](https://img.shields.io/badge/Go-1.18+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Architecture](https://img.shields.io/badge/Architecture-2--Tier%20Hybrid%20Engine-brightgreen)]()

**`backtestgosqlite`** is a production-grade quantitative trading and backtesting system engineered to capture high-probability mean-reversion bounces on leveraged ETFs and high-beta equities.

It features a unique **Two-Tier Hybrid Architecture**: combining the raw speed of **relational SQL vectorization** for universe-wide screening with the precision of an **event-driven chronological portfolio simulator** for real-world risk, cash ledger management, and path-dependent stop-loss validation.

---

## 🏛️ System Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          1. DATA INGESTION ENGINE                           │
│  - Yahoo Finance Chart API + Stooq CSV fallback                             │
│  - 4+ years of multi-asset daily OHLCV bars stored in SQLite (WAL mode)     │
└──────────────────────────────────────┬──────────────────────────────────────┘
                                       │
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│              2. TIER 1: RELATIONAL SQL SCREENING (SQLite Pipeline)          │
│  - 25-Stage Pipeline: Trailing lows, entry feasibility, forward lookbacks   │
│  - Multi-asset batch processing across 5,000+ ticker-years in seconds       │
│  - Generates wc_summary and filters candidates into settings.db             │
└──────────────────────────────────────┬──────────────────────────────────────┘
                                       │ Raw Triggers & Slices
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│              3. TIER 2: CHRONOLOGICAL PORTFOLIO SIMULATOR (Go)              │
│  - Real-time Cash Ledger & Account Equity Curve                             │
│  - Position Limits (e.g., max 5 concurrent positions, 20% allocation each)  │
│  - Dual-Barrier Path Checking (Stop-Loss vs. Profit Target vs. Time-Up)     │
│  - Slippage & Exchange/Broker Fee Deductions                                │
│  - Quantitative Tear Sheet: Sharpe, Sortino, Max Drawdown, CAGR, Payoff    │
└──────────────────────────────────────┬──────────────────────────────────────┘
                                       │ High-Confidence Verified Parameters
                                       ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                   4. LIVE EXECUTION & GAE DEPLOYMENT                        │
│  - Google App Engine (GAE) cron endpoints scheduled for daily market tasks  │
│  - GCS SQLite state persistence across stateless instances                  │
│  - Alpaca Trading API Go SDK: automated limit entries and stop exits        │
│  - Gmail SMTP alerts for entries, exits, and signal notifications           │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## 🚀 Key Capabilities

### 1. Two-Tier Backtesting Engine
* **Tier 1 (SQL Relational Screening)**:
  - Complex lookback calculations computed in SQLite using window functions and indexed joins.
  - Trailing 10-day low lookback window (`Day -3` to `Day -12`).
  - Forward-looking slices for $+3\%$, $+5\%$, and $+20\%$ targets over 4-day and 10-day holding horizons.
  - Output tables: `wc_summary` and `backtested_win_20_10d`.
* **Tier 2 (Portfolio Simulation & Risk Analytics)**:
  - **Capital & Position Constraints**: Simulates trading with finite starting capital (e.g. `$100,000`), enforcing max concurrent positions (e.g. 5 positions) and cash availability.
  - **Path-Dependent Dual-Barrier Checking**: Unlike basic forward scans that only check if a high was touched, Tier 2 walks day-by-day to verify if a stop-loss (e.g. $-7\%$) was triggered *before* the $+20\%$ target was achieved.
  - **Execution Friction Modeling**: Deducts configurable slippage (e.g. $0.05\%$) and broker/regulatory commission fees per share.
  - **Quantitative Tear Sheet**: Computes Sharpe Ratio, Sortino Ratio, Max Drawdown (MDD) & duration, CAGR, Win Rate, Profit Factor, Win/Loss Payoff Ratio, and Average Holding Days.
* **Single-Symbol or Universe Filtering**:
  - Run universe-wide or isolate specific tickers (e.g. `-symbol DFEN`, `-symbol SOXL`) to inspect trade-by-trade logs.

### 2. Multi-Source Historical Data Ingestion (`cmd/download`)
* Ingests 4+ years of daily OHLCV bars for target universes (leveraged ETFs, momentum equities).
* Primary ingestion via **Yahoo Finance v8 Chart API** with automatic fallback to **Stooq CSV feeds**.
* High-speed SQLite transaction batching with rate-limiting.

### 3. Live & Paper Automated Trading (`cmd/server`)
* **Alpaca API Go SDK Integration**: Submits buy limit orders, tracks fills, cancels unfilled orders, and executes limit/stop exits.
* **Google App Engine (GAE) Runtime**: Server endpoints triggered via `cron.yaml` for pre-market scans, order execution, and post-market reconciliations.
* **Google Cloud Storage (GCS) State Sync**: Seamlessly syncs SQLite state databases (`entries.sqlite`, `exits.sqlite`, `settings.db`) between GCS and local memory on stateless cloud instances.
* **Email Alerting**: Instant notification of order entries, exits, and signals via Gmail SMTP.

### 4. Interactive Local Web Dashboard (`cmd/ui`)
* Web interface running on port `8080` (or `8085`).
* Visualizes symbol performance, signal details, database statistics, and triggers backtest runs from the browser.

### 5. Unified Strategy & Single Source of Truth
* Backtest and Live Scanner both reference centralized parameter structs in [`internal/strategy/whitings_creek.go`](file:///Users/darianhickman/Documents/backtestgosqlite/internal/strategy/whitings_creek.go), eliminating parameter drift between backtesting and deployment.

---

## 📁 Repository Structure

```
backtestgosqlite/
├── Makefile                       # Root automation (build, backtest, download, ui, server)
├── README.md                      # Project documentation
├── Comparison.md                  # Comprehensive Backtrader vs. backtestgosqlite comparison
│
├── cmd/                           # Application Entrypoints
│   ├── backtest/main.go           # 2-Tier Backtester & Quantitative Tear Sheet CLI
│   ├── download/main.go           # Historical Market Data Downloader
│   ├── server/main.go             # GAE / Live Trading HTTP Server
│   └── ui/main.go                 # Local Web Dashboard UI Server
│
├── internal/                      # Core Reusable Go Packages
│   ├── models/models.go           # Domain Entities (Bar, Signal, Trade, Position, Report)
│   ├── strategy/whitings_creek.go # Unified Strategy Parameters (WhitingsCreekParams)
│   ├── simulator/                 # Portfolio Cash Ledger & Path-Dependent Stop Engine
│   │   ├── portfolio.go           # Chronological multi-asset event simulator
│   │   └── stoploss.go            # Dual-barrier trade evaluator (MAE/MFE)
│   ├── analytics/metrics.go       # Quant Metrics (Sharpe, Sortino, Drawdown, CAGR)
│   └── storage/sqlite.go          # High-Performance SQLite WAL database helpers
│
├── sql/                           # Structured SQL Pipeline
│   ├── 01_schema/                 # DDL Schemas & Table Index Definitions
│   ├── 02_pipeline/               # 25-Stage Sequenced Pipeline (01_..., 02_...)
│   └── seeds/                     # Symbol Lists & Static Universes
│
├── data/                          # Local SQLite Databases (.gitignore)
│   ├── settings.db                # Master settings & filtered high-confidence symbols
│   ├── wc_master_backtest.db      # Master 4-year multi-asset database
│   └── leveraged_backtest.db      # Leveraged ETF research database
│
├── configs/                       # Application configurations (config.json)
├── deploy/gae/                    # Cloud Deployment (app.yaml, cron.yaml, cron_paper.yaml)
├── scripts/                       # Shell automation scripts (backtest_all.sh)
└── web/                           # Dashboard UI static assets (index.html)
```

---

## ⚡ Quickstart Guide

### 1. Build All Binaries
Compile all tools into `bin/` using the root Makefile:
```bash
make build
```

### 2. Run Unit Tests
```bash
make test
```

### 3. Download Historical Data (4 Years)
Download daily bars for leveraged ETFs into the target database:
```bash
# Standard download (Top 50 leveraged ETFs)
make download

# Or custom universe download:
./bin/download -table leveraged_etf -limit 50 -years 4 -db data/wc_master_backtest.db
```

### 4. Run Backtesting & Portfolio Simulation

#### Run High-Performance BB-Capitulation Strategy (Top Performer: +173.4% Return):
```bash
./bin/backtest -strategy bb-capitulation -capital 100000
```

#### Run Trend-Gated Strategy (Lowest Drawdown: 21.7% MDD, 1.57 Profit Factor):
```bash
./bin/backtest -strategy trend-bb -capital 100000
```

#### Run Baseline Whitings Creek (WC):
```bash
./bin/backtest -strategy wc -capital 100000 -target 1.20 -stoploss 0.93
```

#### Single Ticker Backtest (e.g. SOXL, TQQQ, DFEN):
```bash
./bin/backtest -strategy rsi2 -symbol SOXL -capital 100000
```

### 5. Multi-Strategy Comparative Benchmark
Run all 4 strategies side-by-side on the full universe or single ticker:
```bash
# Universe-wide benchmark
make compare
# or:
./bin/compare -db data/wc_master_backtest.db -capital 100000

# Single symbol benchmark (e.g. SOXL, TQQQ, DFEN)
./bin/compare -db data/wc_master_backtest.db -symbol SOXL
```

### 6. Launch the Local Web Dashboard
```bash
make ui
# Open http://localhost:8080 in your browser
```

### 6. Full Automated Workflow (Download ➔ Backtest ➔ Update Settings)
```bash
./scripts/backtest_all.sh
```

---

## 📈 Strategy Specifications: Whitings Creek (WC)

The core Whitings Creek strategy identifies oversold short-term capitulation events followed by sharp mean-reversion bounces:

| Parameter | Default Value | Description |
| :--- | :---: | :--- |
| **Cliff Drop Ratio** | `0.90` ($-10\%$) | Asset must close $\ge 10\%$ below its trailing minimum low |
| **Lookback Window** | `3` to `12` days | Historical low reference period (`Day -3` to `Day -12`) |
| **Entry Rule** | Limit Buy at Close | Limit order placed at or below signal-day closing price |
| **Profit Target** | `1.20` ($+20\%$) | Take-profit limit exit target |
| **Stop-Loss Floor** | `0.93` ($-7\%$) | Protective stop-loss exit |
| **Max Holding Period**| `10` trading days | Time-up market exit if neither target nor stop is hit |
| **Confidence Threshold** | `90%` win rate | Minimum historical win rate required for live qualification |
| **Sample Size Floor** | $\ge 5$ signals | Minimum historical trade occurrences required |

---

## 📊 Sample Output: Quantitative Tear Sheet

```
==================================================================
📊 QUANTITATIVE PORTFOLIO TEAR SHEET (TIER 2 SIMULATION)
==================================================================
+----------------------------+------------+----------------------------------+
|           METRIC           |   VALUE    |       BENCHMARK / CONTEXT        |
+----------------------------+------------+----------------------------------+
| Initial Capital            | $100000.00 | Starting portfolio cash          |
| Ending Total Equity        | $102435.27 | Cash + open positions            |
| Net Realized Profit        | $2435.27   | 2.44% total return               |
| CAGR (Annualized Return)   | 0.61%      | Compound Annual Growth Rate      |
| Sharpe Ratio (Annualized)  |       0.24 | Risk-adjusted return vs. 0% Rf   |
| Sortino Ratio (Annualized) |       0.41 | Downside volatility adjusted     |
| Max Drawdown (MDD)         | 4.16%      | Longest DD: 552 days             |
| Total Completed Trades     |          4 | 2 Wins / 2 Losses                |
| Trade Win Rate             | 50.00%     | Pct of closed trades in profit   |
| Profit Factor              |       1.71 | Gross Profits / Gross Losses     |
| Win / Loss Payoff Ratio    |       1.71 | Avg Win $ / Avg Loss $           |
| Average Win                | $2923.93   | Per winning trade                |
| Average Loss               | $1706.09   | Per losing trade                 |
| Average Holding Period     | 4.2 days   | Max window: 10 days              |
| Total Commissions & Fees   | $0.43      | Exchange / broker costs deducted |
+----------------------------+------------+----------------------------------+
```

---

## 🛠️ Tech Stack

- **Language & Runtime**: Go (1.18+), Python (historical helpers), Google App Engine (GAE standard)
- **Database**: SQLite 3 with Write-Ahead Logging (WAL)
- **Broker & Market Data**: Alpaca Trade API Go SDK, Yahoo Finance, Stooq
- **Cloud Infrastructure**: Google Cloud Storage (GCS), Google App Engine (GAE) Cron, Gmail SMTP
- **Frontend**: HTML5, Vanilla CSS, Vanilla JavaScript, Chart.js / Tablewriter
