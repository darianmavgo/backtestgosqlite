# Archive

Code kept for historical reference, deliberately **excluded from the Go build**
(the leading underscore on this directory name is a standard Go tooling
convention — `go build ./...`, `go vet ./...`, and `go test ./...` all skip any
path with a `_`- or `.`-prefixed component). Nothing in here is registered,
compiled into any binary, or runnable as-is.

## Archived: Whitings Creek (`wc`, `wc-4d`, `whitings_creek-sql`)

**Archived:** 2026-09-14

**What it was:**
- `wc` — "Whitings Creek Baseline": enters when price closes 10% below the
  trailing 10-day low (day -3 to -12), +20% target / -7% stop.
- `wc-4d` — "Whitings Creek (4-Day Time Exit)": same entry signal, exits
  unconditionally after 4 trading days (no stop/target).
- `whitings_creek-sql` — the same strategy as a 25-stage SQL pipeline
  (`sql/strategies/whitings_creek/`), auto-registered from that directory.

**Why archived, not just left alone:**
1. In `cmd/gridsearch`'s multi-strategy batch mode, `wc`/`wc-4d` don't
   implement a custom `ParameterSpace()`, so they fall back to gridsearch's
   generic parameter-space heuristic — which doesn't exercise their real
   signal logic at all. The result: `wc`, `wc-4d`, and `whitings_creek-sql`
   converged to the *exact same* best config (`VOO/2d/15d/+8%-7%/All Regimes`,
   Score=0.0149, Calmar=0.71) as bb-capitulation, macd-crossover, trend-bb,
   donchian-breakout, and half a dozen other strategies in the registry —
   i.e. gridsearch was testing the same generic proxy heuristic for all of
   them, not anything distinguishing about Whitings Creek specifically.
2. That same generic-fallback signal generator is expensive per config
   (see the resilience-tuning work on `cmd/gridsearch` earlier in this
   project) — each of these three took 2-4 minutes to sweep, for a result
   that wasn't telling you anything about the strategy itself.
3. Calmar 0.71 / resilience score 0.0149 put it firmly at the bottom of the
   registry's rankings regardless.

## Restoring

1. Move the files back:
   ```
   git mv _archive/pkg/strategy/whitings_creek.go pkg/strategy/
   git mv _archive/pkg/strategy/wc_4day_hold.go pkg/strategy/
   git mv _archive/sql/strategies/whitings_creek sql/strategies/
   git mv _archive/docs/strategies/whitings_creek.md docs/strategies/
   ```
2. `go build ./...` — `init()` in each file re-registers it automatically.
3. Re-add the `wc` / `wc-4d` bullet to `README.md`'s strategy library section
   (removed when archived) and the `whitings_creek/` example back into
   `sql/strategies/README.md` if you want it as the authoring-guide example
   again.
