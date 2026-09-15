# Architecture

## Layer Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                        cmd/  (entry points)                     │
│   combo/   dip/   gridsearch/   export_studies/   study/        │
│   ─ CLI flags → StrategyConfig                                  │
│   ─ Call storage → Call Strategy.GenerateSignals → Run engine   │
└──────────────────────┬──────────────────────────────────────────┘
                       │
        ┌──────────────┼──────────────────────┐
        ▼              ▼                      ▼
┌───────────────┐ ┌───────────────────┐ ┌──────────────────────┐
│internal/      │ │internal/strategy/ │ │internal/simulator/   │
│storage/       │ │                   │ │                       │
│               │ │ Strategy (iface)  │ │ PortfolioSimulator    │
│ FetchSymbol   │ │ StrategyConfig    │ │   - chronological     │
│  Bars(symbol) │ │                   │ │     event loop        │
│ FetchBarsWithS│ │ sig_voo_buy_tecl.go │ │   - T-bill yield      │
│  MA(symbol)   │ │   GenerateSignals │ │     on idle cash      │
│ FetchBars(...)│ │   DefaultConfig   │ │   - HoldDaysOverride  │
│               │ │                   │ │     per signal        │
│ SQL is the    │ │ sql_strategy.go   │ │   - TP / SL / Time    │
│ ONLY place    │ │   (SQL file path  │ │     barrier exits     │
│ where raw DB  │ │    → signals)     │ │                       │
│ queries live  │ │                   │ │ Returns:              │
│               │ │ gld_decline.go    │ │   PerformanceReport   │
│               │ │   (other strats)  │ │   []Trade             │
│               │ │                   │ │   []DailyEquityPoint  │
└───────────────┘ └───────────────────┘ └──────────────────────┘
        ▲                   │
        │       ┌───────────┘
        │       ▼
┌────────────────────────┐
│internal/models/        │
│                        │
│  Bar      (+ SMA200)   │
│  Signal   (+ Direction │
│            + Regime    │
│            + HoldDays  │
│            + Override) │
│  Trade                 │
│  Position (+ HoldDays  │
│             Override)  │
│  PerformanceReport     │
│  DailyEquityPoint      │
└────────────────────────┘
```

## SQL / Go Boundary

**SQL owns:** a strategy's full signal-generation pipeline, as a directory of sequential `.sql` scripts under `sql/strategies/<id>/`, executed by `strategy.SQLPipelineStrategy` (see `pkg/strategy/sql_strategy.go`). Streak/regime detection and even per-signal TP/SL/hold values live in these scripts — strategy-config values (e.g. `DeclineDays`, `TakeProfitPct`, `StopLossPct`, `HoldingWindow`) are substituted into the SQL text via `__DECLINE_DAYS__`/`__TAKE_PROFIT_MULT__`/`__STOP_LOSS_MULT__`/`__HOLD_DAYS__`-style placeholders rather than being hardcoded literals, so a strategy's own config stays the single source of truth. (An earlier, simpler design kept only date detection in SQL — a single `signal_date` column per `sql/signals/*.sql` file, with everything else in Go — but that path was never wired up and has been removed.)

**Go owns:**
- Regime filtering when it isn't expressed directly in the SQL — `strategy.GenerateSignals()`
- Execution math (fills, P&L, drawdown) — `simulator.PortfolioSimulator`
- Cash yield accrual — `PortfolioSimulator` reads `StrategyConfig.CashYieldAnnual`

## The Sig VOO Buy TECL Strategy (formerly "VOO-TECL")

**File:** [`internal/strategy/sig_voo_buy_tecl.go`](../internal/strategy/sig_voo_buy_tecl.go)

Implements the `Strategy` interface with two legs shared in one `GenerateSignals` call:

| Leg | Signal | Entry | TP | SL | Hold | Regime |
|-----|--------|-------|----|----|------|--------|
| LONG TECL | 3 consecutive VOO down-closes | next open | +5% | none | 8 days | All |
| SHORT SPXU | 3 consecutive VOO up-closes | next open | +6% | -5% | 2 days | VOO < SMA200 |

**Priority rule:** if both conditions fire on the same day, LONG takes priority (can't happen in practice — a down-close can't be an up-close — but the code is explicit).

## Engine: PortfolioSimulator

**File:** [`internal/simulator/portfolio.go`](../internal/simulator/portfolio.go)

- Knows nothing about VOO, TECL, or SPXU
- Iterates `sortedDates` chronologically
- On each date: accrues T-bill yield → evaluates exits → opens new entries
- Exit priority: TP → SL → trailing stop → ATR stop → time barrier
- `HoldDaysOverride` on `Signal` → stored on `Position` → checked against config `HoldingWindow`

## Running Strategies

```bash
# VOO-TECL All-Weather Combo (canonical):
go run cmd/combo/main.go -html reports/combo.html

# Single-leg dip backtest (any symbol/params):
go run cmd/dip/main.go -signal VOO -trade TECL -days 3 -hold 8 -tp 0.05 -alloc 0.65

# Grid search (parallel parameter sweep):
go run cmd/gridsearch/main.go -mode bear -top 20 -html reports/bear_grid.html

# Export all studies to SQLite + HTML:
go run cmd/export_studies/main.go
```

## Adding a New Strategy

1. Create `internal/strategy/my_strategy.go`
2. Implement the `Strategy` interface: `ID()`, `Name()`, `Description()`, `Validate()`, `DefaultConfig()`, `GenerateSignals(barsBySymbol)`
3. Call `Register(s)` in the constructor (or `NewMyStrategy()`)
4. Add a `cmd/my_strategy/main.go` that calls `storage.FetchBarsWithSMA`, `GenerateSignals`, then `simulator.NewPortfolioSimulator`

No changes to the engine needed.
