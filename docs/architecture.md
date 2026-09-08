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
│ FetchBarsWithS│ │ voo_tecl_combo.go │ │   - T-bill yield      │
│  MA(symbol)   │ │   GenerateSignals │ │     on idle cash      │
│ FetchBars(...)│ │   DefaultConfig   │ │   - HoldDaysOverride  │
│               │ │                   │ │     per signal        │
│ SQL is the    │ │ sql_strategy.go   │ │   - TP / SL / Time    │
│ ONLY place    │ │   (SQL file path  │ │     barrier exits     │
│ where raw DB  │ │    → signals)     │ │                       │
│ queries live  │ │                   │ │ Returns:              │
│               │ │ millwharf.go      │ │   PerformanceReport   │
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

**SQL owns:** date detection only. Every `.sql` file in `sql/signals/` returns one column: `signal_date`.

**Go owns:** everything else.
- Streak detection (e.g. 3 consecutive drops) — `strategy.consecutiveDrops()`
- Regime filtering (e.g. VOO < SMA200) — `strategy.GenerateSignals()`
- TP / SL / hold windows — `StrategyConfig` + per-signal `Signal.HoldDaysOverride`
- Execution math (fills, P&L, drawdown) — `simulator.PortfolioSimulator`
- Cash yield accrual — `PortfolioSimulator` reads `StrategyConfig.CashYieldAnnual`

## The VOO-TECL Strategy

**File:** [`internal/strategy/voo_tecl_combo.go`](../internal/strategy/voo_tecl_combo.go)

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
