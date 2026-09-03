# 🔮 Future Architecture Proposals

This document outlines structural improvements and best practices to evolve `backtestgosqlite` into a cleaner, more robust, and highly integrated multi-project ecosystem.

## 1. Report Organization 📊

Currently, the system generates dozens of HTML and CSV reports (e.g., `comparison_report.html`, `backtest_report.html`) directly into `reports/` or `data/` directories, leading to clutter and potential overwrites.

### Recommendations:
* **Date-Partitioned Subdirectories:** Alter the reporting engine (in `internal/analytics/html_report.go` and various `cmd/` entrypoints) to automatically nest output files.
  * Example: `reports/2024-05-12/backtest_donchian_breakout.html`
* **Dynamic Report Serving (Preferred):** Move away from static `.html` generation. Persist backtest results (equity curves, metrics, trades) into a dedicated SQLite schema (e.g., `reports.db`). Extend the local web UI (`cmd/ui/`) to render these historical backtest results dynamically by reading from the DB.
* **Unified Master Dashboard:** Implement a master index page in the local UI that queries all past backtest runs, allowing side-by-side comparison without running new static scripts.

## 2. Historical Market Data Storage 🗄️

Presently, downloading data often results in redundant network calls or duplicate SQLite insertions if not managed carefully.

### Recommendations:
* **Centralized `master_data.db`:** Establish a single, canonical SQLite database for OHLCV data.
* **Incremental Delta Updates:** Modify `cmd/download` to inspect `master_data.db` for the most recent `Date` per `Symbol`. Instead of downloading a fixed time window (e.g., 2020-2024), fetch only the delta (e.g., `last_date` to `today`) and append.
* **Symbol Metadata Table:** Maintain a `universe` table tracking asset classes, last updated timestamps, and valid trading hours to optimize data refresh cron jobs.
* **Vacuum and Optimize:** Ensure `PRAGMA optimize` and `VACUUM` are run periodically on this central database to maintain high-speed sequential read access.

## 3. Signal Sharing with External Projects 🚀

To bridge the gap between `backtestgosqlite` (the intelligence engine) and `trading_schwab` / `ibkr_personal` (the execution engines):

### Recommendations:
* **Option A: The Shared SQLite "Mailbox" (Easiest)**
  * **How:** `backtestgosqlite` writes generated signals (Symbol, Action, Price, Size) into a shared SQLite database (e.g., `live_signals.db`) configured with Write-Ahead Logging (WAL).
  * **Consumption:** `ibkr_personal` (bot) and `trading_schwab` run polling loops on `live_signals.db` to pick up unexecuted signals. Once executed, they update the signal row with a `status='EXECUTED'`.
* **Option B: Lightweight Go REST / gRPC API (Most Robust)**
  * **How:** Run a lightweight Go HTTP server within `backtestgosqlite` (or a sidecar process) exposing a `/api/v1/signals/pending` endpoint.
  * **Consumption:** External execution scripts poll this API. This abstracts away the database layer and allows execution bots to live on entirely different machines.
* **Option C: Message Queue (Redis / NATS)**
  * **How:** Broadcast live signals to a local NATS or Redis pub/sub topic. Execution bots subscribe to these topics for immediate, sub-millisecond execution.
