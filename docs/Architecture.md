# Architecture

Technical map of this repository. Command usage, defaults, and examples are in [README.md](README.md).

The module is `github.com/darianmavgo/backtestgosqlite`. SQLite is `modernc.org/sqlite` (no cgo). Every database `pkg/storage.OpenSQLite` opens sets `journal_mode=WAL`, `synchronous=NORMAL`, `temp_store=MEMORY`, and a 64 MB cache.

## Layout

```
cmd/<name>/main.go     thin main; calls pkg/<name>.Main
pkg/appenv             APP_FOLDER and .env
pkg/datasource         Yahoo, Stooq, Polygon, Polygon options, SQLite reads
pkg/market_history     gap-filling writer into the market DB
pkg/storage            bar, trade, signal, equity, performance, option schemas
pkg/refdb              strategies.db universes, ETF-tree rows, streak_strategy
pkg/models             Bar, Signal, Trade, Position, PerformanceReport
pkg/strategy           Strategy interface, strategy configs, pipeline runner
pkg/streak_strategy    one Strategy per streak_strategy row
pkg/simulator          PortfolioSimulator and SharedAccountSimulator
pkg/pipeline           one scoped run of every stage (scope and step state in pipeline.db)
pkg/runner             load bars, run, stack, scan, staleness
pkg/analytics          metrics and the HTML tear sheet
pkg/options            covered-call simulation
pkg/study              research studies; market_context is a subpackage
sql/strategies/<id>/   ordered .sql pipeline for a strategy
sql/studies            SQL text some studies execute
sql/validation         walk-forward summary SQL
refdata/strategies.db    streak, hold, tree and markov strategy tables; symbol tables
data/market_history.db daily and intraday bars, option chains
data/reports/          one SQLite file per run, plus HTML
```

`cmd/*` does not hold strategy logic. A command's flags, defaults, and `Run` live in the matching `pkg` so tests can call `Run` without parsing `os.Args`.

## Configuration

`pkg/appenv` searches `.env` in `.`, `..`, `../..`, the binary's directory, and that directory's parent. The first file to define a key wins. Process environment overrides the file.

| Function | Path |
|---|---|
| `Folder()` | `APP_FOLDER` or `.` |
| `Data()` | `Folder()/data` |
| `Ref()` | `Folder()/refdata` |
| `Reports()` | `Folder()/data/reports` |
| `MarketDB()` | `Data()/market_history.db` |
| `RefDB()` | `Ref()/strategies.db` |
| `ReportFile(p)` | absolute paths pass through; a leading `reports/` is stripped and the rest is joined to `Reports()` |

`cliutils.GetDefaultMarketDB` returns `MarketDB()`. `backtest`, `study`, and `livescan` use that helper. `market_history` and the batch tools call `MarketDB()` directly.

`strateval`'s ledger is special. It does not use `ReportFile`, because a deploy `.env` can set `APP_FOLDER` to a Linux path. Order: `STRATEGIES_DB`, then `STRATEVAL_DB`, then `<module root>/reports/strategies.db` found by walking up to `go.mod`.

## Market data

`market_history.Run` opens `-db`, ensures the bar table, and upserts. With no `-symbols` and no `-list` it reads `SELECT DISTINCT symbol FROM <table>` in the settings DB (`leveraged_etf` by default, `-limit 50`). `-list` reads `etf_universe` for that list name. Bare arguments are tickers when `-symbols` is empty: commas are separators, values are uppercased.

Coverage is per `(symbol, timeframe)`. The writer requests only the missing span unless `-force`. Yahoo is the default primary source and Stooq is its fallback. `-source polygon` uses Polygon and falls back to Yahoo. `-source stooq` uses Stooq only. `-source polygon-options` does not download equities; it lists call chains for one underlying and stores them in the market DB. The Polygon key is `-polygon-key` or `POLYGON_API_KEY` via `datasource.ResolvePolygonAPIKey`. Stooq's HTTP body is parsed in memory into `models.Bar` and then inserted. There is no file ingest path.

Bar table (`storage.EnsureBarTable`), default name `backtest_start`:

```
idx, Date, timeframe, asset_class, open, high, low, close, "Adj Close", volume, symbol
UNIQUE (symbol, Date, timeframe)
```

`storage.FetchBars` is the daily simulator's read. It keeps `length(Date) = 10`, so `1m` timestamps never enter a daily run. It no longer computes moving averages: `Bar.SMA50` and `Bar.SMA200` are filled only for strategies that need them, by `strategy.LoadBarSMA`, which runs the `sql/stages/bar_sma` stage into a `bar_sma` slice table in that run's calc database and copies the values onto the bars. Multi-strategy runs (`backtest`, `scoreboard`, `gridsearch apply`) go through `runner.RunBatched`: bars are loaded per batch of up to 200 strategies, only for the symbols that batch declares, and a strategy that declares none runs in one final full-universe batch. `-start` is applied in Go after that window, so bars before the start still warm the averages, then drop out of the simulated dates. `FetchRecentBars` is the live-scan equivalent: same daily filter, last N bars per symbol, averages still computed on the longer series.

Option tables in the same market DB (`storage.EnsureOptionTables`):

| Table | Grain |
|---|---|
| `option_contracts` | one listed contract; `bars_fetched_at` lets a rerun skip finished contracts |
| `option_bars` | one day per `(ticker, Date)` |
| `option_expiry_scan` | one `(underlying, expiry)`, including empty chains |

## Reference DB

`pkg/refdb` opens `refdata/strategies.db` and ensures:

| Table | Contents |
|---|---|
| `etf_universe` | `(list, symbol)`. Lists: `all` (Polygon active US ETFs), `6yr`, `sweep` |
| `streak_strategy` | one runnable streak per row: watch symbol, bought symbol, direction (`drop` or `rally`), signal days, hold, take-profit and stop as fractional offsets, regime, allocation, cash yield, slippage, next-day limit. `pkg/streak_strategy.Register` loads it. `gridsearch promote` upserts winning rows |

`strategy_family_param` says, per family column, whether `gridsearch` varies it and with which values (`sql/stages/family_params`, written on every open of the file). Streak rows search signal days, hold, take-profit, stop and regime. Tree and markov rows search the exits only (hold, take-profit, stop) over the entries their own pipeline produces. Hold and hold_bail rows have nothing to search.

Older symbol tables (`leveraged_etf`, `momentum_candidates`, `backtested_win_20_10d`) are also in this file. `market_history -table` and the UI category lookup read them. `etf_universe` is empty: the command that filled it was removed. The per-ETF CloudForest `dt_*` strategies and the `etf_dt_strategies` table were deleted; the tree family (`tree_strategy` rows) is the one decision-tree family. Stock and ETF discovery now goes to `refdata/universe.db` through `universe`. Registration reads the settings tables through `APP_FOLDER` / `refdb`. A `go test` whose working directory is not the module root does not see that file unless `APP_FOLDER` is set.

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
| `RequiredSymbolsProvider` | symbols to load; also marks a stack candidate as eligible without `-include-universe` |
| `TotalReturnProvider` | simulate on `Adj Close` / `Close` scaled bars so dividends reinvest; `ExecuteStrategyWithDividends(..., false)` keeps raw prices and pays cash dividends |
| `OptionOverlayProvider` | no stock signals; covered call via `pkg/options` |
| `MinHistoryProvider` | trailing bars a live scan needs; default `DefaultMinHistoryBars` (250) |
| `DeclineDaysConfigurable` | `SetDeclineDays` so `gridsearch apply` can apply a swept streak length |

`Register` keys on the lowercased id. `Get` also matches with `-`, `_`, and spaces removed. Constructors call `Register` from `init`. Row-backed tables (streak, hold, tree, markov, rotation) are not registered per row: each is a `strategy.Family` (`pkg/strategy/family.go`) and `Get` falls through to it, reading and building the one row asked for. `List()` returns only the strategies with their own pipeline; `ListAll()` also builds every row.

`StrategyConfig` holds sizing (`fixed_pct`, `fixed_dollar`, `fixed_shares`, `kelly`), hold, slippage, commission, cash yield, and the two profit/stop fields. `TargetPct` / `TakeProfitPct`: the portfolio uses `TargetPct` when it is greater than 1, otherwise `TakeProfitPct`. `StopLossPct` is a price multiplier (`0.93` is −7%), not an offset. `NextDayLimitEntry` means the signal is known after the close and the order is a next-session limit at the signal price. It fills on that next bar only if the low is at or below the limit (at the limit, or at the open if the open is already through it). The booked price is the fill times `(1+SlippagePct)`. An unmet limit opens nothing. Signals saved in the result DB are the pre-simulator signals, so a dropped next-day order is still in `signals`.

There is one strategy type. Its config is built in Go (`pkg/strategy`) and its signals are calculated by the SQL in `sql/strategies/<id>/`. `AutoRegisterSQLStrategies(root, marketDB)` still inserts a `<dir>-sql` registry entry for a pipeline directory that already has an owner. That entry is the same pipeline, used to resolve the directory, not a second strategy. `failed_training/` has no matching registered strategy, so it stays unregistered. That SQL is archived; the Go package is behind `//go:build ignore` plus a `doc.go` stub. Registration is once per `(root, db)` for the process.

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

Registered from this tree today: `voo-up3`, `price-action-reclaim`, `biggest-winner` (and its `-inverse` and `-short` variants), `tsll-daily-one-share`, dividend buy-and-hold and covered-call ids. `sig-voo-buy-tecl`, `sig-voo-buy-spxu`, `gld-decline` and `mara_pdd_nvdl` no longer register as Go strategies (the TECL signal is the `streak-voo-buy-tecl` row), and their orphaned SQL folders were deleted. `mara_tree`, `nvdl_tree` and `pdd_tree` are rows of `tree_strategy`; their own SQL folders were deleted because the generic `tree_strategy` pipeline gives identical signals (109, 65 and 150 on the real market DB). Most strategies are rows in the settings tables, loaded by `pkg/stratreg.RegisterAll`. `./bin/backtest -strategylist` is the live set.

Every strategy calculates its signals in SQL. `strategy.RunPipeline` runs a pipeline directory with the market database attached as `backtest_start` and the strategy's calc database for the slice tables, and gives the pipeline `__START_DATE__` and `__END_DATE__` (the window of bars the run loaded) plus the strategy's own `StrategyConfig.SQLParams`. With either database path empty it logs and returns no signals. Go only passes parameters and stamps the strategy id on the result.

| Pipeline | Strategies |
|---|---|
| `streak_strategy`, `tree_strategy`, `markov_model` | the streak, tree and markov rows (`tree_strategy` and `markov_model` read the saved model, see `train`) |
| `markov_hmm` | `markov_hmm_*`, reading `hmm_regime.db` from `study hmm_regime` |
| `hold_strategy` | the `hold` family and the `*-margin-buy-hold` ids: first bar, plus (when `sma_reentry_period` > 0) every bar whose close is above that SMA. `trailing_stop_pct` > 0 is the simulator's bail; 0 for both is plain buy and hold. `__TOTAL_RETURN__` prices the signal on dividend-adjusted prices when the runner simulates on them (`strategy.DividendModeSetter`) |
| `rotation_strategy` | the `rotation` family. Each session it ranks the 1,000 most liquid names (60 session dollar volume known at that close) on the mean of five percentile ranks (21, 63 and 126 session return, distance above the 50 session average, distance from the 63 session high), buys the top `top_k` and sells a name once it ranks worse than `top_k + exit_buffer`. Optional QQQ-above-SMA gate. Entry = -1 closes the position. Row `symbols` lists the candidates because the runner loads bars only for named symbols. Rows are seeded by `sql/seed/rotation_strategy.sql`, the candidate list comes from `sql/studies/omnifunds_reverse/06_candidates.sql` |
| `every_bar` | `tsll-daily-one-share`: one entry per bar with its own take-profit, stop and hold |
| `price_action_reclaim` | `price-action-reclaim`, over every symbol with 250 bars in the window |
| `annual_winner` | `biggest-winner` (long), `-short` and `-inverse`: the prior calendar year's best performer, traded for the new year. A symbol whose first open of the year is 0 has no return and is not ranked |
| `voo_up3` | `voo-up3` |

The covered-call ids (`schd-covered-call`, `vym-covered-call`, `dvy-covered-call` and the `-5pct` ids) return no stock signals: the overlay is simulated in Go in `pkg/options`. A `<dir>-sql` registry id (`voo_up3-sql`) is a duplicate of a pipeline that already has an owner, and `backtest stack` drops it.

Still Go, and not signal generation: the simulator and its metrics (`pkg/simulator`, `pkg/analytics`), the dividend-adjusted price copy and dividend recovery in `pkg/runner`, the covered-call overlay, and the studies in `pkg/study`.

## Simulation

`PortfolioSimulator` (`pkg/simulator/portfolio.go`) does not know strategy ids. It walks sorted dates, accrues cash yield on leftover cash every day, evaluates exits, then opens entries. Exit order is take-profit, stop, trailing stop, ATR stop, then the time barrier. `HoldDaysOverride` on a signal is stored on the position and beats `HoldingWindow`. Sizing is `pkg/simulator/sizer.go`. `analytics.CalculatePerformanceMetrics` turns trades and the equity curve into the report (CAGR, drawdown, Sharpe, Sortino, Calmar, win rate, and the rest of the tear sheet).

`SharedAccountSimulator` is one cash ledger. List order is priority; 0 is primary. Only the primary may preempt: it can liquidate a subordinate for cash or for the same symbol. Same-symbol eviction requires `signal.Priority == 0` and `position.Priority > 0`. Overlays never evict each other; they size to leftover cash or skip. On a cash shortfall the primary sells the default-asset lot first, then liquidates subordinates longest `HoldDays` first. Subordinates use `CalculateShares(cash, equity, …)` and skip when they cannot afford the order. Positions are keyed by symbol. Idle stats (`AvgCashPct`, `FullyIdlePct`, `DaysFullyIdle`) come from the equity curve. An idle day is `invested IS NULL OR invested = 0`.

`-default-asset` parks leftover cash in one symbol after entries each session. The park lot is not a `Positions` entry, so a sleeve can hold that same symbol beside it. Park fills use the primary's slippage and commission and are not written to `trades`, so the sleeve trade count stays comparable to a cash run. Cash dividends on the park come from steps in `Adj Close / Close` and are swept back into the park the same day. A session with no bar for the symbol stays in cash. The park counts as an open position, so idle days fall while it is held. The result goes to `stack.db` in the run folder. `shared_account_audit` stores `default_asset`, `avg_default_pct`, `default_dividends`, and `days_unparked`.

`park-<symbol>` names the same park as a stack member (`strategy.ParkStrategy`, `strategy.SplitResidual`). `Get` resolves it for any ticker-shaped symbol, `backtest` and `backtest stack` pull it out of the member list and pass its symbol as `DefaultAsset`, and `ExecuteStack` rejects a park left in the sleeve list.

`park_sweep` runs that same one-primary park for every settings row. Streak rows run the `streak_strategy` SQL pipeline; `gridsearch` calculates their entries once per (trade symbol, streak length, regime) in the `streak_slice` and `streak_entry` stages (`sql/stages/`) and applies take-profit, stop and hold per grid point. Decision-tree rows call `DecisionTreeSignals` in memory and are absent while `etf_dt_strategies` is empty. Markov rows call `sql/strategies/markov_model` (or `markov_hmm` when the id contains `hmm`). The Markov prediction table does not depend on the strategy row, so it is built once in a temporary calc database and reused. Each strategy deletes only its signal rows before the next insert. Results stay in `reports/park_googl.db`. `edge_vs_googl` is final equity minus the buy-and-hold final equity stored on `park_asset` for the same window and capital. Park contribution is ending equity minus starting capital minus sleeve net profit.

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
| `performance_summary` | one row per strategy id, the metrics the scoreboard compiles. `idle_days` is sessions with no open position (`equity_curve.invested` null or 0). Older rows leave it NULL and scoreboard counts the curve |
| `return_breakdown` | price vs dividend split for total-return runs |
| `run_metrics` | extra labeled metrics (covered-call notes and similar) |

`reports/scoreboard.db` is a ranking compiled from those summaries. `scoreboard` deletes and rewrites it; a locked file falls through to `CreateUniqueDB(..., "scoreboard")`.

`reports/gridsearch.db`:

| Table | What |
|---|---|
| `gridsearch_runs` | one row per strategy: status `running` / `done` / `failed`, best Calmar and resilience, `data_max_date` for staleness |
| `gridsearch_results` | one row per config: label, baseline flag, CAGR, drawdown, Calmar, resilience, trades, win rate, idle days, and (added later) `symbol`, `signal_days`, `hold_days`, `take_profit_pct`, `stop_loss_pct`, `regime`, `signal_symbol`, `allocation_pct`. Older sweeps leave `idle_days` NULL |

`running` and `failed` are retried. `done` is skipped unless `-force`. `gridsearch apply` reads the best row and applies it, including `DeclineDays` when the strategy implements `DeclineDaysConfigurable`. Many older rows have NULL `hold_days`; the label is `SYM/sigDaysd/holdd/+TP%-SL%/regime` or `SYM/Hold-Nd/TP+x%/SL-y%`. Older rows also have NULL `signal_symbol` and `allocation_pct`. `gridsearch promote` fills the watch symbol from the parent strategy's `ParameterSpace` when the column is NULL.

`pkg/streak_strategy` is the `streak` family: `strategy.Get` builds a `streak_strategy` row from its `id` (`streak-<signal>-<up|down><days>-<trade>`). The package imports `pkg/strategy`, so registration is a call to `Register` from backtest, gridsearch, scoreboard, livescan, and strateval, right after `AutoRegisterSQLStrategies`. Signals come from `sql/strategies/streak_strategy/` (`__SIGNAL_SYMBOL__`, `__TRADE_SYMBOL__`, `__STREAK_COL__`, `__REGIME_PREDICATE__`). `gridsearch -strategy all` skips the `streak-` prefix unless `-include-streak`. Scoreboard and `backtest -strategy all` include the rows.

`reports/stack_eval_<primary>.db` table `overlay_rankings` is replaced on each stack (`DELETE` then insert). Columns include combined equity, CAGR, Sharpe, max drawdown, incremental equity versus the primary alone, secondary PnL and trades, preempted count, and idle-cash percents.

Stack-eval candidate filter (`runner.OverlayCandidates`), when `-secondary` is empty:

- drop the primary, duplicate `*-sql` ids, `voo-buy-hold`, `genetic-momentum`
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

`validate` (`pkg/validate`) is the one command for this stage: `validate` runs the folds and then the verdict, `validate walk` only the folds, `validate verdict` only the verdict. The two halves below keep their logic in `pkg/walk_forward` and `pkg/check_overfit`, and the commands of those names are deprecated aliases.

`walk_forward` slices the daily bars into rolling folds (default 24 months in sample, 6 out of sample, step 6). Each fold is a full `PortfolioSimulator` run. Rows land in `walk_forward_fold`. `sql/validation/walk_forward_summary.sql` rebuilds `walk_forward_summary`. The default file `reports/walk_forward.db` is relative to the process working directory, not `appenv.Reports()`.

`check_overfit` reads `walk_forward_summary` and labels each strategy:

| Verdict | Rule |
|---|---|
| `INSUFFICIENT` | OOS trades below `-min-oos-trades` (8) |
| `CURVE_FIT` | trials at or above `-trial-cutoff` (20) and OOS Sharpe collapses |
| `DECAYS` | OOS Sharpe below `-decay` (0.25) times in-sample Sharpe |
| `HOLDS` | otherwise |

It records the gates in `check_overfit_gate`.

`strateval` is a separate ledger, not the walk-forward file. For each strategy it runs an in-sample window and a held-out tail of `-oos-months` (12). `-optimize` does a coarse in-sample sweep capped by `-max-trials` before the out-of-sample run; the sweep does not see the OOS bars. Rows go to `strategy_evals`. `strategies` and `deployments` track the allowlist snapshot from `sync-deployed`. Tier A requires OOS trades, win rate, and drawdown inside the gates (`-min-oos-trades 12`, `-min-oos-win-rate 0.55`, `-max-oos-dd 0.15`). Scratch sims use `reports/strateval_runs/`.

## Other commands


`universe` writes `refdata/universe.db`. `strategy` lists the registry. `transaction_calc` turns an IBKR CSV into a tear sheet. `markov_test` prints the GOOGL transition matrix. The `dataflare` and ETF commands no longer exist.

## Engine rules that are easy to get wrong

- A positional id must not be followed by flags. `flag.Parse` stops at the first non-flag, and `backtest` joins the leftovers into the strategy list.
- `id.db` is run 1. `id_N.db` is run N. The latest usable file is the highest N that still validates, not the newest mtime.
- Shared-account priority is list order. Overlays do not preempt each other.
- Next-day limits that never trade are absent from `trades` and still present in `signals`.
- Daily fetches ignore intraday rows. Downloading `1m` bars does not change a daily backtest until a study queries `timeframe = '1m'` directly (`march_april_voo_gld_uten`, `sp500_lead_lag`).
- `streak-*` registration also depends on `refdata/strategies.db` (`streak_strategy`). `gridsearch -strategy all` omits that prefix unless `-include-streak`. Scoreboard and backtest include every row.

Markov strategies do not train at backtest time. `train markov` (`pkg/train`) runs `sql/stages/markov_train` and publishes `markov_prediction` and `markov_model_meta` into `data/markov_models.db` (`appenv.MarkovDB()`). The `markov_model` pipeline attaches that file (`__MARKOV_DB__`) and reads one symbol's rows; `markov_strategy.GenerateSignals` refuses to run without a trained model for the symbol. `markov_hmm` attaches `hmm_regime.db` (`__HMM_DB__`), produced by `study hmm_regime`.

Training is not part of `backtest`. `train <family>` is the one training command; `markov` and `tree` have a model to fit. Decision trees are trained by `train tree` into `data/tree_models.db`: `sql/stages/tree_features` builds the features and the 5 bucket label, CloudForest grows the tree, and a `tree_strategy` row walks the saved tree in SQL (`sql/strategies/tree_strategy`), buying on class 2. Nothing trains at backtest time. The CloudForest studies live in `pkg/study` (MARA, MU, HMM), and `gridsearch` searches parameters and writes its own DB.

Results are laid out by run. Each `backtest` invocation creates `data/reports/<run_id>/` (the next number above the highest there is, made with a plain mkdir so two processes get different numbers), and writes one result database per strategy family inside it, `storage.ResultsFor(runDir, strategy.FamilyOf(s))`. The held-out pass writes the same file names under `<run_id>/oos/`. `-run-id N` goes back into an existing run, and then strategies already finished in it are skipped unless `-force`. `scoreboard compile` and `status` and `backtest stale` read the latest run unless given one. Inside a family file, `runs.run_id` still numbers the individual strategy runs.

Throughput: strategies run on a worker pool (default one per CPU core) and `runner.RunBatched` loads the next batch of bars while the current one runs. SQL pipelines run in parallel (the old global lock is gone: every strategy has its own calc database). Pipeline joins to the market database must be on `t.Date = x.date`, not `substr(t.Date, 1, 10) = x.date`, or SQLite cannot use the index and scans the symbol's rows once per row. `backtest -cpuprofile file` writes a CPU profile.
