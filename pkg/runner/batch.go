package runner

import (
	"sort"
	"strings"
	"sync"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/jmoiron/sqlx"
)

// DefaultBatchSize is how many strategies share one bar load in RunBatched.
const DefaultBatchSize = 200

// BatchOptions control RunBatched.
type BatchOptions struct {
	DB           *sqlx.DB
	Table        string
	Start, End   string // bar window passed to FetchBars
	SymbolFilter string // extra symbol every batch also loads (backtest -symbol)
	Workers      int
	BatchSize    int // strategies per bar load; 0 = DefaultBatchSize
}

// BatchRunFunc runs one strategy against the bars of its batch.
type BatchRunFunc func(s strategy.Strategy, bars map[string][]models.Bar, dates []string) RunResult

// RunBatched runs every strategy and returns the results in input order. Bars
// are loaded per batch, only for the symbols that batch's strategies declare,
// and released before the next batch, so memory is bounded by the batch rather
// than by every symbol in the market DB. A strategy that declares no symbols
// (a universe-wide pipeline) cannot be narrowed: those run together in one
// final batch that loads every symbol.
func RunBatched(opts BatchOptions, strats []strategy.Strategy, run BatchRunFunc) ([]RunResult, error) {
	results := make([]RunResult, len(strats))
	size := opts.BatchSize
	if size <= 0 {
		size = DefaultBatchSize
	}

	var scoped, universe []int
	for i, s := range strats {
		if p, ok := s.(strategy.RequiredSymbolsProvider); ok && len(p.RequiredSymbols()) > 0 {
			scoped = append(scoped, i)
		} else {
			universe = append(universe, i)
		}
	}
	// Order by id so a batch holds neighbours, which usually share symbols.
	sort.SliceStable(scoped, func(a, b int) bool {
		return strings.ToLower(strats[scoped[a]].ID()) < strings.ToLower(strats[scoped[b]].ID())
	})

	runBatch := func(idx []int, symbols []string) error {
		bars, dates, err := storage.FetchBars(opts.DB, opts.Table, symbols, opts.Start, opts.End)
		if err != nil {
			return err
		}
		workers := opts.Workers
		if workers < 1 {
			workers = 1
		}
		if workers > len(idx) {
			workers = len(idx)
		}
		jobs := make(chan int, len(idx))
		for _, i := range idx {
			jobs <- i
		}
		close(jobs)
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					results[i] = run(strats[i], bars, dates)
				}
			}()
		}
		wg.Wait()
		return nil
	}

	for from := 0; from < len(scoped); from += size {
		to := from + size
		if to > len(scoped) {
			to = len(scoped)
		}
		batch := scoped[from:to]
		group := make([]strategy.Strategy, len(batch))
		for k, i := range batch {
			group[k] = strats[i]
		}
		if err := runBatch(batch, RequiredSymbolsFor(group, opts.SymbolFilter)); err != nil {
			return results, err
		}
	}
	if len(universe) > 0 {
		if err := runBatch(universe, nil); err != nil {
			return results, err
		}
	}
	return results, nil
}
