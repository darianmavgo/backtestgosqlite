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
* **`wc` / `wc-4d`**: Whitings Creek short-term capitulation mean-reversion.
* **`buy-and-hold`**: Benchmark buy-and-hold baseline for computing active Alpha & Beta.

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
│                        5. REPORTING & VISUALIZATION DASHBOARD                          │
│  - Console: Quantitative Tear Sheets & Side-by-Side Comparison Tables                  │
│  - HTML: Interactive Chart.js Dashboards (reports/backtest_report.html)                │
│  - Web UI: Local browser server (make ui -> http://localhost:8080)                     │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

### SQLite Database Files Reference

| Database File | Directory | Primary Role | Schema / Key Tables | Written By | Read By |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **`market_history.db`** | `data/` | **Master OHLCV Bar Cache** | `backtest_start` (OHLCV daily bars: `symbol`, `Date`, `open`, `high`, `low`, `close`, `volume`) | `cmd/download` | `cmd/backtest`, `cmd/livescan`, `cmd/ui`, `cmd/gridsearch` |
| **`<strategy_id>.db`** *(e.g. `bb-capitulation.db`, `_2.db`, `_3.db`)* | `reports/` | **Isolated Backtest Run Results** | `signals`, `trades`, `equity_curve`, `performance_summary` | `cmd/backtest` | External analysis, SQLite CLI, Notebooks |
| **`livescan_signals.db`** *(optional with `-save`)* | `reports/` | **Live Signal Log** | `live_signals` (actionable orders for today/tomorrow) | `cmd/livescan` | Live order execution & alerts |
| **`settings.db`** | `data/` | **Universe & Configuration Seed** | `leveraged_etf`, `momentum_candidates`, `backtested_win_20_10d` | Seed scripts / Admin | `cmd/download`, `cmd/ui` |
| **`sample.db` / `sample_stocks.db`** | `data/` | **Testing & Custom CSV Sandbox** | `backtest_start` | `cmd/download -csv` | `examples/custom_csv_backtest` |
| **`sp500_etfs_study.db`** | `data/` | **Multi-Scenario Study Matrix** | `tecl_allocation_matrix`, `compare_3x_etfs_matrix` | `cmd/export_studies` | Study reports |

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
# Open http://localhost:8080 in your browser
```

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
├── Makefile                          # Root automation (build, backtest, compare, test, ui)
├── README.md                         # Main documentation
├── Comparison.md                     # Performance & architecture comparison
│
├── cmd/                              # CLI Executable Entrypoints
│   ├── backtest/main.go              # Multi-strategy backtester & tear sheet CLI
│   ├── livescan/main.go              # Live signal scanner for today/tomorrow orders
│   ├── download/main.go              # Multi-source data loader (CSV, Yahoo, Stooq)
│   ├── ui/main.go                    # Local Web Dashboard UI Server
│   ├── export_studies/main.go        # Batch CSV export for strategy studies
│   └── gridsearch/main.go            # Multi-core parameter optimization grid search
│
├── pkg/                              # Modular Core Go Packages
│   ├── models/models.go              # Domain types (Bar, Signal, Position, Trade, Report)
│   ├── datasource/                   # Pluggable data layer (CSV, Yahoo, Stooq, SQLite)
│   ├── strategy/                     # Unified strategy registry, indicators & algorithms
│   │   ├── indicators.go             # Pure Go technical indicators (RSI, BB, MACD, Donchian, ATR)
│   │   ├── bb_capitulation.go        # Bollinger Band Capitulation Strategy
│   │   ├── macd_crossover.go         # MACD Bullish Crossover Strategy
│   │   ├── donchian_breakout.go      # Donchian 20-Day Momentum Breakout
│   │   ├── whitings_creek.go         # Whitings Creek Baseline Strategy
│   │   ├── trend_bb.go               # Trend-Gated Bollinger Strategy
│   │   └── rsi2_trend.go             # Connors RSI(2) Strategy
│   ├── simulator/                    # Portfolio ledger, execution models & sizing
│   │   ├── portfolio.go              # Chronological event simulator
│   │   ├── sizer.go                  # Position sizing (Fixed %, Fixed $, Fixed Shares, Kelly)
│   │   └── concurrent.go             # Multi-goroutine concurrent backtest runner
│   ├── analytics/                    # Performance analytics & HTML report generator
│   │   ├── metrics.go                # Sharpe, Sortino, Calmar, Omega, Ulcer, Alpha/Beta
│   │   └── html_report.go            # Interactive HTML report generator
│   └── storage/                      # SQLite WAL database helpers & query engine
│
├── sql/                              # SQL Pipeline Strategies
│   └── strategies/                   # Auto-discovered SQL strategy pipelines
│       ├── README.md                 # SQL strategy authoring guide
│       └── whitings_creek/           # 25-stage relational pipeline
│
├── docs/                             # In-Depth Guides & Strategy Specs
│   └── strategies/                   # Strategy documentation & tutorial
│
└── examples/                         # Standalone runnable examples
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
