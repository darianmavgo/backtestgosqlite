package runner

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/datasource"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

// RunDownload fetches missing daily bars for symbols into targetTable of the
// SQLite DB at targetDb, covering the last `years` years through today. It is
// cmd/download's Yahoo-primary/Stooq-fallback missing-window loop (both keyless)
// as a plain function -- only date windows not already cached are fetched --
// so callers can refresh market data in-process instead of exec'ing a
// separately built `download` binary (or `go run ./cmd/download`), which can't
// exist on a deployment target like App Engine.
//
// A symbol whose fetch fails is only an error when it has no cached bars at
// all (nothing to scan); a failed incremental top-up of an already-cached
// symbol is tolerated, and a genuinely stale tip is still refused downstream
// by ResolveLiveAsOf.
func RunDownload(targetDb, targetTable string, symbols []string, years int) error {
	return RunDownloadContext(context.Background(), targetDb, targetTable, symbols, years)
}

// RunDownloadContext is RunDownload with cancellation: once ctx is done no
// further symbols are started, in-flight fetches are aborted, and ctx's error
// is returned.
func RunDownloadContext(ctx context.Context, targetDb, targetTable string, symbols []string, years int) error {
	if len(symbols) == 0 {
		return nil
	}
	if targetTable == "" {
		targetTable = "backtest_start"
	}

	db, err := storage.OpenSQLite(targetDb)
	if err != nil {
		return fmt.Errorf("open %s: %w", targetDb, err)
	}
	defer db.Close()
	if err := storage.EnsureBarTable(db, targetTable); err != nil {
		return fmt.Errorf("ensure table %s: %w", targetTable, err)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	primary := datasource.NewYahooDataSource(client)
	fallback := datasource.NewStooqDataSource(client)

	now := time.Now().UTC()
	start := now.AddDate(-years, 0, 0)
	startStr, endStr := start.Format("2006-01-02"), now.Format("2006-01-02")

	var (
		mu       sync.Mutex // serializes all DB access and firstErr (SQLite is single-writer)
		firstErr error
		wg       sync.WaitGroup
	)
	fail := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}
	sem := make(chan struct{}, 4) // network-bound; modest fan-out

	for _, sym := range symbols {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(sym string) {
			defer wg.Done()
			defer func() { <-sem }()

			mu.Lock()
			cov, covErr := storage.GetSymbolDateCoverageWithTimeframe(db, targetTable, sym, "1d")
			mu.Unlock()
			if covErr != nil {
				fail(fmt.Errorf("%s: coverage check: %w", sym, covErr))
				return
			}

			var windows []datasource.FetchRequest
			if cov.BarCount == 0 {
				windows = append(windows, datasource.FetchRequest{Symbol: sym, StartDate: start, EndDate: now, Timeframe: "1d"})
			} else {
				if startStr < cov.MinDate {
					if dbMin, perr := time.Parse("2006-01-02", cov.MinDate); perr == nil && dbMin.Sub(start) > 4*24*time.Hour {
						windows = append(windows, datasource.FetchRequest{Symbol: sym, StartDate: start, EndDate: dbMin, Timeframe: "1d"})
					}
				}
				if cov.MaxDate < endStr {
					if dbMax, perr := time.Parse("2006-01-02", cov.MaxDate); perr == nil {
						windows = append(windows, datasource.FetchRequest{Symbol: sym, StartDate: dbMax, EndDate: now, Timeframe: "1d"})
					}
				}
			}

			for _, win := range windows {
				bars, ferr := primary.Fetch(ctx, win)
				if ferr != nil || len(bars) == 0 {
					bars, ferr = fallback.Fetch(ctx, win)
				}
				if ferr != nil {
					if cov.BarCount == 0 {
						fail(fmt.Errorf("%s: fetch %s..%s: %w", sym, win.StartDate.Format("2006-01-02"), win.EndDate.Format("2006-01-02"), ferr))
					} else {
						// Tolerated (the cached bars stay usable), but say so: a stale
						// tip otherwise surfaces only as STALE_MARKET_DATA downstream.
						log.Printf("[download] %s: top-up %s..%s failed, keeping cached bars through %s: %v",
							sym, win.StartDate.Format("2006-01-02"), win.EndDate.Format("2006-01-02"), cov.MaxDate, ferr)
					}
					continue
				}
				if len(bars) == 0 {
					continue
				}
				mu.Lock()
				uerr := storage.UpsertBars(db, targetTable, bars)
				mu.Unlock()
				if uerr != nil {
					fail(fmt.Errorf("%s: save bars: %w", sym, uerr))
				}
			}
		}(sym)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	return firstErr
}
