# backtestgosqlite

Go backtester. Market bars, reference lists, signals, trades, and reports live in SQLite. Build the binaries, then run a command from the repository root so `.env` and the default paths resolve.

```bash
make build          # every cmd/* binary into bin/
make test           # go test ./pkg/... ./cmd/...
make list           # ./bin/backtest -list
```

`go run ./cmd/<name>` is the same program as `./bin/<name>`. Package map and internals are in [Architecture.md](Architecture.md); rules for editing the code are in [CLAUDE.md](CLAUDE.md).

## What it can do

| Capability | Command |
|---|---|
| Download daily, minute and option bars | `market_history` |
| Build the stock and ETF universe (`refdata/universe.db`) | `universe` |
| Backtest one strategy, many, or a shared-cash stack | `backtest` |
| Stack strategies on one cash ledger with `a+b+c`, with leftover cash parked in a symbol (`park-<symbol>`) | `backtest` |
| Rank strategies as idle-cash overlays and build a greedy stack | `backtest stack-eval` |
| Sweep hold, take-profit and stop, then promote winners to new strategies | `gridsearch` |
| Walk-forward folds plus an overfit verdict per strategy | `walk_forward`, `check_overfit` |
| In-sample / out-of-sample ledger with tiers A to D | `strateval` |
| Rank every registered strategy | `scoreboard` |
| Live signal scan for the next session | `livescan` |
| Research studies (clustering, HMM regimes, lead/lag, decision trees) | `study` |
| Report on an IBKR transaction CSV | `transaction_calc` |
| Covered-call simulation | `backtest covered-call` |

### Strategies

Strategies come from three places. `backtest -list` (or `./bin/strategy`) prints all of them, about 65,600 today.

| Source | Count | Defined in |
|---|---|---|
| Go strategies | a handful (`voo-up3`, `price-action-reclaim`, `biggest-winner*`, `tsll-daily-one-share`, covered calls) | `pkg/strategy/` |
| SQL pipelines | folders under `sql/strategies/` that have a Go owner | `sql/strategies/<id>/` |
| Rows in `refdata/strategies.db` | `streak_strategy` 13,136, `hold_strategy` 13,121, `hold_bail_strategy` 13,121, `tree_strategy` 13,121 (ids `<symbol>_tree`), `markov_strategy` 13,122 | `pkg/streak_strategy`, `pkg/hold_strategy`, `pkg/hold_bail_strategy`, `pkg/tree_strategy`, `pkg/markov_strategy` |

Most streak rows are a generic "drop 3 days, buy the rebound" rule, one per symbol. A few were promoted from sweeps (`source_strategy` `voo-up3`, `gld-decline`, `universe-screen`, `manual`). `streak-voo-buy-tecl` is now `streak-voo-buy-tecl`. `park-<symbol>` resolves for any ticker without registration (see below). The older `streak-voo-buy-tecl`, `gld-decline` and `googl-hop` strategies are no longer registered; their SQL folders remain in `sql/strategies/`.

### Searching for a stack

The search that produced the current numbers, in order:

1. `walk_forward -keep-going` over the candidates on a market DB cut off before a held-out final year, then `check_overfit`.
2. `sql/search/01_gate_walk_forward.sql`: strict out-of-sample gate (at least 30 OOS trades, OOS Sharpe 1.5, 75% positive folds, worst fold drawdown 6%).
3. `sql/search/02_liquidity_returns.sql` and `03_liquidity_screen.sql`: dollar volume, price and split-jump screen. `04_candidates.sql` joins the survivors to their trade symbols. Run these with `sqlite3`, attaching the walk-forward DB (`wf`), market DB (`mkt`) and `refdata/strategies.db` (`ref`).
4. `backtest stack-eval` over the survivors for several primaries and `-alloc` sizes.
5. Re-run the frozen stacks on the held-out year with `-start`.

Every `backtest` and `stack-eval` run now does step 5 itself (see the holdout note under `backtest`). Result to date: no stack reached 79% CAGR with drawdown under 6%. Liquid stacks held a Calmar of about 4 to 8 in-sample and about 4.5 on the holdout. The benchmark to beat is in [docs/omnifunds_benchmark.md](docs/omnifunds_benchmark.md).

## Paths

`pkg/appenv` reads the environment, then the nearest `.env`. A real environment variable wins.

| Variable | Default | Used for |
|---|---|---|
| `APP_FOLDER` | `.` | repository root. `data/`, `refdata/` and `data/reports/` are fixed subfolders of it |
| `POLYGON_API_KEY` | empty | Polygon equity and option downloads, `universe` |
| `STRATEGY_ALLOWLIST` | empty | `strateval` live-list snapshot |
| `STRATEGIES_DB` or `STRATEVAL_DB` | empty | overrides the strateval ledger path |

Default files:

| Role | Path |
|---|---|
| Market bars | `data/market_history.db`, table `backtest_start` |
| Strategy tables and symbol lists | `refdata/strategies.db` (`streak_strategy`, `hold_strategy`, `hold_bail_strategy`, `tree_strategy`, `markov_strategy`) |
| Stock and ETF universe | `refdata/universe.db`, table `universe` |
| Per-strategy results | `data/reports/<id>.db`, then `data/reports/<id>_2.db`, `data/reports/<id>_3.db`, … |
| Shared-account results | `data/reports/shared_<primary>_<secondary>_….db` |
| HTML tear sheet | `data/reports/backtest_report.html` |

Commands that call `cliutils.GetDefaultMarketDB` (`backtest`, `study`, `livescan`) use `data/market_history.db`. `market_history`, `gridsearch` and `scoreboard` also default to it and `market_history` creates it when it writes.

The Go `flag` package stops at the first bare argument. Put flags before a positional id or ticker. A subcommand is argument 1: `backtest stack-eval -primary …`, not `backtest -primary … stack-eval`.

Daily simulations load rows with `length(Date) = 10`. Minute bars stay in the same table and are skipped by the daily bar query.

---

## market_history

Pull bars into SQLite. Default source is Yahoo, with Stooq as the fallback. Polygon is opt-in.

**Reads:** `refdata/strategies.db` when no tickers are passed (table `leveraged_etf`, limit 50) or when `-list` names an `etf_universe` list (`all`, `6yr`, `sweep`). Both tables are empty today and no command fills them, so pass tickers explicitly.

**Writes:** `data/market_history.db`, table `backtest_start`. Option history uses the same DB, tables `option_contracts`, `option_bars`, `option_expiry_scan`. Missing dates are filled; `-force` replaces the range.

| Flag | Default |
|---|---|
| `-db` | `data/market_history.db` |
| `-settings` | `refdata/strategies.db` |
| `-source` | `yahoo` (`polygon`, `polygon-options`, `stooq`) |
| `-target-table` | `backtest_start` |
| `-timeframe` | `1d` |
| `-years` | `4` |
| `-table` | `leveraged_etf` (settings DB symbol table) |
| `-limit` | `50` (only the settings-table fallback) |
| `-otm` | `0,2,5` (polygon-options, percent OTM) |
| `-rate` | `5` API calls/minute (polygon-options) |

```bash
./bin/market_history VOO IEF GLD
./bin/market_history -symbols PDD -years 6
./bin/market_history -list 6yr -years 6
./bin/market_history -source polygon -symbols VOO -timeframe 1m -polygon-key "$POLYGON_API_KEY"
./bin/market_history -source polygon-options -symbols VOO
```

`-start YYYY-MM-DD` overrides `-years`. `-end` defaults to today.

---

## backtest

Run one strategy, many strategies, or a shared cash account.

**Reads:** market DB (`-db`, table `-table` `backtest_start`). `optimized` also reads `data/reports/gridsearch.db`. SQL pipelines read `sql/strategies/<id>/` when that directory exists, otherwise the copy embedded in the binary.

**Writes:** `data/reports/<id>.db` (next free `data/reports/<id>_N.db` if the name is taken). Tables `signals`, `trades`, `equity_curve`, `performance_summary`, and when relevant `return_breakdown` and `run_metrics`. HTML at `data/reports/backtest_report.html`. Auto-download (`-auto-download`, default on) writes missing bars into the market DB. Window starts `2021-01-01` (`-start`); earlier bars warm up SMAs only. Capital default `$100,000`. `-download-years` default `5`.

```bash
./bin/backtest -list
./bin/backtest -strategy streak-voo-buy-tecl -capital 100000
./bin/backtest -strategy streak-voo-buy-tecl,mara_tree
./bin/backtest -strategy all
./bin/backtest -primary streak-voo-buy-tecl -secondary mara_tree,pdd_tree -capital 100000
./bin/backtest streak-voo-buy-tecl+mara_tree
./bin/backtest -alloc 0.10 streak-voo-buy-tecl+mara_tree+pdd_tree
./bin/backtest -primary streak-voo-buy-tecl -secondary mara_tree -alloc 0.10 -default-asset VYM
```

**Out-of-sample holdout (default).** The last 12 months of history are held out. The main pass runs from `-start` through the cutoff (the last bar minus `-holdout-months`), prints as `IN-SAMPLE`, and writes its usual files. Then the same strategy, or for `stack-eval` the stack it found (park included), runs once on the held-out months, flat at the start, printed as `OUT-OF-SAMPLE` and written to `data/reports/oos/` with an `_oos` HTML file. Select and tune on the in-sample pass only. `-holdout-months 0` simulates all history in one pass; `-end YYYY-MM-DD` ends history earlier. The split is skipped, with a message, when less than a year would be left in-sample. It does not apply to `-list`, `-signals-only`, `stale`, `optimized` or `covered-call`. Two caveats: single-strategy results in `data/reports/<id>.db` now cover the in-sample window only, and a strategy whose SQL pipeline or tree was fitted on all history can still see the future inside the held-out months.

`-symbol` limits the book to one ticker. `-hold`, `-target`, `-stoploss`, `-max-positions`, and `-alloc` override the strategy config when set (non-zero). `-alloc` is a fraction of equity per position (`0.10` = 10%) on standalone runs and on shared-account stacks. `-default-asset GOOGL` is shared-account only: after each session, leftover cash is bought into that symbol, and a sleeve entry sells it first to fund the order. The result file is `data/reports/shared_<primary>_<secondaries>_default-<symbol>.db`. When that name would make the SQLite journal longer than 255 bytes, the file is `data/reports/default_asset_<symbol>.db`. `-no-reinvest-dividends` pays dividends into cash for total-return strategies. `-force` re-runs strategies that already have a usable result (multi-strategy only). `-signals-only` skips the portfolio sim and scans the live window the same way `livescan` does; it does not stack.

### backtest stale

Print which `data/reports/*.db` results are stale (unknown strategy, newer market bars, or an edited SQL pipeline). No simulation.

```bash
./bin/backtest stale
```

### backtest optimized

Re-run strategies with the highest-resilience row in `data/reports/gridsearch.db` (`hold_days` must be set; older rows without it are ignored). No `-strategy` means every registered strategy. A strategy with no usable sweep runs on its own defaults and is named in the output.

```bash
./bin/backtest optimized -strategy streak-voo-buy-tecl
./bin/backtest optimized
```

### backtest stack-eval

Rank registered strategies as idle-cash overlays on one primary, one shared ledger. Rankings go to `data/reports/stack_eval_<primary>.db` (table `overlay_rankings`). Pairwise runs do not each write a `shared_*.db`. `-persist-best` (default on) writes one `data/reports/shared_<primary>_<secondaries>_N.db` for the greedy stack.

Default candidates skip duplicate `*-sql` ids, `voo-buy-hold`, `genetic-momentum`, and any strategy that does not name its symbols. Pairs are ranked by added equity; the greedy stack is chosen the same way, not by drawdown. Rank the finalists by Calmar and max drawdown from the persisted DB. `-include-universe` adds symbol-scanning strategies. Pass `-secondary` with an explicit list when the registry holds tens of thousands of strategies; `-db`, `-auto-download=false` and `-out-dir` let a search run on a trimmed market DB without touching `data/`. `-stack-depth` (3) is how many complementary overlays are stacked after the ranking.

```bash
./bin/backtest stack-eval -primary streak-voo-buy-tecl
./bin/backtest stack-eval -primary streak-voo-buy-tecl -secondary mara_tree,pdd_tree -persist-best
./bin/backtest stack-eval -primary streak-voo-buy-tecl -stack-depth 5
```

### Park member in a stack

`park-<symbol>` (for example `park-googl`, `park-sgov`) is a stack member that holds leftover cash in that symbol. It is the same book as `-default-asset`, written as a stack member so it can be named in `-strategy`, `-secondary` and `stack-eval`. It emits no signals, resolves for any ticker without registration, cannot be the primary, and a stack holds one. Combining it with `-default-asset` for a different symbol is an error.

```bash
./bin/backtest -strategy "streak-voo-buy-tecl+streak-fslr-down3+park-googl" -alloc 0.1
./bin/backtest stack-eval -primary streak-voo-buy-tecl -secondary "streak-fslr-down3,streak-penn-down3,park-sgov"
```

In `stack-eval` the park applies to the baseline, every pairwise run and the final stack; it is not ranked as an overlay. Compare parks by running `stack-eval` once per park symbol.

### backtest covered-call

Hold one underlying (default `VOO`) and sell a monthly call. Prints the comparison on stdout. Does not write a strategy result DB.

**Reads:** equity bars and `option_contracts` / `option_bars` in the market DB. Load the chains first with `market_history -source polygon-options`.

```bash
./bin/market_history -source polygon-options -symbols VOO
./bin/backtest covered-call -symbol VOO -otm 2 -commission 0.65 -opt-slip 0.05
```

`-start` defaults to the first rollable month (option history is about two years on the Polygon free tier), not `2021-01-01`.

---

## livescan

Same signal generation as `backtest`, window fixed to the last completed session in the market DB. `ENTER` means a buy for the next session. Refresh failure aborts; it does not scan a stale tip.

**Reads:** market DB, table `backtest_start`. `-bars 0` loads each strategy's minimum history.

**Writes:** `data/reports/livescan.db` (`livescan_status`, `livescan_signals`). `-json` also prints the scan to stdout for `trade_orchestrator`.

```bash
./bin/livescan -list
./bin/livescan -strategy streak-voo-buy-tecl,mara_tree
./bin/livescan all
./bin/livescan -strategy streak-voo-buy-tecl -json
```

`-auto-download` defaults on. `-download-years 0` derives the window from `-bars` or the strategy minimum.

---

## gridsearch

Sweep hold, take-profit, stop, and the strategy's other axes. One strategy writes HTML. Several strategies share one worker pool and record progress so a rerun skips finished ids.

**Reads:** `data/market_history.db`, table `backtest_start`. `-symbols-from` reads a study DB `etf_compare` view (for example `data/reports/voo_up3_etf.db`) ranked by `rank_cagr`.

**Writes:** `data/reports/gridsearch.db` (`gridsearch_runs`, `gridsearch_results`). Single-strategy HTML defaults to `data/reports/<strategy>_gridsearch.html`. Start date `2021-01-01`. Capital `$100,000`. Allocation `0.65`. Cash yield `0.045`. `-min-trades 5`, `-top 10`. `-max-perms 20000` skips a huge generic grid in multi-strategy mode only. `streak-*` strategies loaded from `refdata/strategies.db` are excluded unless `-include-streak`.

```bash
./bin/gridsearch -list
./bin/gridsearch -strategy streak-voo-buy-tecl -top 20
./bin/gridsearch -strategy all -no-html
./bin/gridsearch -strategy voo-up3 -symbols-from data/reports/voo_up3_etf.db -top-cagr 10
```

`-force` redoes a strategy already marked done in `gridsearch_runs`.

### gridsearch params

Print the parameter grid. No database and no simulation.

```bash
./bin/gridsearch params streak-voo-buy-tecl
./bin/gridsearch params all
```

### gridsearch stale

Report which completed sweeps are stale. No new sweep.

```bash
./bin/gridsearch stale
```

### gridsearch promote

Copy winning sweep rows into `refdata/strategies.db` table `streak_strategy`. Each row becomes a strategy id `streak-<signal>-<up|down><days>-<trade>` with the watch symbol, the symbol bought, and the swept hold, take-profit, stop, and regime. Backtest, scoreboard, livescan, and strateval load those rows on the next run.

```bash
./bin/gridsearch promote -strategy voo-up3 -min-win-rate 0.6 -min-trades 30 -top 5
```

Defaults for promote are win rate `0.6`, `30` trades, and `-top 5`. A sweep's own `-min-trades` default stays `5`. `-gridsearch-db` chooses the sweep file. Rows that share an id keep the higher win rate. A NULL `signal_symbol` (sweeps from before that column existed) uses the parent strategy's watch symbol.

The walkthrough for writing a row, backtesting it, sweeping it, and ranking it is [docs/strategies/streak_strategy.md](docs/strategies/streak_strategy.md).

---

## park_sweep

Run every row of `streak_strategy` and `markov_strategy` with leftover cash parked in one symbol. The lists, the window, and the results stay in SQLite. The park symbol for the default config is GOOGL.

**Reads:** `refdata/strategies.db` (`streak_strategy`, `markov_strategy`) and `data/market_history.db`.

**Writes:** `data/reports/park_googl.db` (`sweep_config`, `park_asset`, `sweep_strategy`, `strategy_run`). Nothing is inserted into the settings tables, and no per-strategy file is written under `data/reports/`.

```bash
./bin/park_sweep seed
./bin/park_sweep run
./bin/park_sweep rank
./bin/park_sweep report
```

`report` writes `data/reports/park_googl_report.db` and `data/reports/park_googl.html` from the sweep. The database holds every strategy row. The page shows the park buy-and-hold, every done streak, and the 25 Markov rows with the highest edge.

`sweep_config` holds the window (`2021-10-01` through `2026-10-01`), capital (`100000`), and park symbol (`GOOGL`). `allocation_pct` NULL uses each row's own allocation. A symbol that does not cover the window is stored as `skipped`. `run` retries `failed` and `running` rows and leaves `done` rows. `rank` prints whatever is already `done`. `-db`, `-market-db`, `-settings-db`, and `-concurrency` override the defaults.

---

## scoreboard

Backtest every registered strategy that lacks a usable result, then rank them.

**Reads:** `data/market_history.db` (`backtest_start`) and existing `data/reports/<id>[_N].db`.

**Writes:** per-strategy `data/reports/<id>[_N].db` for anything it has to run, then `data/reports/scoreboard.db`. If that file is locked it writes `data/reports/scoreboard_N.db`. Capital `$100,000`, start `2021-01-01`, download horizon 5 years. No `-db` or `-out-dir` flag; both follow `APP_FOLDER`.

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

**Writes:** `data/reports/<study-id>.db`.

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

**Writes:** `data/reports/strategies.db` (`strategies`, `strategy_evals`, `deployments`), unless `STRATEGIES_DB` or `STRATEVAL_DB` is set. Scratch artifacts under `data/reports/strateval_runs/`. Held-out window `-oos-months 12`. Capital `$100,000`. Start `2021-01-01`. Tier A gates: at least 12 OOS trades, win rate 0.55, max drawdown 0.15.

```bash
./bin/strateval -list
./bin/strateval -strategy streak-voo-buy-tecl
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

**Writes:** `data/reports/walk_forward.db`. Tables `walk_forward_fold` and `walk_forward_summary`. Train 24 months, test 6, step 6. Capital `$100,000`. Registers the same strategies as `backtest` (`pkg/stratreg`). It walks strategies one at a time, not as a stack. `-keep-going` skips a strategy with too little history instead of stopping the list. `-market-db` and `-db` choose the inputs, so a run can use a copy of the market DB that ends before a held-out period.

```bash
./bin/walk_forward -strategy streak-voo-buy-tecl
./bin/walk_forward -strategy streak-voo-buy-tecl,mara_tree -train-months 24 -test-months 6 -step-months 6
```

## check_overfit

Read a walk-forward DB and print a verdict per strategy: `HOLDS`, `DECAYS`, `CURVE_FIT`, or `INSUFFICIENT`.

**Reads / writes:** `data/reports/walk_forward.db`. Adds `check_overfit_gate` when it records the gates. Defaults: `-min-oos-trades 8`, `-trial-cutoff 20`, `-decay 0.25`. A streak strategy has one trial, so it is never `CURVE_FIT` however many were screened. After screening thousands on the same data, add a stricter gate such as `sql/search/01_gate_walk_forward.sql`.

```bash
./bin/check_overfit
./bin/check_overfit -db data/reports/walk_forward.db -min-oos-trades 8
```

---

## universe

Discover US stocks and ETFs from Polygon and classify them (leverage, direction, category, first trade date, whether history reaches 2021).

**Writes:** `refdata/universe.db`, table `universe` (about 13,500 rows). Needs `POLYGON_API_KEY` or `-polygon-key`.

```bash
./bin/universe
./bin/universe -etfs-only -max-checks 200
```

Flags: `-db`, `-polygon-key`, `-workers 16`, `-limit 1000`, `-max-checks 0`, `-etfs-only`, `-stocks-only`.

## train_markov

Trains the Markov regime models and saves them in SQLite. `backtest` only reads a saved model; it never trains one, so run this first, and again when the market data has advanced.

The model is per symbol: a bar is bull at +5% or more over 20 bars, bear at -5% or less, otherwise sideways. For each date it stores the walk-forward chance that the next bar is bull or bear, using only transitions known by that date. The calculation is SQL (`sql/stages/markov_train`, one slice table per stage).

**Reads:** `data/market_history.db`, and `refdata/strategies.db` (`markov_strategy.signal_symbol`) when no symbols are given. **Writes:** `data/markov_models.db` (`markov_prediction`, `markov_model_meta`). Symbols already in the model are replaced. A symbol with fewer than 21 bars gets no model. A markov strategy whose signal symbol has no model produces no signals and logs which `train_markov` command to run.

```bash
./bin/train_markov                      # every signal_symbol in markov_strategy
./bin/train_markov GOOGL AAPL           # just these
./bin/train_markov -symbols GOOGL,AAPL -batch 100
```

Flags: `-db`, `-model-db`, `-ref-db`, `-symbols`, `-batch 200`, `-calc-dir` (keep the last batch's slice tables). `markov_hmm_*` strategies read `data/reports/hmm_regime.db`, written by `study hmm_regime`.

## stratlist

Run a SELECT from a `.sql` file against `refdata/strategies.db` and print the first column (the strategy id) of each row. Duplicates and blanks are dropped; the DB is opened query-only. Examples are in `sql/lists/`.

```bash
./bin/stratlist sql/lists/sample_100_per_table.sql          # one id per line
./bin/backtest -strategy "$(./bin/stratlist -comma sql/lists/sample_100_per_table.sql)"
```

Flags: `-db` (default `refdata/strategies.db`), `-sql` (or give the file as the first argument), `-comma`.

## strategy

Print every registered code and SQL strategy with its definition source, then a row count per strategies.db table. Rows are not listed one by one. No flags.

```bash
./bin/strategy
```

## transaction_calc

Turn an Interactive Brokers transaction-history CSV into the same performance report the backtester uses.

**Writes:** `data/reports/<YYYY-MM-DD>/<csv name>.html`.

```bash
./bin/transaction_calc -in data/U22262325.TRANSACTIONS.1Y.csv
```

## markov_test

Print the empirical bear / sideways / bull transition matrix and tomorrow's probabilities for GOOGL (20-day return, plus or minus 5% thresholds). No flags, no writes. See [docs/MarkovModel.md](docs/MarkovModel.md).

```bash
./bin/markov_test
```
