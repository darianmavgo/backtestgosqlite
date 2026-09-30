# Architecture

Technical map of this repository. Command usage, defaults, and examples are in [README.md](README.md).

The module is `github.com/darianmavgo/backtestgosqlite`. SQLite is `modernc.org/sqlite` (no cgo). Every database `pkg/storage.OpenSQLite` opens sets `journal_mode=WAL`, `synchronous=NORMAL`, `temp_store=MEMORY`, and a 64 MB cache.

## Layout

```
cmd/<name>/main.go     thin main; calls pkg/<name>.Main
pkg/appenv             APP_FOLDER / APP_DATA / APP_REF / APP_REPORTS and .env
pkg/datasource         Yahoo, Stooq, Polygon, Polygon options, SQLite reads
pkg/download           gap-filling writer into the market DB
pkg/storage            bar, trade, signal, equity, performance, option schemas
pkg/refdb              settings.db universes and ETF-tree rows
pkg/models             Bar, Signal, Trade, Position, PerformanceReport
pkg/strategy           Strategy interface, Go strategies, SQL pipelines
pkg/simulator          PortfolioSimulator and SharedAccountSimulator
pkg/runner             load bars, run, stack, scan, staleness, stack-eval
pkg/analytics          metrics and the HTML tear sheet
pkg/options            covered-call simulation
pkg/study              research studies; market_context is a subpackage
sql/strategies/<id>/   ordered .sql pipeline for a strategy
sql/studies            SQL text some studies execute
sql/validation         walk-forward summary SQL
refdata/settings.db    universes, symbol tables, etf_dt_strategies
data/market_history.db daily and intraday bars, option chains
reports/               one SQLite file per run, plus HTML
```

`cmd/*` does not hold strategy logic. A command's flags, defaults, and `Run` live in the matching `pkg` so tests can call `Run` without parsing `os.Args`.

## Configuration

`pkg/appenv` searches `.env` in `.`, `..`, `../..`, the binary's directory, and that directory's parent. The first file to define a key wins. Process environment overrides the file.

| Function | Path |
|---|---|
| `Folder()` | `APP_FOLDER` or `.` |
| `Data()` | `Folder()/APP_DATA` or `Folder()/data` |
| `Ref()` | `Folder()/APP_REF` or `Folder()/refdata` |
| `Reports()` | `Folder()/APP_REPORTS` or `Folder()/reports` |
| `MarketDB()` | `Data()/market_history.db` |
| `RefDB()` | `Ref()/settings.db` |
| `ReportFile(p)` | absolute paths pass through; a leading `reports/` is stripped and the rest is joined to `Reports()` |

`cliutils.GetDefaultMarketDB` returns `MarketDB()` if the file exists, otherwise `Data()/leveraged_backtest.db`. `backtest`, `study`, and `livescan` use that helper. Download and the batch tools call `MarketDB()` directly.

`eval_ledger`'s ledger is special. It does not use `ReportFile`, because a deploy `.env` can set `APP_FOLDER` to a Linux path. Order: `STRATEGIES_DB`, then `EVAL_LEDGER_DB`, then `<module root>/reports/strategies.db` found by walking up to `go.mod`.

## Market data

`download.Run` opens `-db`, ensures the bar table, and upserts. With no `-symbols` and no `-list` it reads `SELECT DISTINCT symbol FROM <table>` in the settings DB (`leveraged_etf` by default, `-limit 50`). `-list` reads `etf_universe` for that list name. Bare arguments are tickers when `-symbols` is empty: commas are separators, values are uppercased.

Coverage is per `(symbol, timeframe)`. The writer requests only the missing span unless `-force`. Yahoo is the default primary source and Stooq is its fallback. `-source polygon` uses Polygon and falls back to Yahoo. `-source stooq` uses Stooq only. `-source polygon-options` does not download equities; it lists call chains for one underlying and stores them in the market DB. The Polygon key is `-polygon-key` or `POLYGON_API_KEY` via `datasource.ResolvePolygonAPIKey`. Stooq's HTTP body is parsed in memory into `models.Bar` and then inserted. There is no file ingest path.

Bar table (`storage.EnsureBarTable`), default name `backtest_start`:

```
idx, Date, timeframe, asset_class, open, high, low, close, "Adj Close", volume, symbol
UNIQUE (symbol, Date, timeframe)
```

`storage.FetchBars` is the daily simulator's read. It keeps `length(Date) = 10`, so `1m` timestamps never enter a daily run. It also computes `sma200` and `sma50` in SQL. `-start` is applied in Go after that window, so bars before the start still warm the averages, then drop out of the simulated dates. `FetchRecentBars` is the live-scan equivalent: same daily filter, last N bars per symbol, averages still computed on the longer series.

Option tables in the same market DB (`storage.EnsureOptionTables`):

| Table | Grain |
|---|---|
| `option_contracts` | one listed contract; `bars_fetched_at` lets a rerun skip finished contracts |
| `option_bars` | one day per `(ticker, Date)` |
| `option_expiry_scan` | one `(underlying, expiry)`, including empty chains |

## Reference DB

`pkg/refdb` opens `refdata/settings.db` and ensures:

| Table | Contents |
|---|---|
| `etf_universe` | `(list, symbol)`. Lists: `all` (Polygon active US ETFs), `6yr`, `sweep` |
| `etf_dt_strategies` | one row per symbol the registry turns into `dt_<symbol>`. `SaveDTStrategies` replaces the whole table |
| `etf_dt_strategies_all` | created with the schema; no current command writes it |

Older symbol tables (`leveraged_etf`, `momentum_candidates`, `backtested_win_20_10d`) are also in this file. `download -table` and the UI category lookup read them. `etf_universe` fills `all`. `etf_decision_trees` reads a list and replaces `etf_dt_strategies`. Registration of `dt_*` strategies reads `etf_dt_strategies` through `APP_FOLDER` / `refdb`. A `go test` whose working directory is not the module root does not see that file unless `APP_FOLDER` is set.

## Strategies

```go
type Strategy interface {
    ID() string
    Name() string
    Description() string
    DefaultConfig() StrategyConfig
    Validate() error
    SetDatabases(marketDBPath, calcDBPath string)
    GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal
}
```

Optional interfaces, checked by the runner:

| Interface | Role |
|---|---|
| `RequiredSymbolsProvider` | symbols to load; also marks a stack-eval candidate as eligible without `-include-universe` |
| `TotalReturnProvider` | simulate on `Adj Close` / `Close` scaled bars so dividends reinvest; `ExecuteStrategyWithDividends(..., false)` keeps raw prices and pays cash dividends |
| `OptionOverlayProvider` | no stock signals; covered call via `pkg/options` |
| `MinHistoryProvider` | trailing bars a live scan needs; default `DefaultMinHistoryBars` (250) |
| `DeclineDaysConfigurable` | `SetDeclineDays` so `backtest optimized` can apply a swept streak length |

`Register` keys on the lowercased id. `Get` also matches with `-`, `_`, and spaces removed. Constructors call `Register` from `init`.

`StrategyConfig` holds sizing (`fixed_pct`, `fixed_dollar`, `fixed_shares`, `kelly`), hold, slippage, commission, cash yield, and the two profit/stop fields. `TargetPct` / `TakeProfitPct`: the portfolio uses `TargetPct` when it is greater than 1, otherwise `TakeProfitPct`. `StopLossPct` is a price multiplier (`0.93` is −7%), not an offset. `NextDayLimitEntry` means the signal is known after the close and the order is a next-session limit at the signal price. It fills on that next bar only if the low is at or below the limit (at the limit, or at the open if the open is already through it). The booked price is the fill times `(1+SlippagePct)`. An unmet limit opens nothing. Signals saved in the result DB are the pre-simulator signals, so a dropped next-day order is still in `signals`.

There is one strategy type. It is defined in Go (`pkg/strategy`) and its signals are calculated by the SQL pipeline in `sql/strategies/<id>/`. `AutoRegisterSQLStrategies(root, marketDB)` still inserts a `<dir>-sql` registry entry for a pipeline directory that already has a Go owner. That entry is the same pipeline, used to resolve the directory, not a second strategy. `failed_training/` has no matching Go strategy, so it stays unregistered. That SQL is archived; the Go package is behind `//go:build ignore` plus a `doc.go` stub. Registration is once per `(root, db)` for the process.

A pipeline is a directory of `.sql` files run in name order by `SQLPipelineStrategy`. `pipelinefs.go` reads the directory on disk when it exists and otherwise the embedded FS in `sql/embed.go`, keyed by the directory's base name. Placeholders substituted from `StrategyConfig` before execution:

| Token | Source |
|---|---|
| `__DECLINE_DAYS__` | `DeclineDays` |
| `__TAKE_PROFIT_MULT__` | take-profit multiplier |
| `__STOP_LOSS_MULT__` | stop multiplier |
| `__HOLD_DAYS__` | `HoldingWindow` |
| `__SHORT_TAKE_PROFIT_MULT__` | short-leg take-profit |
| `__SHORT_STOP_LOSS_MULT__` | short-leg stop |
| `__SHORT_HOLD_DAYS__` | short-leg hold |
| `__SYMBOL__` | `Benchmark` (one pipeline, many symbols) |

Each strategy gets its own calc DB (`calc_<id>.db`) so pipeline tables do not collide. `SetDatabases(market, calc)` is called before `GenerateSignals`.

Registered from this tree: `sig-voo-buy-tecl`, `sig-voo-buy-spxu`, `sig-voo-up1-buy-tqqq`, `sig-qqq-up1-buy-tqqq`, `sig-qqq-up1-buy-sqqq`, `voo-up3`, `gld-decline`, `mara_tree`, `nvdl_tree`, `pdd_tree`, `mara_pdd_nvdl` (aliases `tree_bounce_combo` and `mara_pdd_nvdl_combo`), `voo-buy-hold`, `biggest-winner`, `tsll-daily-one-share`, dividend buy-and-hold ids (`<symbol>-buy-hold`), dividend covered calls, and `dt_<symbol>` from `etf_dt_strategies`. `./bin/backtest -list` is the live set.

`sig-voo-buy-tecl` and `sig-voo-buy-spxu` always calculate in SQL. `voo-up3`, `gld-decline`, the three up-volume strategies, `mara_tree`, `pdd_tree`, `nvdl_tree`, and `mara_pdd_nvdl` calculate in SQL when both database paths are set, which is every live backtest. An empty path falls back to the Go loop used by in-memory tests.

These are defined in Go and do not calculate their signals in SQL:

| ID | What runs instead |
|---|---|
| `voo-buy-hold` | one market signal built in Go from the first VOO bar |
| `schd-buy-hold`, `vym-buy-hold`, `dvy-buy-hold` | one market signal built in Go |
| `tsll-daily-one-share` | one market signal per TSLL bar, in Go |
| `biggest-winner` | annual-return ranking in Go |
| `schd-covered-call`, `vym-covered-call`, `dvy-covered-call`, and the three `-5pct` ids | `GenerateSignals` returns nil; the covered-call overlay is Go in `pkg/options` |
| `dt_<symbol>` | feature rows can come from `sql/strategies/decision_tree_features`; the CloudForest tree is grown in Go |
| `<dir>-sql` | duplicate registry id for a pipeline that already has a Go strategy. `stack-eval` drops these ids |

## Simulation

`PortfolioSimulator` (`pkg/simulator/portfolio.go`) does not know strategy ids. It walks sorted dates, accrues cash yield on leftover cash every day, evaluates exits, then opens entries. Exit order is take-profit, stop, trailing stop, ATR stop, then the time barrier. `HoldDaysOverride` on a signal is stored on the position and beats `HoldingWindow`. Sizing is `pkg/simulator/sizer.go`. `analytics.CalculatePerformanceMetrics` turns trades and the equity curve into the report (CAGR, drawdown, Sharpe, Sortino, Calmar, win rate, and the rest of the tear sheet).

`SharedAccountSimulator` is one cash ledger. List order is priority; 0 is primary. Only the primary may preempt: it can liquidate a subordinate for cash or for the same symbol. Same-symbol eviction requires `signal.Priority == 0` and `position.Priority > 0`. Overlays never evict each other; they size to leftover cash or skip. On a cash shortfall the primary sizes against full equity, then liquidates subordinates longest `HoldDays` first. Subordinates use `CalculateShares(cash, equity, …)` and skip when they cannot afford the order. Positions are keyed by symbol. Idle stats (`AvgCashPct`, `FullyIdlePct`, `DaysFullyIdle`) come from the equity curve. An idle day is `invested IS NULL OR invested = 0`.

Stacking is a runner concern. `runner.ExecuteStack` does not construct a new `pkg/strategy` type. Combined ids look like `sig-voo-buy-tecl+mara_tree`. The persisted file is `reports/shared_<primary>_<secondary>_….db` from `storage.CreateUniqueDB`.

`CreateUniqueDB(dir, base)` creates `<base>.db` with `O_EXCL`, then `<base>_2.db`, `<base>_3.db`, …. The base is lowercased and non-alphanumerics other than `-` and `_` become `_`. The first file is run 1. Later runs are `_N`. Readers (`runner.ScanAndValidate`) take the highest increment that still has a usable `performance_summary` / signals payload and fall back when a newer file is corrupt.

`backtest -signals-only` and `livescan` both call `runner.RunSignalScan`. The as-of date is the tip bar. The entry date is the next session. Output is `reports/livescan.db`.

## Result schema

Written by `pkg/storage` into each strategy or shared DB:

| Table | What |
|---|---|
| `signals` | pre-simulator signals: strategy, symbol, date, order type, direction, entry, take-profit, stop, regime, metadata JSON |
| `trades` | fills: entry/exit, reason, shares, PnL, hold, MAE, MFE, commission |
| `equity_curve` | `date`, `total_equity`, `cash`, `invested`, `drawdown_pct` |
| `performance_summary` | one row per strategy id, the metrics the scoreboard compiles |
| `return_breakdown` | price vs dividend split for total-return runs |
| `run_metrics` | extra labeled metrics (covered-call notes and similar) |

`reports/scoreboard.db` is a ranking compiled from those summaries. `scoreboard` deletes and rewrites it; a locked file falls through to `CreateUniqueDB(..., "scoreboard")`.

`reports/gridsearch.db`:

| Table | What |
|---|---|
| `gridsearch_runs` | one row per strategy: status `running` / `done` / `failed`, best Calmar and resilience, `data_max_date` for staleness |
| `gridsearch_results` | one row per config: label, baseline flag, CAGR, drawdown, Calmar, resilience, trades, win rate, and (added later) `symbol`, `signal_days`, `hold_days`, `take_profit_pct`, `stop_loss_pct`, `regime` |

`running` and `failed` are retried. `done` is skipped unless `-force`. `backtest optimized` reads the best row and applies it, including `DeclineDays` when the strategy implements `DeclineDaysConfigurable`. Many older rows have NULL `hold_days`; the label is `SYM/sigDaysd/holdd/+TP%-SL%/regime` or `SYM/Hold-Nd/TP+x%/SL-y%`.

`reports/stack_eval_<primary>.db` table `overlay_rankings` is replaced on each stack-eval (`DELETE` then insert). Columns include combined equity, CAGR, Sharpe, max drawdown, incremental equity versus the primary alone, secondary PnL and trades, preempted count, and idle-cash percents.

Stack-eval candidate filter (`runner.OverlayCandidates`), when `-secondary` is empty:

- drop the primary, duplicate `*-sql` ids, `voo-buy-hold`, `genetic-momentum`
- drop the sibling pair `sig-voo-buy-tecl` / `voo-tecl-spxu-combo`
- `dt_*` only with `-include-dt`, and only the top `-dt-top` by score
- strategies that do not implement `RequiredSymbolsProvider` only with `-include-universe`

Explicit `-secondary` ids skip that filter. After pairwise ranking, a greedy pass stacks up to `-stack-depth` overlays whose traded symbols do not overlap. `-persist-best` writes one shared DB for that stack. Calc databases for the sweep sit in `reports/stack_eval_calc/`.

Staleness (`pkg/runner/staleness.go`) is shared by `backtest stale` and `gridsearch stale`. A result is stale if any one of these is true: the strategy is no longer registered, the market DB has a later bar than the run recorded, or a SQL pipeline file is newer than the result.

## Studies

`pkg/study` is a registry of `Study` values (`ID`, `Name`, `Description`, `SetDatabases`, `Run`). `cmd/study` imports `pkg/study` and blank-imports `pkg/study/market_context`. A study in another subpackage does not register unless that import is added.

`Run` writes `filepath.Join(outDir, id + ".db")`. `SymbolAware` studies receive `-symbol`.

| ID | Output | Notes |
|---|---|---|
| `market_context_20d` | `reports/market_context_20d.db` | 20-session simple and log returns for VOO, IEF, GLD, USO, HYG. SQL: `sql/studies/20dayreturn.sql` |
| `cluster_5pct` | `reports/cluster_5pct.db` | 5% up/down rates by the IEF/GLD/USO cluster. Requires `SetClusterDB` (not a CLI flag) and `data/gain_5pct_frequency.db` next to the market DB |
| `googl_market_context` | `reports/googl_market_context.db` | GOOGL beta, alpha, residual by `cluster_day`. Requires `SetClusterDB`. SQL: `sql/studies/googl_regime.sql` |
| `gain_5pct_frequency` | `reports/gain_5pct_frequency.db` | ranks symbols by days with a ≥5% gain |
| `voo_up3_etf` | `reports/voo_up3_etf.db` | VOO 3-up days, then each sweep-list ETF. View `etf_compare` feeds `gridsearch -symbols-from` |
| `hmm_regime` | `reports/hmm_regime.db` | HMM regimes; `-symbol` defaults to QQQ |
| `qqq_tqqq_volume` | `reports/qqq_tqqq_volume.db` | daily QQQ vs TQQQ volume |
| `march_april_voo_gld_uten` | `reports/march_april_voo_gld_uten.db` | 1-minute VOO/GLD/UTEN, Granger and spillover |
| `mara_decision_tree` / `mu_decision_tree` | `reports/<id>.db` | CloudForest trees aimed at ±5% days |
| `etf_study` | `reports/etf_study.db` | decline-slice study on top S&P ETFs |
| `sp500_lead_lag` | `reports/sp500_lead_lag.db` | minute lead/lag into VOO |
| `market_clustering` | `reports/market_clustering.db` | universe feature clusters |

`sql/studies/cluster_5pct.sql` is the cluster study's SQL text.

## Validation

`walk_forward` slices the daily bars into rolling folds (default 24 months in sample, 6 out of sample, step 6). Each fold is a full `PortfolioSimulator` run. Rows land in `walk_forward_fold`. `sql/validation/walk_forward_summary.sql` rebuilds `walk_forward_summary`. The default file `reports/walk_forward.db` is relative to the process working directory, not `appenv.Reports()`.

`check_overfit` reads `walk_forward_summary` and labels each strategy:

| Verdict | Rule |
|---|---|
| `INSUFFICIENT` | OOS trades below `-min-oos-trades` (8) |
| `CURVE_FIT` | trials at or above `-trial-cutoff` (20) and OOS Sharpe collapses |
| `DECAYS` | OOS Sharpe below `-decay` (0.25) times in-sample Sharpe |
| `HOLDS` | otherwise |

It records the gates in `check_overfit_gate`.

`eval_ledger` is a separate ledger, not the walk-forward file. For each strategy it runs an in-sample window and a held-out tail of `-oos-months` (12). `-optimize` does a coarse in-sample sweep capped by `-max-trials` before the out-of-sample run; the sweep does not see the OOS bars. Rows go to `strategy_evals`. `strategies` and `deployments` track the allowlist snapshot from `sync-deployed`. Tier A requires OOS trades, win rate, and drawdown inside the gates (`-min-oos-trades 12`, `-min-oos-win-rate 0.55`, `-max-oos-dd 0.15`). Scratch sims use `reports/eval_ledger_runs/`.

## Other commands

`audit_shared` runs Go queries against one shared result DB (newest `reports/shared_*.db` by mtime).

`dataflare` is `open -a Dataflare [db]`.

## Engine rules that are easy to get wrong

- A positional id must not be followed by flags. `flag.Parse` stops at the first non-flag, and `backtest` joins the leftovers into the strategy list.
- `id.db` is run 1. `id_N.db` is run N. The latest usable file is the highest N that still validates, not the newest mtime.
- Shared-account priority is list order. Overlays do not preempt each other.
- Next-day limits that never trade are absent from `trades` and still present in `signals`.
- Daily fetches ignore intraday rows. Downloading `1m` bars does not change a daily backtest until a study queries `timeframe = '1m'` directly (`march_april_voo_gld_uten`, `sp500_lead_lag`).
- `dt_*` registration depends on `refdata/settings.db` and `APP_FOLDER`. Grid search and stack-eval omit those trees unless `-include-dt`.
- `AutoRegisterSQLStrategies` will not revive `sql/strategies/failed_training`.
