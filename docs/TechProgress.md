# Technical Progress & Architectural Milestones (2026)

## Executive Summary

Throughout 2026, **`backtestgosqlite`** has evolved into an **institutional-grade, ultra-high-speed quantitative backtesting and algorithmic trading engine** engineered in compiled **Go** and backed by **SQLite WAL (Write-Ahead Logging)**.

The platform eliminates quant framework bloat by combining Go's compiled speed and native concurrency with SQLite's relational transparency, auditability, and ease of inspection.

---

## 🎯 The Core Mission & Technical Vision

1. **Defeating Quant Tool Bloat with Go + SQLite**:
   * Traditional Python backtesters (Backtrader, Zipline) suffer from interpreter overhead, GIL constraints, and fragile dependency trees.
   * C++ engines are often excessively rigid, slow to prototype, and obscure calculation steps.
   * **Core Thesis**: Pair the raw execution speed and goroutine concurrency of compiled Go with the universal relational storage, querying, and auditing power of SQLite.

2. **Transparent "White-Box" Calculation Pipeline**:
   * Instead of hiding indicator math and trading signals inside ephemeral in-memory objects, every stage of the pipeline produces **inspectable relational SQLite tables** (`_slice` tables, `signals`, `trades`, `equity_curve`, `performance_summary`).
   * If a trade triggers or fails, any quantitative researcher can run a standard SQL query to verify the exact inputs and conditions.

3. **Bridging Historical Backtesting to Live Execution**:
   * Creating a unified pipeline:
     $$\text{Historical Data Ingestion} \longrightarrow \text{Strategy Optimization} \longrightarrow \text{Lead-Lag Macro Studies} \longrightarrow \text{Daily Live Market Scanning}$$

---

## 🏛️ Platform Architecture

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           1. CACHE-FIRST DATA LAYER                             │
│  - data/market_history.db (SQLite WAL)                                          │
│  - cmd/download: Smart gap-detection (fetches only missing dates, 0 duplicates) │
│  - Supports Yahoo Finance API v8, Stooq, and custom high-resolution CSV feeds    │
└────────────────────────────────────────┬────────────────────────────────────────┘
                                         │
                                         ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│               2. DUAL-ENGINE STRATEGY SYSTEM (GO & PURE SQL)                    │
│  - Vectorized Go Strategy Library (RSI, BB, MACD, Donchian, ATR, Streaks)        │
│  - Pure SQL Pipeline Strategies in sql/strategies/ (auto-discovered as *-sql)   │
│  - Strategy Isolation: Each backtest writes to its own reports/<strategy>.db    │
└────────────────────────────────────────┬────────────────────────────────────────┘
                                         │
                                         ▼
┌─────────────────────────────────────────────────────────────────────────────────┐
│             3. CHRONOLOGICAL MULTI-ASSET PORTFOLIO SIMULATOR                    │
│  - Order Models: Market, Limit, Stop-Limit with dynamic path-checking           │
│  - Risk Systems: Trailing Stops, ATR Volatility Stops, Dual-Barrier Exits       │
│  - Position Sizing: Fixed %, Fixed $, Fixed Shares, Kelly Criterion             │
│  - Concurrent Execution: Multi-strategy benchmarking across Goroutines          │
└────────────────────────────────────────┬────────────────────────────────────────┘
                                         │
                 ┌───────────────────────┴───────────────────────┐
                 ▼                                               ▼
┌───────────────────────────────────┐   ┌─────────────────────────────────────────┐
│       4. LIVE SCANNING & UI       │   │     5. MACRO & CAUSALITY STUDIES        │
│ - cmd/livescan: Evaluates live    │   │ - cmd/study: Aligns 1-minute bars into  │
│   market bars & writes actionable │   │   prototyping tables                    │
│   orders to live_scan.db          │   │ - cmd/granger_chart: Go-Echarts         │
│ - cmd/scoreboard: Comparative     │   │   visualizer testing lead-lag causality │
│   leaderboard (Sharpe, Drawdown)  │   │   (VOO -> GLD / UTEN)                   │
└───────────────────────────────────┘   └─────────────────────────────────────────┘
```

---

## 📈 Major Progress & Milestones Achieved in 2026

### 1. Decoupling & Repository Specialization
* **Migrated TWS / Interactive Brokers**: Extracted Interactive Brokers automated trading code into a dedicated repository (`ibkr_personal`), leaving `backtestgosqlite` strictly focused on clean, reproducible quant modeling, backtesting, and market signal detection.
* **Eliminated Deprecated Python Environs**: Removed legacy Python server dependencies in favor of native compiled Go tools and standalone analytical scripts.

### 2. The "SQL-First" Strategy Refactoring
* Identified that early iterations were calculating metrics purely in Go memory without persisting intermediate steps.
* Refactored the strategy library so that every strategy (e.g. `bb_capitulation`, `macd_crossover`, `rsi2_trend`, `trend_bb`, `millwharf`, `voo_tecl_combo`, `voo_tecl_spxu_combo`, `whitings_creek`) can execute either:
  * As compiled **Go code**, or
  * As **sequential SQL pipelines** (`01_schema.sql`, `02_calc_*_slice.sql`, `03_calc_signals.sql`).
* Each strategy now runs in isolated SQLite files (`reports/<strategy>.db`, auto-incrementing to `_2.db`, `_3.db`) with zero lock contention when running in parallel.

### 3. High-Performance Scoreboard & Concurrency (`cmd/scoreboard`)
* Engineered a centralized **Scoreboard** that runs the entire universe of registered strategies concurrently across Go goroutines.
* Produces unified institutional tear sheets comparing **Sharpe Ratio, Sortino Ratio, Calmar Ratio, Win Rate, Profit Factor, Max Drawdown, and Alpha/Beta**.

### 4. Live Signal Scanner (`cmd/livescan`)
* Built a dedicated live market scanner that connects to the latest market data, evaluates active setups across all registered strategies, and outputs categorized signals (`ACTIONABLE_NOW` vs `RECENT_SETUP`).
* Calculates exact limit entry prices, target profit prices, and stop-loss levels, saving results into `live_scan.db` and printing clean CLI tables.

### 5. High-Frequency Lead-Lag & Granger Causality Studies (`cmd/study` & `cmd/granger_chart`)
* **1-Minute Bar Ingestion**: Ingested high-resolution 1-minute historical bars for March–April to study intraday price discovery and market reaction to news.
* **Macro Transmission (VOO &rarr; GLD / UTEN)**:
  * Pivoted 1-minute data across equities (VOO), gold (GLD), and 10-year Treasuries (UTEN).
  * Evaluated **Granger Causality** to see whether S&P 500 shocks lead gold or bond yield movements.
* **Interactive Visualization**:
  * Built native **Go-Echarts** visualization tools (`cmd/granger_chart`) to generate institutional-grade HTML dashboards.
  * Added the **Tutorial Mode** dashboard explaining p-values, lag horizons, and real-world trading rules.

### 6. Codebase Hardening & Refactoring Quality
* Refactored hardcoded database and report paths into configurable CLI flags (`-db`, `-out-dir`).
* Added unit test coverage for position sizers (`FixedSharesSizer`, etc.) and multi-asset combo strategies.
* Cleaned up legacy seed tables, orphaned HTML artifacts, and ensured fast module builds.

---

## 🧭 System Component Status

| Component | Directory / Command | Status | Description |
| :--- | :--- | :---: | :--- |
| **Data Ingestion** | `cmd/download`, `pkg/storage` | 🟢 Complete | Cache-first incremental updates in `data/market_history.db` |
| **Strategy Library** | `pkg/strategy`, `sql/strategies/` | 🟢 Complete | Dual Go & SQL implementations for 10+ standard & custom strategies |
| **Portfolio Simulator** | `pkg/simulator` | 🟢 Complete | Multi-asset chronological simulation with dynamic stops & sizing |
| **Performance Tear Sheets** | `pkg/analytics`, `reports/` | 🟢 Complete | Standalone HTML tear sheets, SQLite result DBs, and Scoreboard |
| **Live Scanner** | `cmd/livescan` | 🟢 Complete | `cmd/livescan` outputs trade-ready orders with targets and stops |
| **Comparative Scoreboard** | `cmd/scoreboard` | 🟢 Complete | Concurrent multi-strategy benchmarking and ranking engine |
| **Microstructure Studies** | `cmd/study`, `cmd/granger_chart` | 🟢 Active | 1-minute Granger lead-lag analytics and volatility spillover modeling |

---

## 🔮 Next Horizons

1. **Intraday Strategy Execution**: Extend the Tier 2 portfolio simulator to ingest high-frequency 1-minute and 5-minute bars alongside daily regimes.
2. **Dynamic Lead-Lag Signals**: Utilize empirical Granger causality findings (e.g. VOO &rarr; GLD at Lags 2–10m) to generate cross-asset predictive indicators for live execution.
3. **Automated Live Pipeline**: Connect `cmd/livescan` to automated alert dispatchers or order queues.
