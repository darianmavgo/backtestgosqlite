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
* **Concurrent Strategy Benchmarking**: Run any number of strategies in parallel across Go goroutines (`make compare`).

### 2. Pluggable Market Data Sources (`cmd/download`)
* **Bring Your Own CSV**: Ingest any standard OHLCV CSV file (from Polygon.io, Alpaca, Interactive Brokers, or manual export) with automatic column header detection.
* **Automated Data Feed Downloads**: Multi-source daily data downloader with Yahoo Finance API v8 and Stooq fallback.
* **SQLite Storage Engine**: High-speed batch insertion into SQLite tables with indexed lookups and WAL concurrency.

### 3. Built-in Technical Indicator Library
Zero external C dependencies. Pure Go vectorized indicator math in [`internal/strategy/indicators.go`](file:///Users/darianhickman/Documents/backtestgosqlite/internal/strategy/indicators.go):
* **Moving Averages**: `CalcSMA`, `CalcEMA`
* **Oscillators**: `CalcRSI` (Wilder's smoothing)
* **Volatility**: `CalcBollinger`, `CalcATR`, `CalcDonchian`
* **Trend & Momentum**: `CalcMACD` (MACD line, Signal line, Histogram)

### 4. Built-in Strategy Library
* **`bb-capitulation`**: Lower Bollinger Band exhaustion pierces with RSI(5) oversold confirmation.
* **`macd-crossover`**: Classic MACD (12, 26, 9) signal-line bullish crossover.
* **`donchian-breakout`**: Turtle-style 20-day high momentum breakout with trailing stop.
* **`trend-bb`**: Macro trend-gated Bollinger dips (Close > SMA50).
* **`rsi2`**: Connors RSI(2) deep pullback strategy.
* **`wc` / `wc-4d`**: Whitings Creek short-term capitulation mean-reversion.
* **`buy-and-hold`**: Benchmark buy-and-hold baseline for computing active Alpha & Beta.

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

### 3. Run Custom CSV Backtest
Ingest your own OHLCV CSV file and run an immediate backtest:
```bash
make example-csv
# or:
./bin/download -csv examples/custom_csv_backtest/sample_stocks.csv -db data/sample.db
./bin/backtest -db data/sample.db -strategy donchian-breakout -capital 50000
```

### 4. Run Strategy Backtests
```bash
# High-Performance BB-Capitulation Strategy
./bin/backtest -strategy bb-capitulation -capital 100000

# MACD Crossover Strategy
./bin/backtest -strategy macd-crossover -capital 100000

# Donchian 20-Day Momentum Breakout with Trailing Stop
./bin/backtest -strategy donchian-breakout -capital 100000

# Single Symbol Filter (e.g. SOXL, AAPL, SPY)
./bin/backtest -strategy bb-capitulation -symbol SOXL -capital 100000
```

### 5. Multi-Strategy Comparative Benchmark
Run all strategies concurrently against your dataset in parallel:
```bash
make compare
# or:
./bin/compare -db data/wc_master_backtest.db -capital 100000
```

### 6. Launch the Local Web Dashboard
```bash
make ui
# Open http://localhost:8080 in your browser
```

### 7. Other Utilities
```bash
# Run Market Scanner (checks latest dates for signals)
./bin/scan_check

# Run Empirical Stop-Loss Probability Analysis (on DFEN)
./bin/stop_prob
```

---

## 🛠️ Writing Your Own Strategy in Go (Under 35 Lines)

Create `internal/strategy/my_strategy.go`:

```go
package strategy

import (
    "sort"
    "github.com/darianmavgo/backtestgosqlite/internal/models"
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
│   ├── backtest/main.go              # Single strategy backtester & tear sheet CLI
│   ├── compare/main.go               # Concurrent multi-strategy benchmark suite
│   ├── download/main.go              # Multi-source data loader (CSV, Yahoo, Stooq)
│   ├── scan_check/main.go            # Market scanner & recent signal detection CLI
│   ├── stop_prob/main.go             # Empirical stop-loss probability analysis CLI
│   ├── ui/main.go                    # Local Web Dashboard UI Server
│   └── server/main.go                # Automated execution HTTP server
│
├── internal/                         # Modular Core Go Packages
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
│   ├── 01_schema/                    # Master database schema DDL
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

## Validation

Four checks compare a live strategy with its backtest. Each one is a `pkg/` function with a thin `cmd/` wrapper, and each writes its own table. Run them from the CLI when you want a report. They are not on the cron schedule. Walk-forward and the curve-fit check live in this repo. Trade reconciliation and the slippage audit live in `trade_orchestrator`.

### Walk-forward

Rolls an in-sample window forward and simulates each out-of-sample window from a flat book. Signals are generated once on the full history. Only that window's dates are simulated. Writes `walk_forward_fold` and `walk_forward_summary`. Returns in those tables are fractions (`0.05` is five percent).

```bash
go run ./cmd/walk_forward -strategy tsll-daily-one-share -market-db data/market.db -table backtest_start -db reports/walk_forward.db -train-months 24 -test-months 6 -step-months 6 -capital 100000
```

| Flag | Default | Meaning |
|---|---|---|
| `-strategy` | required | Strategy id, or a comma-separated list |
| `-market-db` | `MARKET_DB` | Bars SQLite |
| `-table` | `backtest_start` | Bars table |
| `-db` | `reports/walk_forward.db` | Where fold rows and the summary are written |
| `-train-months` | 24 | In-sample window, calendar months immediately before each out-of-sample window |
| `-test-months` | 6 | Out-of-sample window |
| `-step-months` | 6 | How far the next fold moves forward |
| `-capital` | 100000 | Starting capital for each window |

### Curve-fit check

Reads `walk_forward_summary` and writes `check_overfit`. It does not re-simulate. Run `walk_forward` first. Verdicts are `HOLDS`, `DECAYS`, `CURVE_FIT`, and `INSUFFICIENT`.

```bash
go run ./cmd/check_overfit -db reports/walk_forward.db -min-oos-trades 8 -trial-cutoff 20 -decay 0.25
```

| Flag | Default | Meaning |
|---|---|---|
| `-db` | `reports/walk_forward.db` | The database `walk_forward` wrote |
| `-min-oos-trades` | 8 | Fewer out-of-sample trades than this is `INSUFFICIENT` |
| `-trial-cutoff` | 20 | A parameter grid at least this large can be `CURVE_FIT` |
| `-decay` | 0.25 | Out-of-sample Sharpe below this fraction of in-sample Sharpe has collapsed |

`INSUFFICIENT` means too few out-of-sample trades. `CURVE_FIT` means many trials, an in-sample Sharpe above 0.5, and an out-of-sample Sharpe below the decay fraction of in-sample. `DECAYS` means the out-of-sample Sharpe collapsed. `HOLDS` means the out-of-sample Sharpe stays positive and at least half of in-sample.

### Trade reconciliation

In `trade_orchestrator`, `reconcile_trades` rebuilds `reconcile_trade`: one row per account, strategy, symbol, and signal date. The live fill is compared with a one-share replay of that same signal through the backtest's entry and exit rules.

```bash
go run ./cmd/orchestrator reconcile_trades --account 123 --market-db /path/market.db --table backtest_start
```

`--market-db` defaults to `MARKET_DB`. If that file is missing, the live side is still written and `model_outcome` is `NO_BARS`. `--account` limits staged orders and managed trades to one account. Signals that were never staged are still included. `--table` defaults to `backtest_start`.

### Slippage and latency

`audit_slippage` summarizes that ledger into `audit_slippage`: average entry and exit slippage, the worst entry slippage, slippage versus the limit sent, average sessions from the intended session to the fill, and fees. Run `reconcile_trades` first.

```bash
go run ./cmd/orchestrator audit_slippage
```

### How to read a row

- Returns are fractions. `0.05` is five percent.
- `entry_slippage_bps` is positive when the live entry paid more than the model. `exit_slippage_bps` is positive when the live exit sold higher than the model.
- `fill_delay_sessions` counts trading sessions from `next_session` to the entry fill. A negative number means the fill was before that session. A fill price of `0` means the broker price is still unknown.
- Only `mara_tree`, `pdd_tree`, and `sig-voo-buy-tecl` enter on the next session's limit. Every other strategy, including `tsll-daily-one-share`, is replayed on the signal bar, which is how that strategy's backtest fills. The row shows that difference.

### What the live book stores

`manage_exit` keeps the decision key (`strategy_id`, `as_of_date`, `next_session`, the signal ids, and the stage id) plus `entry_fill_price`, `exit_fill_price`, `filled_qty`, `fees`, and `exit_reason_code`. The Schwab order mirror stores `execution_price`. The transaction mirror stores `fees` and `symbol`. Reconciliation copies the broker fill off the order, including a sell nested under the entry, when the managed trade does not already have one. The managed-trades page shows the new columns.

`go build ./...` in this repo still stops on `cmd/backtest`, `cmd/download`, and `internal/storage`. Those files import `internal/` packages that are not in the tree. `./pkg/...`, `cmd/walk_forward`, and `cmd/check_overfit` build.

## 🛠️ Tech Stack

- **Language & Runtime**: Go (1.18+)
- **Database**: SQLite 3 with Write-Ahead Logging (WAL)
- **Market Data Feeds**: Standard CSV Import, Yahoo Finance Chart API, Stooq
- **Broker Execution**: Alpaca Trade API Go SDK
- **Frontend**: HTML5, Vanilla CSS, Vanilla JavaScript, Chart.js, Tablewriter
