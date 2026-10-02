package storage

import (
	"fmt"
	"sync"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// Many workers writing runs into one results.db at the same time must all
// land, each run whole, with distinct run ids.
func TestWriteRunConcurrentWriters(t *testing.T) {
	r, err := OpenResults(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	const workers, perWorker = 16, 10
	var wg sync.WaitGroup
	errs := make(chan error, workers*perWorker)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := fmt.Sprintf("s%d_%d", w, i)
				_, err := r.WriteRun(RunMeta{StrategyID: id, Kind: "single"}, RunPayload{
					Trades: []models.Trade{{Symbol: "VOO", EntryDate: "2024-01-02", ExitDate: "2024-01-05", NetPnL: 5}, {Symbol: "VOO", EntryDate: "2024-02-02", ExitDate: "2024-02-05", NetPnL: -1}},
					Equity: []models.DailyEquityPoint{{Date: "2024-01-02", TotalEquity: 100}, {Date: "2024-01-03", TotalEquity: 101}, {Date: "2024-01-04", TotalEquity: 102}},
					Reports: []NamedReport{{StrategyID: id, Report: models.PerformanceReport{CAGR: float64(i), TotalTrades: 2}}},
				})
				if err != nil {
					errs <- err
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	var runs, trades, curve, perf int
	for table, dst := range map[string]*int{"runs": &runs, "trades": &trades, "equity_curve": &curve, "performance_summary": &perf} {
		if err := r.DB.Get(dst, "SELECT COUNT(*) FROM "+table); err != nil {
			t.Fatal(err)
		}
	}
	n := workers * perWorker
	if runs != n || trades != 2*n || curve != 3*n || perf != n {
		t.Fatalf("runs=%d trades=%d curve=%d perf=%d, want %d/%d/%d/%d", runs, trades, curve, perf, n, 2*n, 3*n, n)
	}
	var distinct int
	if err := r.DB.Get(&distinct, "SELECT COUNT(DISTINCT run_id) FROM trades"); err != nil || distinct != n {
		t.Fatalf("distinct run ids on trades = %d (err %v), want %d", distinct, err, n)
	}
}

// A run that fails midway leaves nothing behind.
func TestWriteRunIsAtomic(t *testing.T) {
	r, err := OpenResults(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// Drop a table the payload writes to, so the write fails after the runs row is inserted.
	if _, err := r.DB.Exec("DROP TABLE equity_curve"); err != nil {
		t.Fatal(err)
	}
	_, err = r.WriteRun(RunMeta{StrategyID: "x"}, RunPayload{
		Trades: []models.Trade{{Symbol: "VOO"}},
		Equity: []models.DailyEquityPoint{{Date: "2024-01-02", TotalEquity: 100}},
	})
	if err == nil {
		t.Fatal("expected a write error")
	}
	var runs, trades int
	_ = r.DB.Get(&runs, "SELECT COUNT(*) FROM runs")
	_ = r.DB.Get(&trades, "SELECT COUNT(*) FROM trades")
	if runs != 0 || trades != 0 {
		t.Fatalf("partial run left behind: runs=%d trades=%d", runs, trades)
	}
}
