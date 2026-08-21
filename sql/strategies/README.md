# SQL Pipeline Strategies Guide

`backtestgosqlite` allows trading strategies to be written entirely in standard SQL queries executed directly inside high-performance SQLite in-memory or WAL databases.

## Directory Structure

To create a new SQL-based strategy:
1. Create a subdirectory under `sql/strategies/<your_strategy_name>/`.
2. Add `.sql` script files that execute sequentially (e.g. `01_schema.sql`, `02_calculate_signals.sql`).
3. The engine automatically discovers all folders in `sql/strategies/` and registers them as CLI strategies named `<your_strategy_name>-sql`.

```
sql/strategies/
├── whitings_creek/
│   ├── 01_calc_minlow_all.sql
│   ├── 02_wc_calc_trailing_minlow.sql
│   └── ...
├── rsi_oversold/
│   ├── 01_schema.sql
│   └── 02_calc_rsi_signals.sql
└── macd_breakout/
    ├── 01_schema.sql
    └── 02_signals.sql
```

## Contract & Signal Extraction

The backtester looks for an output table containing trade triggers:
- Any table with columns: `idx, symbol, date, open, high, low, close, volume, buylimit, entry`.
- Rows where `entry = 1` are treated as entry signals and routed into the Tier 2 chronological portfolio simulator.
- Standard signal table names checked:
  - `rsi_oversold_signals`
  - `wc_backtest_details`
  - `wc_buy_signal_slice`
  - `entry`

## Running Your SQL Strategy

```bash
# List all registered Go and SQL strategies
./bin/backtest -list

# Execute your SQL strategy
./bin/backtest -strategy whitings_creek-sql -capital 100000
```
