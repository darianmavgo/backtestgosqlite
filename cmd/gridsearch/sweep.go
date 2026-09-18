package main

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/jmoiron/sqlx"
)

// sweepOptions bundles per-run overrides and controls shared by single-strategy
// and batch grid-search modes.
type sweepOptions struct {
	AllocOverride     *float64
	CashYieldOverride *float64
	SymbolOverride    string
	SignalOverride    string
	Capital           float64
	MinTrades         int
	TopN              int
	StartDate         string // earliest bar date to sweep ("" = full history)
	InnerWorkers      int // single-strategy mode only: workers within this one sweep
}

// sweepTask is one (signal-days, hold, TP, SL, regime, allocation, symbol)
// permutation to evaluate.
type sweepTask struct {
	sym        string
	sigDays    int
	hold       int
	tp         float64
	sl         float64
	regime     string
	alloc      float64
	tradeBars  []models.Bar
	isBaseline bool
}

// sweepContext holds everything about one strategy's parameter space needed to
// evaluate its tasks, computed once up front (bar fetches, tree-bounce base
// signals) and then reused across every task.
type sweepContext struct {
	Strat       strategy.Strategy
	ParamSpace  strategy.ParameterSpace
	SignalBars  []models.Bar
	BaseSignals []models.Signal // precomputed once for tree_bounce strategies
	SortedDates []string
	TotalPerms  int
	StartedAt   time.Time
}

// estimatePerms computes a strategy's generic parameter grid size without
// fetching any bars — cheap enough to call before deciding whether a strategy
// is worth sweeping at all in batch mode (see -max-perms).
func estimatePerms(strat strategy.Strategy, opts sweepOptions) int {
	paramSpace := strategy.AssessParameterSpace(strat)
	if opts.AllocOverride != nil {
		paramSpace.Allocations = []float64{*opts.AllocOverride}
	}
	if opts.SymbolOverride != "" {
		paramSpace.Symbols = []string{opts.SymbolOverride}
	}
	return len(paramSpace.Symbols) * len(paramSpace.SignalDays) * len(paramSpace.HoldDays) *
		len(paramSpace.TakeProfits) * len(paramSpace.StopLosses) * len(paramSpace.Regimes) * len(paramSpace.Allocations)
}

// prepareSweep resolves a strategy's parameter space, fetches the bars it needs,
// and builds the full list of tasks to evaluate — but evaluates nothing. This is
// the part of a sweep that's cheap (I/O, not simulation), so both single-strategy
// mode and the batch flattened pool call it once per strategy up front.
func prepareSweep(db *sqlx.DB, strat strategy.Strategy, opts sweepOptions) (*sweepContext, []sweepTask, error) {
	paramSpace := strategy.AssessParameterSpace(strat)
	if opts.AllocOverride != nil {
		paramSpace.Allocations = []float64{*opts.AllocOverride}
	}
	if opts.CashYieldOverride != nil {
		paramSpace.CashYield = *opts.CashYieldOverride
	}
	if opts.SymbolOverride != "" {
		paramSpace.Symbols = []string{opts.SymbolOverride}
	}
	if opts.SignalOverride != "" {
		paramSpace.SignalSymbol = opts.SignalOverride
	}

	barMap, _, err := storage.FetchBars(db, "backtest_start", []string{paramSpace.SignalSymbol}, opts.StartDate, "")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch %s bars: %w", paramSpace.SignalSymbol, err)
	}
	signalBars := barMap[paramSpace.SignalSymbol]
	if len(signalBars) == 0 {
		return nil, nil, fmt.Errorf("no price bars found for signal symbol %s", paramSpace.SignalSymbol)
	}

	tradeBarsMap := make(map[string][]models.Bar, len(paramSpace.Symbols))
	for _, sym := range paramSpace.Symbols {
		tBarMap, _, err := storage.FetchBars(db, "backtest_start", []string{sym}, opts.StartDate, "")
		bars := tBarMap[sym]
		if err == nil && len(bars) > 0 {
			tradeBarsMap[sym] = bars
		}
	}

	dateSet := make(map[string]struct{})
	for _, b := range signalBars {
		dateSet[b.Date] = struct{}{}
	}
	for _, bars := range tradeBarsMap {
		for _, b := range bars {
			dateSet[b.Date] = struct{}{}
		}
	}
	sortedDates := make([]string, 0, len(dateSet))
	for d := range dateSet {
		sortedDates = append(sortedDates, d)
	}
	sort.Strings(sortedDates)

	totalPerms := len(paramSpace.Symbols) * len(paramSpace.SignalDays) * len(paramSpace.HoldDays) *
		len(paramSpace.TakeProfits) * len(paramSpace.StopLosses) * len(paramSpace.Regimes) * len(paramSpace.Allocations)

	var baseSignals []models.Signal
	if paramSpace.Direction == "tree_bounce" {
		baseSignals = strat.GenerateSignals(map[string][]models.Bar{paramSpace.SignalSymbol: signalBars})
	}

	ctx := &sweepContext{
		Strat:       strat,
		ParamSpace:  paramSpace,
		SignalBars:  signalBars,
		BaseSignals: baseSignals,
		SortedDates: sortedDates,
		TotalPerms:  totalPerms,
		StartedAt:   time.Now(),
	}

	var tasks []sweepTask
	for _, sym := range paramSpace.Symbols {
		tBars, ok := tradeBarsMap[sym]
		if !ok {
			continue
		}
		for _, sigDays := range paramSpace.SignalDays {
			for _, regime := range paramSpace.Regimes {
				for _, hold := range paramSpace.HoldDays {
					for _, tp := range paramSpace.TakeProfits {
						for _, sl := range paramSpace.StopLosses {
							for _, alloc := range paramSpace.Allocations {
								isBase := false
								if paramSpace.Direction == "tree_bounce" {
									isBase = (hold == paramSpace.Baseline.HoldDays &&
										math.Abs(tp-paramSpace.Baseline.TakeProfit) < 1e-4 &&
										math.Abs(sl-paramSpace.Baseline.StopLoss) < 1e-4)
								} else {
									isBase = (sigDays == paramSpace.Baseline.SignalDays &&
										hold == paramSpace.Baseline.HoldDays &&
										math.Abs(tp-paramSpace.Baseline.TakeProfit) < 1e-4 &&
										math.Abs(sl-paramSpace.Baseline.StopLoss) < 1e-4 &&
										regime == paramSpace.Baseline.Regime)
								}
								tasks = append(tasks, sweepTask{
									sym: sym, sigDays: sigDays, hold: hold, tp: tp, sl: sl,
									regime: regime, alloc: alloc, tradeBars: tBars, isBaseline: isBase,
								})
							}
						}
					}
				}
			}
		}
	}

	return ctx, tasks, nil
}

// evaluateTask runs the simulation for a single task and returns the result. ok
// is false when the task was filtered out by the minimum trade count (either by
// raw signal count or by the simulator's actual fill count) and should not be
// counted as a result.
func evaluateTask(ctx *sweepContext, t sweepTask, opts sweepOptions) (gridResult, bool) {
	// Trades and the daily equity curve are only kept for the baseline; every
	// other result is slim (the full grid is far too big to hold in memory at
	// 100k+ permutations) and finalizeSweep re-simulates the few winners.
	return evalTask(ctx, t, opts, t.isBaseline)
}

func evalTask(ctx *sweepContext, t sweepTask, opts sweepOptions, keepDetail bool) (gridResult, bool) {
	barsBySymbol := map[string][]models.Bar{
		ctx.ParamSpace.SignalSymbol: ctx.SignalBars,
		t.sym:                       t.tradeBars,
	}

	cfg := strategy.StrategyConfig{
		AllocationPct:   t.alloc,
		TakeProfitPct:   t.tp,
		StopLossPct:     t.sl,
		HoldingWindow:   t.hold,
		PositionCap:     1,
		CashYieldAnnual: ctx.ParamSpace.CashYield,
	}

	var sigs []models.Signal
	if ctx.ParamSpace.Direction == "tree_bounce" {
		sigs = make([]models.Signal, len(ctx.BaseSignals))
		for idx, bs := range ctx.BaseSignals {
			sCopy := bs
			sCopy.HoldDaysOverride = t.hold
			if t.tp > 0 {
				sCopy.TakeProfit = bs.Close * (1.0 + t.tp)
			} else {
				sCopy.TakeProfit = 0
			}
			if t.sl > 0 {
				sCopy.StopLoss = bs.Close * (1.0 - t.sl)
			} else {
				sCopy.StopLoss = 0
			}
			sigs[idx] = sCopy
		}
	} else {
		sigs = buildSignals(ctx.SignalBars, t.tradeBars, t.sigDays, ctx.ParamSpace.Direction, t.regime, t.tp, t.sl, t.hold, t.sym)
	}

	if len(sigs) < opts.MinTrades {
		return gridResult{}, false
	}

	sim := simulator.NewPortfolioSimulator(cfg, opts.Capital)
	report, trades, curve := sim.Run(sigs, barsBySymbol, ctx.SortedDates)

	if report.TotalTrades < opts.MinTrades {
		return gridResult{}, false
	}

	label := ""
	signalDays := t.sigDays
	regime := t.regime
	if ctx.ParamSpace.Direction == "tree_bounce" {
		label = fmt.Sprintf("%s/Hold-%dd/TP+%.0f%%/SL-%.0f%%", t.sym, t.hold, t.tp*100, t.sl*100)
		signalDays = 0 // tree_bounce's SignalDays axis is a fixed [1] placeholder, not a real decline-day window
		regime = ""    // tree_bounce entries aren't regime-gated
	} else {
		label = fmt.Sprintf("%s/%dd/%dd/+%.0f%%-%.0f%%/%s", t.sym, t.sigDays, t.hold, t.tp*100, t.sl*100, t.regime)
	}

	if !keepDetail {
		trades, curve = nil, nil
	}
	return gridResult{
		task:       t,
		Label:      label,
		Report:     report,
		Trades:     trades,
		Curve:      curve,
		IsBaseline: t.isBaseline,
		Symbol:     t.sym,
		SignalDays: signalDays,
		HoldDays:   t.hold,
		TakeProfit: t.tp,
		StopLoss:   t.sl,
		Regime:     regime,
	}, true
}

// finalizeSweep ranks a strategy's completed results three ways and packages
// everything into a sweepOutcome.
func finalizeSweep(ctx *sweepContext, results []gridResult, baselineRes *gridResult, opts sweepOptions) sweepOutcome {
	outcome := sweepOutcome{
		Strat:       ctx.Strat,
		ParamSpace:  ctx.ParamSpace,
		SignalBars:  ctx.SignalBars,
		TotalPerms:  ctx.TotalPerms,
		Results:     results,
		BaselineRes: baselineRes,
		Elapsed:     time.Since(ctx.StartedAt),
	}
	if len(results) == 0 {
		return outcome
	}

	topN := opts.TopN
	if topN < 1 {
		topN = 10
	}

	byCalmar := append([]gridResult(nil), results...)
	sort.Slice(byCalmar, func(i, j int) bool { return byCalmar[i].Report.CalmarRatio > byCalmar[j].Report.CalmarRatio })
	if len(byCalmar) > topN {
		byCalmar = byCalmar[:topN]
	}
	outcome.TopCalmar = byCalmar

	byResilience := append([]gridResult(nil), results...)
	sort.Slice(byResilience, func(i, j int) bool {
		return resilienceScore(byResilience[i].Report) > resilienceScore(byResilience[j].Report)
	})
	if len(byResilience) > topN {
		byResilience = byResilience[:topN]
	}
	outcome.TopResilience = byResilience

	byProfit := append([]gridResult(nil), results...)
	sort.Slice(byProfit, func(i, j int) bool { return byProfit[i].Report.NetProfit > byProfit[j].Report.NetProfit })
	if len(byProfit) > topN {
		byProfit = byProfit[:topN]
	}
	outcome.TopProfit = byProfit

	// Re-simulate the winners to restore their trades/equity curves.
	for _, top := range [][]gridResult{outcome.TopCalmar, outcome.TopResilience, outcome.TopProfit} {
		for i := range top {
			if top[i].Curve != nil {
				continue
			}
			if full, ok := evalTask(ctx, top[i].task, opts, true); ok {
				top[i] = full
			}
		}
	}

	return outcome
}

// runSweep executes the full baked-in parameter grid search for one strategy
// using a local worker pool sized by opts.InnerWorkers. This is the
// single-strategy CLI path — batch mode uses prepareSweep/evaluateTask directly
// against one shared pool instead (see runBatchSweepFlat) so slow strategies
// aren't stuck with a small fixed inner-worker count while other cores idle.
func runSweep(db *sqlx.DB, strat strategy.Strategy, opts sweepOptions) (sweepOutcome, error) {
	ctx, tasks, err := prepareSweep(db, strat, opts)
	if err != nil {
		return sweepOutcome{}, err
	}

	taskCh := make(chan sweepTask, len(tasks))
	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	var results []gridResult
	var baselineRes *gridResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	workers := opts.InnerWorkers
	if workers < 1 {
		workers = 1
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range taskCh {
				res, ok := evaluateTask(ctx, t, opts)
				if !ok {
					continue
				}
				mu.Lock()
				results = append(results, res)
				if t.isBaseline {
					bCopy := res
					baselineRes = &bCopy
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	return finalizeSweep(ctx, results, baselineRes, opts), nil
}
