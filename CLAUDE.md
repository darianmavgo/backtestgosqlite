# backtestgosqlite (BT)

Go backtester. Bars, signals, trades and reports live in SQLite. Module `github.com/darianmavgo/backtestgosqlite`, pure-Go SQLite (`modernc.org/sqlite`, no cgo).

The cross-repo rules (ownership, one implementation for CLI/web/GAE, naming, no mocks) are in `../CLAUDE.md` and apply here in full. This file holds only what is specific to BT. Read `README.md` for command usage and `Architecture.md` for the package map; do not duplicate them here.

## Commands

Run from the repo root so `.env` and default paths resolve.

```bash
make build            # every cmd/* into bin/
make test             # go test ./pkg/... ./cmd/...
go build ./... && go vet ./... && go test ./...   # required before saying it works
go test ./pkg/strategy -run TestName              # one test
./bin/backtest -list                              # registered strategy ids
```

- Flags go before positional args (Go `flag` stops at the first bare arg). A subcommand is arg 1: `backtest stack-eval -primary ...`.
- `data/market_history.db` (table `backtest_start`) and `refdata/settings.db` are real data. Tests and experiments must not write to them; use `t.TempDir()` databases or `reports/`.
- Paths come from `pkg/appenv` (e.g. `APP_FOLDER`). Do not hardcode `data/...` in Go; call `appenv.MarketDB()` / `cliutils.GetDefaultMarketDB`.

## Layout rules

- `cmd/<name>/main.go` is a thin main that calls `pkg/<name>.Main` / `Run`. Flags, defaults and logic live in `pkg/<name>` so tests call `Run` without `os.Args`. Follow `pkg/backtest/main.go` and `pkg/livescan/main.go`.
- `pkg/strategy` holds the `Strategy` interface and registry (`Register`, `RegisterAlias`, `Get`; lookup is case- and punctuation-insensitive). Keys differ by `-`/`_`, so do not add a second id that normalizes to an existing one.
- Other packages: `simulator` (portfolio and shared-account), `runner` (load bars, run, stack, stack-eval), `storage` (schemas, `OpenSQLite`, `ExecuteSQLFile`), `refdb` (settings.db), `analytics` (metrics, HTML tear sheet), `calendar`.

## Adding or changing a strategy

1. Implement `strategy.Strategy` in `pkg/strategy/<id>.go` and register it (see `price_action_reclaim.go` for the newest example).
2. Put the signal math in `sql/strategies/<id>/` as ordered files (`01_schema.sql`, `02_calc_....sql`, ...), one stage per file, each writing a slice table. `GenerateSignals` runs them in name order.
3. The output table needs `idx, symbol, date, open, high, low, close, volume, buylimit, entry`; rows with `entry = 1` become entries. See `sql/strategies/README.md`.
4. Add a test using a real temp SQLite DB with literal bars. No mocks or fakes (`mockStrategy` in `pkg/simulator/shared_account_test.go` is a known leftover; do not extend it).
5. If the Go side needs a new indicator, write it as SQL instead of extending `strategy/indicators.go` or `decisiontree.go`.

SQL files are embedded via `sql/embed.go`; rebuild after editing them.

## Rule 5: Signals use absolute prices

In `models.Signal`, `TakeProfit` and `StopLoss` are absolute dollar prices (`150.00`), never multipliers (`0.95`) or percentages (`0.10`). A percentage passed as a price triggers instantly on the next tick. To use the configured fallback (`TakeProfitPct`, etc.), leave the field `0`; the simulator applies it to the real entry price.

## Data gotchas

- Daily simulation reads rows with `length(Date) = 10`; minute bars share the same table and are skipped.
- Result DBs are written per run: `reports/<id>.db`, then `<id>_2.db`, `<id>_3.db`. Do not assume a fixed filename.
- `strateval`'s ledger path ignores `ReportFile`: `STRATEGIES_DB`, then `STRATEVAL_DB`, then `<module root>/reports/strategies.db`.
- `*.db`, `bin/` and `reports/` are gitignored. Do not commit databases or binaries.

## Before you finish

Run the checklist in `../CLAUDE.md`, plus: strategy ids registered and listed by `backtest -list`; any new env var or flag is in `README.md`; `TakeProfit`/`StopLoss` are absolute or `0`.
