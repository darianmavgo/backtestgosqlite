# Backtesting Engine Comparison: `backtestgosqlite` vs. `Backtrader`

This document provides a comprehensive technical comparison between the custom backtesting engine implemented in [`backtestgosqlite`](file:///Users/darianhickman/Documents/backtestgosqlite) and the industry-standard [Backtrader](https://www.backtrader.com/) framework. 

Following a major architectural upgrade, `backtestgosqlite` now boasts a fully-featured chronological Portfolio Simulator in Go, dramatically closing the feature gap with Backtrader while retaining its massive SQL-driven speed advantages.

---

## 1. Executive Summary

| Dimension | `backtestgosqlite` (Current v2.0) | `Backtrader` |
| :--- | :--- | :--- |
| **Primary Architecture** | **Two-Tier: Vectorized SQL Pipeline + Go Chronological Simulator** | **Hybrid Event-Driven & Vectorized (Python)** |
| **Execution Model** | SQLite performs bulk signal generation using window functions. Go iterates chronologically over the signals to enforce portfolio constraints. | Python bar-by-bar event loop (`Cerebro`) simulating live broker state transitions. |
| **Core Strength** | **Lightning-fast execution & universe screening** across thousands of symbols without sacrificing portfolio realism. | **Ecosystem maturity**, third-party broker integrations, PyFolio analytics, and native charting. |
| **Universe Handling** | Seamless batch processing natively inside SQLite; the Go engine simply consumes a flat array of resulting signals. | Iterates over data feeds; requires handling multiple concurrent data streams in Python memory. |
| **Portfolio & Cash Tracking** | ✅ **Complete** (Maintains `InitialCapital`, `Cash`, Idle Yield (T-Bills), and tracks the `EquityCurve` chronologically). | ✅ **Complete** (Cash balance, margin, leverage, equity curve, portfolio allocation). |
| **Slippage & Commission** | ⚠️ **Partial** (Basic entry execution logic; currently does not explicitly deduct per-share commissions). | ✅ **Full** (Custom commission schemes, spread, fixed/variable slippage, volume fill limits). |
| **Order Types** | Next-Day Open Market Entries + Fixed Target/Stop Limits. | Market, Limit, Stop, StopLimit, StopTrail, OCO, Bracket, MOC. |
| **Performance Analytics** | ✅ **Extensive** (Sharpe, Max Drawdown, CAGR, Win Rate, Expectancy, Profit Factor) generated via HTML reports. | ✅ **Extensive** (Sharpe, Sortino, Max Drawdown, Calmar, SQN, VWR, PyFolio integration). |

---

## 2. In-Depth Architectural Comparison

### `backtestgosqlite`: The Two-Tier Hybrid
`backtestgosqlite` achieves its performance by splitting the heavy lifting:
1. **Tier 1 (SQL Database Engine):** Calculations requiring complex sliding windows (SMA50, SMA200, trailing minimums) are pushed directly into SQLite via `FetchBars`. This leverages highly optimized C-based database indexing to generate raw candidate signals across the entire stock universe in milliseconds.
2. **Tier 2 (Go Portfolio Simulator):** The `PortfolioSimulator` receives this flat array of signals, sorts them chronologically by date, and walks through time day-by-day. It checks if the portfolio has enough `Cash` to enter, caps maximum concurrent positions, handles conflict resolution (e.g., Short vs. Long on the same day), and precisely tracks Daily Equity and Idle Cash Yields (e.g. 4.5% T-Bills).

**Benefits:**
- **Speed:** By avoiding the heavy Python object overhead of passing millions of candlestick objects through an event bus, the Go engine can run multi-year, multi-asset grid searches in a fraction of the time.
- **Realism:** The chronological Go simulator completely solves the "Independent Trade Fallacy" of previous versions. It accurately enforces capital starvation and path-dependent stop-loss failures.

### `Backtrader`: Python Event-Driven Broker Simulation
Backtrader dispatches data chronologically tick-by-tick or bar-by-bar into an event engine (`Cerebro`). Indicators are vectorized where possible using Pandas/Numpy, but strategy logic and broker operations execute sequentially inside `next()` handlers.

**Benefits:**
- **True-to-life market simulation:** It natively supports complex order routing (like OCO and trailing stops) and precise margin maintenance.
- **Ecosystem:** It hooks effortlessly into standard Python Data Science tooling (Matplotlib, PyFolio, Pandas).

---

## 3. Feature-by-Feature Matrix

| Capability Area | Feature | `backtestgosqlite` | `Backtrader` | Analysis & Impact |
| :--- | :--- | :---: | :---: | :--- |
| **Data Handling** | Multi-Asset Universe | ✅ Native | ✅ Feed-based | `backtestgosqlite` handles hundreds of symbols instantly via SQL filtering. |
| | Mixed Timeframes | ❌ | ✅ Supported | Backtrader can mix 5m, 1h, and 1D bars in a single strategy. |
| **Order Management** | Order Types | Basic Entry + Limits | Market, Limit, Stop, Bracket, OCO | `backtestgosqlite` simplifies execution to standard EOD / Next Open models. |
| | Volume Fill Limits | ❌ | ✅ Supported | Backtrader can restrict fills to a maximum percentage of bar volume. |
| **Portfolio & Risk** | Account Cash Ledger | ✅ Built-in | ✅ Built-in | Both accurately model finite cash starvation. |
| | Concurrent Position Cap | ✅ Built-in | ✅ Built-in | Both can limit maximum simultaneous trades to control exposure. |
| | Idle Cash Yields | ✅ Built-in | ⚠️ Manual | `backtestgosqlite` uniquely offers native compounding T-Bill yield for uninvested cash (`CashYieldAnnual`). |
| | Short Selling & Margins | ✅ Supported | ✅ Built-in | Both support inverse/short positions (e.g., SPXU). |
| **Analytics & Reporting** | Risk-Adjusted Returns | ✅ Built-in | ✅ Built-in | Both output Sharpe, Max Drawdown, and CAGR. |
| | Visual Trade Plotting | ✅ HTML Dashboards | ✅ Matplotlib | `backtestgosqlite` exports interactive Tailwind/Chart.js HTML tear sheets. |
| **Optimization** | Parameter Grid Search | ✅ Built-in | ✅ Built-in | `backtestgosqlite` leverages Go concurrency for incredibly fast multi-core parameter sweeping (`cmd/gridsearch`). |

---

## 4. Where `backtestgosqlite` Currently Outperforms

1. **Massive Multi-Core Grid Searching**:
   Due to Go's lightweight goroutines and the `gridsearch` runner, testing thousands of parameter permutations (Stop Loss, Profit Target, Lookback Windows) across a universe of stocks is vastly faster than Python multiprocessing overhead.
2. **Idle Cash Management (T-Bill Yields)**:
   The Go simulator natively understands `CashYieldAnnual` and automatically compounding uninvested cash daily. This is critical for assessing the true opportunity cost of cash-heavy or highly selective strategies (like the VOO-TECL Combo).
3. **Automated HTML Tear Sheets**:
   Instead of generating static Matplotlib `.png` images, the Go engine natively generates portable, interactive HTML reports containing equity curves, drawdown charts, and trade tables that can be instantly hosted or shared.

## 5. Where `Backtrader` Currently Outperforms

1. **Intraday Tick/Minute Granularity**:
   `backtestgosqlite` is optimized for Daily (EOD) data. Backtrader remains superior for high-frequency, minute-by-minute order book simulation.
2. **Complex Order Types & Routing**:
   If a strategy requires modifying trailing stops mid-trade, cancelling and replacing orders on the fly, or using complex OCO (One-Cancels-Other) brackets based on real-time spread changes, Backtrader's broker simulation provides much deeper routing realism.
3. **Margin & Leverage Accounting**:
   While `backtestgosqlite` can trade leveraged ETFs directly (like TECL), it does not natively track the complex borrowing costs, Reg T maintenance margins, or margin-call liquidations required for leveraged margin accounts. Backtrader models these natively.
