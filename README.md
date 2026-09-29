# backtestgosqlite

Go backtester. Market bars, reference lists, signals, trades, and reports live in SQLite. Build the binaries, then run a command from the repository root so `.env` and the default paths resolve.

```bash
make build          # every cmd/* binary into bin/
make test           # go test ./pkg/... ./cmd/...
make list           # ./bin/backtest -list
```

`go run ./cmd/<name>` is the same program as `./bin/<name>`.

## Paths

`pkg/appenv` reads the environment, then the nearest `.env`. A real environment variable wins.

| Variable | Default | Used for |
|---|---|---|
| `APP_FOLDER` | `.` | repository root |
| `APP_DATA` | `data` | market history, under `APP_FOLDER` |
| `APP_REF` | `refdata` | reference DB, under `APP_FOLDER` |
| `APP_REPORTS` | `reports` | result DBs and HTML, under `APP_FOLDER` |
| `POLYGON_API_KEY` | empty | Polygon equity and option downloads, ETF universe |
| `STRATEGY_ALLOWLIST` | empty | `strateval` live-list snapshot |
| `STRATEGIES_DB` or `STRATEVAL_DB` | empty | overrides the strateval ledger path |

Default files:

| Role | Path |
|---|---|
| Market bars | `data/market_history.db`, table `backtest_start` |
| Reference lists and ETF trees | `refdata/settings.db` |
| Per-strategy results | `reports/<id>.db`, then `reports/<id>_2.db`, `reports/<id>_3.db`, … |
| Shared-account results | `reports/shared_<primary>_<secondary>_….db` |
| HTML tear sheet | `reports/backtest_report.html` |

Commands that call `cliutils.GetDefaultMarketDB` (`backtest`, `study`, `livescan`) use `data/market_history.db` when that file exists and `data/leveraged_backtest.db` when it does not. `download`, `gridsearch`, `scoreboard`, and `etf_decision_trees` always default to `data/market_history.db` and create it when they write.

The Go `flag` package stops at the first bare argument. Put flags before a positional id or ticker. A subcommand is argument 1: `backtest stack-eval -primary …`, not `backtest -primary … stack-eval`.

Daily simulations load rows with `length(Date) = 10`. Minute bars stay in the same table and are skipped by the daily bar query.

---

## download

Pull bars into SQLite. Default source is Yahoo, with Stooq as the fallback. Polygon is opt-in.

**Reads:** `refdata/settings.db` when no tickers are passed (table `leveraged_etf`, limit 50) or when `-list` names an `etf_universe` list (`all`, `6yr`, `sweep`).

**Writes:** `data/market_history.db`, table `backtest_start`. Option history uses the same DB, tables `option_contracts`, `option_bars`, `option_expiry_scan`. Missing dates are filled; `-force` replaces the range.

| Flag | Default |
|---|---|
| `-db` | `data/market_history.db` |
| `-settings` | `refdata/settings.db` |
| `-source` | `yahoo` (`polygon`, `polygon-options`, `stooq`) |
| `-target-table` | `backtest_start` |
| `-timeframe` | `1d` |
| `-years` | `4` |
| `-table` | `leveraged_etf` (settings DB symbol table) |
| `-limit` | `50` (only the settings-table fallback) |
| `-otm` | `0,2,5` (polygon-options, percent OTM) |
| `-rate` | `5` API calls/minute (polygon-options) |

```bash
./bin/download VOO IEF GLD
./bin/download -symbols PDD -years 6
./bin/download -list 6yr -years 6
./bin/download -source polygon -symbols VOO -timeframe 1m -polygon-key "$POLYGON_API_KEY"
./bin/download -source polygon-options -symbols VOO
```

`-start YYYY-MM-DD` overrides `-years`. `-end` defaults to today.

---

## backtest

Run one strategy, many strategies, or a shared cash account.

**Reads:** market DB (`-db`, table `-table` `backtest_start`). `optimized` also reads `reports/gridsearch.db`. SQL pipelines read `sql/strategies/<id>/` when that directory exists, otherwise the copy embedded in the binary.

**Writes:** `reports/<id>.db` (next free `reports/<id>_N.db` if the name is taken). Tables `signals`, `trades`, `equity_curve`, `performance_summary`, and when relevant `return_breakdown` and `run_metrics`. HTML at `reports/backtest_report.html`. Auto-download (`-auto-download`, default on) writes missing bars into the market DB. Window starts `2021-01-01` (`-start`); earlier bars warm up SMAs only. Capital default `$100,000`. `-download-years` default `5`.

```bash
./bin/backtest -list
./bin/backtest -strategy sig-voo-buy-tecl -capital 100000
./bin/backtest -strategy sig-voo-buy-tecl,mara_tree
./bin/backtest -strategy all
./bin/backtest -primary sig-voo-buy-tecl -secondary mara_tree,pdd_tree -capital 100000
./bin/backtest sig-voo-buy-tecl+mara_tree
```

`-symbol` limits the book to one ticker. `-hold`, `-target`, `-stoploss`, and `-max-positions` override the strategy config when set (non-zero). `-no-reinvest-dividends` pays dividends into cash for total-return strategies. `-force` re-runs strategies that already have a usable result (multi-strategy only). `-signals-only` skips the portfolio sim and scans the live window the same way `livescan` does; it does not stack.

### backtest stale

Print which `reports/*.db` results are stale (unknown strategy, newer market bars, or an edited SQL pipeline). No simulation.

```bash
./bin/backtest stale
```

### backtest optimized

Re-run strategies with the highest-resilience row in `reports/gridsearch.db` (`hold_days` must be set; older rows without it are ignored). No `-strategy` means every registered strategy. A strategy with no usable sweep runs on its own defaults and is named in the output.

```bash
./bin/backtest optimized -strategy sig-voo-buy-tecl
./bin/backtest optimized
```

### backtest stack-eval

Rank registered strategies as idle-cash overlays on one primary, one shared ledger. Rankings go to `reports/stack_eval_<primary>.db` (table `overlay_rankings`). Pairwise runs do not each write a `shared_*.db`. `-persist-best` (default on) writes one `reports/shared_<primary>_<secondaries>_N.db` for the greedy stack.

Default candidates skip `*-sql` twins, `voo-buy-hold`, `genetic-momentum`, `dt_*` trees, and any strategy that does not name its symbols. `-include-dt` adds the top `-dt-top` (15) ETF trees. `-include-universe` adds symbol-scanning strategies. `-stack-depth` (3) is how many complementary overlays are stacked after the ranking.

```bash
./bin/backtest stack-eval -primary sig-voo-buy-tecl
./bin/backtest stack-eval -primary sig-voo-buy-tecl -secondary mara_tree,pdd_tree -persist-best
./bin/backtest stack-eval -primary sig-voo-buy-tecl -include-dt -dt-top 15 -stack-depth 3
```

### backtest covered-call

Hold one underlying (default `VOO`) and sell a monthly call. Prints the comparison on stdout. Does not write a strategy result DB.

**Reads:** equity bars and `option_contracts` / `option_bars` in the market DB. Load the chains first with `download -source polygon-options`.

```bash
./bin/download -source polygon-options -symbols VOO
./bin/backtest covered-call -symbol VOO -otm 2 -commission 0.65 -opt-slip 0.05
```

`-start` defaults to the first rollable month (option history is about two years on the Polygon free tier), not `2021-01-01`.

---

## livescan

Same signal generation as `backtest`, window fixed to the last completed session in the market DB. `ENTER` means a buy for the next session. Refresh failure aborts; it does not scan a stale tip.

**Reads:** market DB, table `backtest_start`. `-bars 0` loads each strategy's minimum history.

**Writes:** `reports/livescan.db` (`livescan_status`, `livescan_signals`). `-json` also prints the scan to stdout for `trade_orchestrator`.

```bash
./bin/livescan -list
./bin/livescan -strategy sig-voo-buy-tecl,mara_tree
./bin/livescan all
./bin/livescan -strategy sig-voo-buy-tecl -json
```

`-auto-download` defaults on. `-download-years 0` derives the window from `-bars` or the strategy minimum.

---

## gridsearch

Sweep hold, take-profit, stop, and the strategy's other axes. One strategy writes HTML. Several strategies share one worker pool and record progress so a rerun skips finished ids.

**Reads:** `data/market_history.db`, table `backtest_start`. `-symbols-from` reads a study DB `etf_compare` view (for example `reports/voo_up3_etf.db`) ranked by `rank_cagr`.

**Writes:** `reports/gridsearch.db` (`gridsearch_runs`, `gridsearch_results`). Single-strategy HTML defaults to `reports/<strategy>_gridsearch.html`. Start date `2021-01-01`. Capital `$100,000`. Allocation `0.65`. Cash yield `0.045`. `-min-trades 5`, `-top 10`. `-max-perms 20000` skips a huge generic grid in multi-strategy mode only. `dt_*` trees are excluded from `-strategy all` unless `-include-dt`.

```bash
./bin/gridsearch -list
./bin/gridsearch -strategy sig-voo-buy-tecl -top 20
./bin/gridsearch -strategy all -no-html
./bin/gridsearch -strategy gld-decline -symbols-from reports/voo_up3_etf.db -top-cagr 10
```

`-force` redoes a strategy already marked done in `gridsearch_runs`.

### gridsearch params

Print the parameter grid. No database and no simulation.

```bash
./bin/gridsearch params sig-voo-buy-tecl
./bin/gridsearch params all
```

### gridsearch stale

Report which completed sweeps are stale. No new sweep.

```bash
./bin/gridsearch stale
```

---

## scoreboard

Backtest every registered strategy that lacks a usable result, then rank them.

**Reads:** `data/market_history.db` (`backtest_start`) and existing `reports/<id>[_N].db`.

**Writes:** per-strategy `reports/<id>[_N].db` for anything it has to run, then `reports/scoreboard.db`. If that file is locked it writes `reports/scoreboard_N.db`. Capital `$100,000`, start `2021-01-01`, download horizon 5 years. No `-db` or `-out-dir` flag; both follow `APP_FOLDER`.

```bash
./bin/scoreboard
./bin/scoreboard -force
./bin/scoreboard compile
./bin/scoreboard status
```

`compile` only reads result DBs. `status` reports whether every registered strategy has a usable result and does not write `scoreboard.db`.

---

## study

Run one research study.

**Reads:** market DB (`-db`).

**Writes:** `reports/<study-id>.db`.

```bash
./bin/study -list
./bin/study -study market_context_20d
./bin/study -study hmm_regime -symbol QQQ
./bin/study -study gain_5pct_frequency
./bin/study -study voo_up3_etf
```

Registered ids: `market_context_20d`, `cluster_5pct`, `googl_market_context`, `gain_5pct_frequency`, `hmm_regime`, `etf_study`, `sp500_lead_lag`, `market_clustering`, `qqq_tqqq_volume`, `voo_up3_etf`, `mara_decision_tree`, `mu_decision_tree`, `march_april_voo_gld_uten`.

`cluster_5pct` and `googl_market_context` need a cluster database supplied in code (`SetClusterDB`). The CLI does not pass one, so those two exit until a caller sets it. `cluster_5pct` also expects `data/gain_5pct_frequency.db` beside the market DB.

---

## strateval

In-sample / out-of-sample ledger. It does not edit `STRATEGY_ALLOWLIST` or live jobs.

**Reads:** `data/market_history.db`, table `backtest_start`. Allowlist from `-allowlist` or `STRATEGY_ALLOWLIST` (comma-separated ids).

**Writes:** `reports/strategies.db` (`strategies`, `strategy_evals`, `deployments`), unless `STRATEGIES_DB` or `STRATEVAL_DB` is set. Scratch artifacts under `reports/strateval_runs/`. Held-out window `-oos-months 12`. Capital `$100,000`. Start `2021-01-01`. Tier A gates: at least 12 OOS trades, win rate 0.55, max drawdown 0.15.

```bash
./bin/strateval -list
./bin/strateval -strategy sig-voo-buy-tecl
./bin/strateval -strategy all -optimize -max-trials 50
./bin/strateval report
./bin/strateval status
./bin/strateval sync-deployed -allowlist "$STRATEGY_ALLOWLIST"
./bin/strateval path
```

`path` prints how to open the ledger. `sync-deployed` snapshots the allowlist into `deployments`. `-sync-deployed` on an eval run does the same snapshot after the eval.

---

## walk_forward

Rolling train / test folds.

**Reads:** `data/market_history.db`, table `backtest_start`.

**Writes:** `reports/walk_forward.db` relative to the current directory (this default does not go through `APP_REPORTS`). Tables `walk_forward_fold` and `walk_forward_summary`. Train 24 months, test 6, step 6. Capital `$100,000`.

```bash
./bin/walk_forward -strategy sig-voo-buy-tecl
./bin/walk_forward -strategy sig-voo-buy-tecl,mara_tree -train-months 24 -test-months 6 -step-months 6
```

## check_overfit

Read a walk-forward DB and print a verdict per strategy: `HOLDS`, `DECAYS`, `CURVE_FIT`, or `INSUFFICIENT`.

**Reads / writes:** `reports/walk_forward.db` (cwd-relative). Adds `check_overfit_gate` when it records the gates. Defaults: `-min-oos-trades 8`, `-trial-cutoff 20`, `-decay 0.25`.

```bash
./bin/check_overfit
./bin/check_overfit -db reports/walk_forward.db -min-oos-trades 8
```

---

## etf_universe

List active US ETFs from Polygon and store them.

**Writes:** `refdata/settings.db`, table `etf_universe`, list `all`. Needs `POLYGON_API_KEY` or `-polygon-key`. Page size `-limit 1000`.

```bash
./bin/etf_universe
```

## etf_decision_trees

Fit a decision tree and a TP/SL/hold grid for each symbol in an ETF list.

**Reads:** `data/market_history.db` and `refdata/settings.db` list `6yr` (`-list all|6yr|sweep`).

**Writes:** `refdata/settings.db` table `etf_dt_strategies` (registered later as `dt_<symbol>`), replacing that table with the merged fit. Skips symbols that already have a row unless `-force`. Capital `$100,000`, allocation `0.65`, yield `0.045`, `-min-trades 15`, prints `-top 40`.

```bash
./bin/etf_decision_trees
./bin/etf_decision_trees -list sweep -min-trades 15 -force
```

---

## audit_shared

Print exit-reason and preemption audits for one shared-account result.

**Reads:** `-db`, or the newest `reports/shared_*.db` by modification time. Stdout only.

```bash
./bin/audit_shared
./bin/audit_shared -db reports/shared_sig-voo-buy-tecl_mara_tree_2.db
```

## dataflare

Open a SQLite file in the Dataflare macOS app. No flags. Pass the DB as the only argument, or pass none to launch the app.

```bash
./bin/dataflare
./bin/dataflare data/market_history.db
./bin/dataflare reports/sig-voo-buy-tecl.db
```
