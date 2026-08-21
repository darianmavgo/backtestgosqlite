package simulator

import (
	"sync"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/darianmavgo/backtestgosqlite/internal/strategy"
)

// SimResult encapsulates the output of a single backtest run.
type SimResult struct {
	StrategyID  string
	Name        string
	Report      models.PerformanceReport
	Trades      []models.Trade
	EquityCurve []models.DailyEquityPoint
}

// RunConcurrent runs multiple strategies across the same market data in parallel goroutines.
func RunConcurrent(
	strategies []strategy.Strategy,
	barsBySymbol map[string][]models.Bar,
	sortedDates []string,
	initialCapital float64,
	benchmarkBars map[string]models.Bar,
) []SimResult {
	results := make([]SimResult, len(strategies))
	var wg sync.WaitGroup

	for i, strat := range strategies {
		wg.Add(1)
		go func(idx int, s strategy.Strategy) {
			defer wg.Done()

			cfg := s.DefaultConfig()
			signals := s.GenerateSignals(barsBySymbol)

			sim := NewPortfolioSimulator(cfg, initialCapital)
			if benchmarkBars != nil {
				sim.SetBenchmarkBars(benchmarkBars)
			}

			report, trades, eqCurve := sim.Run(signals, barsBySymbol, sortedDates)
			results[idx] = SimResult{
				StrategyID:  s.ID(),
				Name:        s.Name(),
				Report:      report,
				Trades:      trades,
				EquityCurve: eqCurve,
			}
		}(i, strat)
	}

	wg.Wait()
	return results
}
