package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

// SignalScanRow is one strategy's status as of the live as-of bar.
// Same shape livescan has always written to livescan_status.
type SignalScanRow struct {
	StrategyID   string `json:"strategy_id"`
	StrategyName string `json:"strategy_name"`
	Status       string `json:"status"` // "ENTER" or "NO_SIGNAL"
	Symbols      string `json:"symbols"`
	SignalDate   string `json:"signal_date"`
}

// SignalDetail is one ENTER-eligible signal on the as-of bar (priced).
// Persisted to livescan_signals and emitted in -json so trade_orchestrator
// can stage orders without re-running GenerateSignals.
type SignalDetail struct {
	StrategyID string  `json:"strategy_id"`
	Symbol     string  `json:"symbol"`
	Direction  string  `json:"direction"`
	Date       string  `json:"date"`
	Price      float64 `json:"price"`
	BuyLimit   float64 `json:"buy_limit,omitempty"`
	Close      float64 `json:"close,omitempty"`
	TakeProfit float64 `json:"take_profit,omitempty"`
	StopLoss   float64 `json:"stop_loss,omitempty"`
}

// SignalScanOptions configures the live/signal-only window over the same
// GenerateSignals path backtest uses before simulation.
type SignalScanOptions struct {
	MarketDB      string
	Table         string
	Strategies    []strategy.Strategy
	SymbolFilter  string
	BarsLimit     int // 0 = max MinHistoryBarsFor among strategies
	AutoDownload  bool
	DownloadYears int
	Concurrency   int
	OutDir        string    // livescan.db parent; empty skips persist
	Now           time.Time // tests; zero → time.Now()
}

// SignalScanResult is the full live-window scan output.
type SignalScanResult struct {
	AsOf          string          `json:"as_of"`
	NextSession   string          `json:"next_session"`
	TipDate       string          `json:"tip_date"`
	Rows          []SignalScanRow `json:"rows"`
	Signals       []SignalDetail  `json:"signals"`
	SymbolsLoaded int             `json:"symbols_loaded"`
	OutDBPath     string          `json:"out_db_path"`
}

// RunSignalScan is the shared backtest signal phase with the window fixed to
// the live tip bar (buy signal for today / next session). livescan is a thin
// CLI wrapper around this; backtest -signals-only can call it too.
func RunSignalScan(opts SignalScanOptions) (*SignalScanResult, error) {
	if len(opts.Strategies) == 0 {
		return nil, fmt.Errorf("no strategies")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	outDir := opts.OutDir
	if outDir == "" {
		outDir = "."
	}
	livescanDBPath := filepath.Join(outDir, "livescan.db")
	for _, s := range opts.Strategies {
		s.SetDatabases(opts.MarketDB, livescanDBPath)
	}

	var knownSymbols []string
	if opts.SymbolFilter != "" {
		for _, p := range strings.Split(opts.SymbolFilter, ",") {
			if t := strings.ToUpper(strings.TrimSpace(p)); t != "" {
				knownSymbols = append(knownSymbols, t)
			}
		}
	} else if allStrategiesDeclareSymbols(opts.Strategies) {
		knownSymbols = RequiredSymbolsFor(opts.Strategies, "")
	}

	minBars := opts.BarsLimit
	if minBars <= 0 {
		for _, s := range opts.Strategies {
			if n := strategy.MinHistoryBarsFor(s); n > minBars {
				minBars = n
			}
		}
	}
	scanYears := minBars/240 + 1
	if opts.DownloadYears > 0 && scanYears > opts.DownloadYears {
		scanYears = opts.DownloadYears
	}
	if scanYears < 1 {
		scanYears = 1
	}

	// Default: fill market history for the live window before scanning.
	// Never soft-continue on cache — download failure and stale tip both abort.
	symbolsForRefresh := knownSymbols
	if len(symbolsForRefresh) == 0 {
		symbolsForRefresh = RequiredSymbolsFor(opts.Strategies, opts.SymbolFilter)
	}
	if opts.AutoDownload {
		fmt.Printf("\n📥 Refreshing market history for live window (%d symbol(s), %d yr)...\n", len(symbolsForRefresh), scanYears)
		if len(symbolsForRefresh) > 0 {
			if err := RunDownload(opts.MarketDB, opts.Table, symbolsForRefresh, scanYears); err != nil {
				return nil, fmt.Errorf("MARKET_DATA_REFRESH_FAILED: download for %v into %s (%s) failed: %w", symbolsForRefresh, opts.MarketDB, opts.Table, err)
			}
		} else {
			if err := DetectAndDownloadMissingData(opts.MarketDB, opts.Table, opts.Strategies, opts.SymbolFilter, true, scanYears); err != nil {
				return nil, fmt.Errorf("MARKET_DATA_REFRESH_FAILED: %w", err)
			}
		}
	} else {
		fmt.Println("\n⚠️  -auto-download=false: skipping refresh; will still refuse a stale tip")
	}

	db, err := storage.OpenSQLite(opts.MarketDB)
	if err != nil {
		return nil, fmt.Errorf("open market DB %s: %w", opts.MarketDB, err)
	}
	defer db.Close()

	barsBySymbol, sortedDates, err := storage.FetchRecentBars(db, opts.Table, knownSymbols, minBars)
	if err != nil {
		return nil, fmt.Errorf("load recent bars from %s (%s): %w", opts.MarketDB, opts.Table, err)
	}
	if len(barsBySymbol) == 0 || len(sortedDates) == 0 {
		return nil, fmt.Errorf("no historical bars in %s (%s) after refresh", opts.MarketDB, opts.Table)
	}
	tip := sortedDates[len(sortedDates)-1]
	asOf, nextSession, err := ResolveLiveAsOf(tip, now)
	if err != nil {
		return nil, err
	}

	type scanOut struct {
		row  SignalScanRow
		sigs []SignalDetail
	}
	workers := opts.Concurrency
	if workers < 1 {
		workers = 1
	}
	if workers > len(opts.Strategies) {
		workers = len(opts.Strategies)
	}
	jobs := make(chan strategy.Strategy, len(opts.Strategies))
	for _, s := range opts.Strategies {
		jobs <- s
	}
	close(jobs)
	resultsChan := make(chan scanOut, len(opts.Strategies))
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for strat := range jobs {
				sigs := strat.GenerateSignals(barsBySymbol)
				row, details := buildSignalScanRow(strat, sigs, asOf)
				resultsChan <- scanOut{row: row, sigs: details}
			}
		}()
	}
	wg.Wait()
	close(resultsChan)

	var rows []SignalScanRow
	var details []SignalDetail
	for r := range resultsChan {
		rows = append(rows, r.row)
		details = append(details, r.sigs...)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].StrategyID < rows[j].StrategyID })
	sort.Slice(details, func(i, j int) bool {
		if details[i].StrategyID != details[j].StrategyID {
			return details[i].StrategyID < details[j].StrategyID
		}
		return details[i].Symbol < details[j].Symbol
	})

	out := &SignalScanResult{
		AsOf:          asOf,
		NextSession:   nextSession,
		TipDate:       tip,
		Rows:          rows,
		Signals:       details,
		SymbolsLoaded: len(barsBySymbol),
		OutDBPath:     livescanDBPath,
	}
	if opts.OutDir != "" {
		if err := SaveSignalScanRows(livescanDBPath, rows); err != nil {
			return out, fmt.Errorf("save livescan_status: %w", err)
		}
		if err := SaveSignalDetails(livescanDBPath, details); err != nil {
			return out, fmt.Errorf("save livescan_signals: %w", err)
		}
	}
	return out, nil
}

func allStrategiesDeclareSymbols(strategies []strategy.Strategy) bool {
	for _, s := range strategies {
		declares := false
		if rp, ok := s.(strategy.RequiredSymbolsProvider); ok && len(rp.RequiredSymbols()) > 0 {
			declares = true
		}
		if !declares && strings.TrimSpace(s.DefaultConfig().Benchmark) != "" {
			declares = true
		}
		if !declares {
			return false
		}
	}
	return true
}

func buildSignalScanRow(strat strategy.Strategy, signals []models.Signal, asOf string) (SignalScanRow, []SignalDetail) {
	seen := map[string]bool{}
	var parts []string
	var details []SignalDetail
	for _, sig := range signals {
		if sig.Date != asOf {
			continue
		}
		dir := sig.Direction
		if dir == "" {
			dir = "LONG"
		}
		if dir != "LONG" && dir != "BUY" {
			continue
		}
		sym := strings.ToUpper(strings.TrimSpace(sig.Symbol))
		if sym == "" {
			continue
		}
		key := sym + ":" + dir
		if seen[key] {
			continue
		}
		seen[key] = true
		parts = append(parts, key)
		price := sig.BuyLimit
		if price <= 0 {
			price = sig.Close
		}
		details = append(details, SignalDetail{
			StrategyID: strat.ID(),
			Symbol:     sym,
			Direction:  dir,
			Date:       asOf,
			Price:      price,
			BuyLimit:   sig.BuyLimit,
			Close:      sig.Close,
			TakeProfit: sig.TakeProfit,
			StopLoss:   sig.StopLoss,
		})
	}
	status := "NO_SIGNAL"
	if len(parts) > 0 {
		status = "ENTER"
	}
	return SignalScanRow{
		StrategyID:   strat.ID(),
		StrategyName: strat.Name(),
		Status:       status,
		Symbols:      strings.Join(parts, ", "),
		SignalDate:   asOf,
	}, details
}

// SaveSignalScanRows upserts livescan_status (same schema livescan always used).
func SaveSignalScanRows(dbPath string, rows []SignalScanRow) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return err
	}
	db, err := storage.OpenSQLite(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS livescan_status (
			strategy_id   TEXT PRIMARY KEY,
			strategy_name TEXT,
			status        TEXT NOT NULL,
			symbols       TEXT,
			signal_date   TEXT,
			scanned_at    DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := db.Exec(`
			INSERT INTO livescan_status (strategy_id, strategy_name, status, symbols, signal_date, scanned_at)
			VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(strategy_id) DO UPDATE SET
				strategy_name = excluded.strategy_name,
				status        = excluded.status,
				symbols       = excluded.symbols,
				signal_date   = excluded.signal_date,
				scanned_at    = excluded.scanned_at
		`, r.StrategyID, r.StrategyName, r.Status, r.Symbols, r.SignalDate); err != nil {
			return err
		}
	}
	return nil
}

// SaveSignalDetails replaces livescan_signals with this scan's priced ENTER rows.
func SaveSignalDetails(dbPath string, details []SignalDetail) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return err
	}
	db, err := storage.OpenSQLite(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS livescan_signals (
			strategy_id TEXT NOT NULL,
			symbol      TEXT NOT NULL,
			direction   TEXT,
			date        TEXT NOT NULL,
			price       REAL NOT NULL,
			buy_limit   REAL,
			close       REAL,
			take_profit REAL,
			stop_loss   REAL,
			PRIMARY KEY (strategy_id, symbol, date)
		);
	`); err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM livescan_signals`); err != nil {
		return err
	}
	for _, d := range details {
		if _, err := db.Exec(`
			INSERT INTO livescan_signals (strategy_id, symbol, direction, date, price, buy_limit, close, take_profit, stop_loss)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, d.StrategyID, d.Symbol, d.Direction, d.Date, d.Price, d.BuyLimit, d.Close, d.TakeProfit, d.StopLoss); err != nil {
			return err
		}
	}
	return nil
}

// MarshalSignalScanJSON is the machine-readable livescan contract for orchestrator.
func MarshalSignalScanJSON(res *SignalScanResult) ([]byte, error) {
	return json.MarshalIndent(res, "", "  ")
}
