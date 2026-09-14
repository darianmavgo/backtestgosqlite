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
	InnerWorkers      int // concurrency WITHIN this one strategy's sweep
}

// runSweep executes the full baked-in parameter grid search for one strategy:
// fetches the bars it needs, builds every (signal-days, hold, TP, SL, regime,
// allocation) permutation, evaluates them with a bounded worker pool sized by
// opts.InnerWorkers, and ranks the results three ways (Calmar, resilience, net
// profit). It performs no I/O beyond fetching market bars — printing and
// persistence are the caller's job, so this is reusable from both the rich
// single-strategy CLI path and the concurrent multi-strategy batch path.
func runSweep(db *sqlx.DB, strat strategy.Strategy, opts sweepOptions) (sweepOutcome, error) {
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

	barMap, _, err := storage.FetchBars(db, "backtest_start", []string{paramSpace.SignalSymbol}, "", "")
	if err != nil {
		return sweepOutcome{}, fmt.Errorf("failed to fetch %s bars: %w", paramSpace.SignalSymbol, err)
	}
	signalBars := barMap[paramSpace.SignalSymbol]
	if len(signalBars) == 0 {
		return sweepOutcome{}, fmt.Errorf("no price bars found for signal symbol %s", paramSpace.SignalSymbol)
	}

	tradeBarsMap := make(map[string][]models.Bar, len(paramSpace.Symbols))
	for _, sym := range paramSpace.Symbols {
		tBarMap, _, err := storage.FetchBars(db, "backtest_start", []string{sym}, "", "")
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

	start := time.Now()

	type task struct {
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

	tasks := make(chan task, 5000)
	var results []gridResult
	var baselineRes *gridResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	var baseSignals []models.Signal
	if paramSpace.Direction == "tree_bounce" {
		baseSignals = strat.GenerateSignals(map[string][]models.Bar{paramSpace.SignalSymbol: signalBars})
	}

	workers := opts.InnerWorkers
	if workers < 1 {
		workers = 1
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tasks {
				barsBySymbol := map[string][]models.Bar{
					paramSpace.SignalSymbol: signalBars,
					t.sym:                   t.tradeBars,
				}

				cfg := strategy.StrategyConfig{
					AllocationPct:   t.alloc,
					TakeProfitPct:   t.tp,
					StopLossPct:     t.sl,
					HoldingWindow:   t.hold,
					PositionCap:     1,
					CashYieldAnnual: paramSpace.CashYield,
				}

				var sigs []models.Signal
				if paramSpace.Direction == "tree_bounce" {
					sigs = make([]models.Signal, len(baseSignals))
					for idx, bs := range baseSignals {
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
					sigs = buildSignals(signalBars, t.tradeBars, t.sigDays, paramSpace.Direction, t.regime, t.tp, t.sl, t.hold, t.sym)
				}

				if len(sigs) < opts.MinTrades {
					continue
				}

				sim := simulator.NewPortfolioSimulator(cfg, opts.Capital)
				report, trades, curve := sim.Run(sigs, barsBySymbol, sortedDates)

				if report.TotalTrades < opts.MinTrades {
					continue
				}

				label := ""
				if paramSpace.Direction == "tree_bounce" {
					label = fmt.Sprintf("%s/Hold-%dd/TP+%.0f%%/SL-%.0f%%", t.sym, t.hold, t.tp*100, t.sl*100)
				} else {
					label = fmt.Sprintf("%s/%dd/%dd/+%.0f%%-%.0f%%/%s", t.sym, t.sigDays, t.hold, t.tp*100, t.sl*100, t.regime)
				}

				res := gridResult{
					Label:      label,
					Report:     report,
					Trades:     trades,
					Curve:      curve,
					IsBaseline: t.isBaseline,
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

								tasks <- task{
									sym: sym, sigDays: sigDays, hold: hold, tp: tp, sl: sl,
									regime: regime, alloc: alloc, tradeBars: tBars, isBaseline: isBase,
								}
							}
						}
					}
				}
			}
		}
	}
	close(tasks)
	wg.Wait()

	outcome := sweepOutcome{
		Strat:       strat,
		ParamSpace:  paramSpace,
		SignalBars:  signalBars,
		TotalPerms:  totalPerms,
		Results:     results,
		BaselineRes: baselineRes,
		Elapsed:     time.Since(start),
	}
	if len(results) == 0 {
		return outcome, nil
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

	return outcome, nil
}
