# SQL pipelines

A strategy is defined in Go (`pkg/strategy`) and calculated here. A folder under `sql/strategies/<id>/` is the signal math for the Go strategy with that id. It is not a second strategy type.

## Directory Structure

1. Register the Go strategy in `pkg/strategy`.
2. Add `sql/strategies/<id>/` with `.sql` files that run in name order (`01_schema.sql`, `02_calc_signals.sql`, …).
3. `GenerateSignals` runs that pipeline when the market and calc databases are set.

A folder with no Go strategy is not registered. `AutoRegisterSQLStrategies` still inserts a `<dir>-sql` lookup for a pipeline that already has a Go owner. That id is the same pipeline. Run the Go id.

```
sql/strategies/
├── streak_strategy/   one pipeline for every streak_strategy row
├── tree_strategy/     one pipeline for every tree_strategy row
├── markov_model/      reads the persisted Markov model (see train)
├── markov_hmm/        reads hmm_regime.db from study hmm_regime
└── voo_up3/           owned by the voo-up3 Go strategy
```

## Contract & Signal Extraction

The backtester looks for an output table containing trade triggers:
- Any table with columns: `idx, symbol, date, open, high, low, close, volume, buylimit, entry`.
- Rows where `entry = 1` are treated as entry signals and routed into the Tier 2 chronological portfolio simulator.
- Standard signal table names checked:
  - `<strategy_id>_signals`
  - `rsi_oversold_signals`
  - `entry`

## Running

```bash
./bin/backtest -list
./bin/backtest -strategy streak-voo-buy-tecl -capital 100000
```
