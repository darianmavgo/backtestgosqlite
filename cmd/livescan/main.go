// cmd/livescan — scans the live market for each selected strategy's buy
// signal as of the latest available bar and writes a simple per-strategy
// status table (ENTER / NO_SIGNAL) to SQLite. Arguments mirror cmd/backtest
// where they mean the same thing (-db, -table, -strategy, -symbol, -out-dir,
// -auto-download, -download-years, -concurrency, -list); -bars is livescan's
// own addition, since it only loads a recent window of bars rather than full
// history.
//
// Usage:
//
//	go run cmd/livescan/main.go -strategy bb-capitulation
//	go run cmd/livescan/main.go -strategy gld-decline,sig-voo-buy-tecl
//	go run cmd/livescan/main.go all
//	go run cmd/livescan/main.go -list
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/darianmavgo/backtestgosqlite/pkg/cliutils"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/runner"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
)

// scanResult is one strategy's live-signal status as of the latest scanned bar.
type scanResult struct {
	StrategyID   string
	StrategyName string
	Status       string // "ENTER" or "NO_SIGNAL"
	Symbols      string // e.g. "TECL:LONG, SPXU:SHORT" — empty when NO_SIGNAL
	SignalDate   string
}

func main() {
	defaultMarketDb := cliutils.GetDefaultMarketDB()

	targetDb := flag.String("db", defaultMarketDb, "Path to source SQLite DB containing historical market bars")
	tableName := flag.String("table", "backtest_start", "Table name containing historical bars")
	strategyType := flag.String("strategy", "", "Strategy ID to scan, comma-separated list, or 'all'")
	symbolFilter := flag.String("symbol", "", "Optional: restrict scan to a specific symbol (e.g. SOXL)")
	outDir := flag.String("out-dir", "reports", "Directory to write the live-signal status SQLite table")
	listFlag := flag.Bool("list", false, "List all registered Go and SQL strategies")
	autoDownload := flag.Bool("auto-download", true, "Automatically detect missing market data and run download")
	downloadYears := flag.Int("download-years", 5, "Number of years of history to fetch when downloading missing data")
	concurrency := flag.Int("concurrency", runtime.NumCPU(), "Max concurrent strategy signal-generation workers. Defaults to all CPU cores.")
	barsLimit := flag.Int("bars", 250, "Number of recent historical bars per symbol to load for indicator calculations")
	flag.Parse()

	// Auto-discover any SQL pipeline strategies in sql/strategies/
	strategy.AutoRegisterSQLStrategies(".", *targetDb)

	if *listFlag {
		runner.PrintStrategyList()
		return
	}

	// Resolve strategies from flags or positional arguments, same convention
	// as cmd/backtest.
	stratArg := strings.TrimSpace(*strategyType)
	if stratArg == "" && len(flag.Args()) > 0 {
		stratArg = strings.Join(flag.Args(), ",")
	}
	if stratArg == "" {
		stratArg = "bb-capitulation"
	}

	var selectedStrategies []strategy.Strategy
	if strings.EqualFold(stratArg, "all") {
		selectedStrategies = strategy.List()
	} else {
		for _, token := range strings.FieldsFunc(stratArg, func(r rune) bool { return r == ',' || r == ' ' }) {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			s, exists := strategy.Get(token)
			if !exists {
				log.Fatalf("Strategy '%s' not found in registry. Run with -list to view available strategies.", token)
			}
			selectedStrategies = append(selectedStrategies, s)
		}
	}
	if len(selectedStrategies) == 0 {
		log.Fatalf("No valid strategies selected. Run with -list to view available strategies.")
	}

	// A single persistent file backs both strategies' calc tables (SQL-
	// pipeline slice/signal tables, and genetic-momentum's Python-subprocess
	// predictions — needs a real file, not ":memory:", which is a private,
	// per-connection database no other process can ever see) and the final
	// livescan_status results table written at the end of the run. Nothing
	// temporary to clean up.
	livescanDBPath := filepath.Join(*outDir, "livescan.db")
	for _, s := range selectedStrategies {
		s.SetDatabases(*targetDb, livescanDBPath)
	}

	// Resolve which symbols we can scope to: an explicit -symbol filter
	// always wins; otherwise, only if EVERY selected strategy declares its
	// own specific symbols (RequiredSymbols()/Benchmark) — a universe-wide
	// strategy like bb-capitulation declares none, and mixing it in means
	// there's no safe subset short of the whole table.
	var knownSymbols []string
	if *symbolFilter != "" {
		for _, p := range strings.Split(*symbolFilter, ",") {
			if t := strings.ToUpper(strings.TrimSpace(p)); t != "" {
				knownSymbols = append(knownSymbols, t)
			}
		}
	} else if allStrategiesDeclareSymbols(selectedStrategies) {
		knownSymbols = runner.RequiredSymbolsFor(selectedStrategies, "")
	}

	if len(knownSymbols) > 0 {
		// Actually make this "live": refresh these specific symbols to the
		// latest close before every scan, not just when one is missing
		// outright. cmd/download is cache-first — pulling only the missing
		// tail costs ~10ms per already-current symbol (confirmed: 3 symbols,
		// 0 new bars, 14ms total) — so this is cheap when the cache is
		// already fresh and is exactly what closes the gap that made
		// "LATEST MARKET CLOSE" show a stale date when the local cache
		// hadn't been refreshed in days.
		if *autoDownload {
			fmt.Printf("\n🔄 Refreshing %d symbol(s) to the latest close...\n", len(knownSymbols))
			if err := runner.RunDownload(*targetDb, *tableName, knownSymbols, *downloadYears); err != nil {
				log.Printf("Warning: failed to refresh market data (%v) — continuing with cached data", err)
			}
		}
	} else {
		// Universe-wide scan (no specific symbols declared by every selected
		// strategy) — can't cheaply keep 1800+ symbols current on every
		// invocation, so just make sure the table isn't completely empty.
		if err := runner.DetectAndDownloadMissingData(*targetDb, *tableName, selectedStrategies, *symbolFilter, *autoDownload, *downloadYears); err != nil {
			log.Fatalf("Market data resolution error: %v", err)
		}
	}

	db, err := storage.OpenSQLite(*targetDb)
	if err != nil {
		log.Fatalf("Failed to open market DB %s: %v", *targetDb, err)
	}
	defer db.Close()

	barsBySymbol, sortedDates, err := storage.FetchRecentBars(db, *tableName, knownSymbols, *barsLimit)
	if err != nil {
		log.Fatalf("Error loading recent bars for live scan: %v", err)
	}
	if len(barsBySymbol) == 0 || len(sortedDates) == 0 {
		log.Fatalf("No historical bar data found in %s (%s). Run ./bin/download first to cache data.", *targetDb, *tableName)
	}
	latestDate := sortedDates[len(sortedDates)-1]

	fmt.Printf("\n========================================================================================\n")
	fmt.Printf("⚡ LIVE SIGNAL SCAN\n")
	fmt.Printf("📅 LATEST MARKET CLOSE : %s\n", latestDate)
	fmt.Printf("📊 UNIVERSE MONITORED  : %d symbols across %d strategies\n", len(barsBySymbol), len(selectedStrategies))
	fmt.Printf("========================================================================================\n")

	// Bounded worker pool — unbounded one-goroutine-per-strategy here can
	// strain the machine once there are hundreds of registered strategies
	// (some, like genetic-momentum, shell out to Python; the dt_* ones each
	// fit a fresh CloudForest decision tree). Same fix as cmd/backtest/
	// cmd/scoreboard.
	workers := *concurrency
	if workers < 1 {
		workers = 1
	}
	if workers > len(selectedStrategies) {
		workers = len(selectedStrategies)
	}
	jobs := make(chan strategy.Strategy, len(selectedStrategies))
	for _, s := range selectedStrategies {
		jobs <- s
	}
	close(jobs)

	resultsChan := make(chan scanResult, len(selectedStrategies))
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for strat := range jobs {
				signals := strat.GenerateSignals(barsBySymbol)
				resultsChan <- buildScanResult(strat, signals, latestDate)
			}
		}()
	}
	wg.Wait()
	close(resultsChan)

	var results []scanResult
	for r := range resultsChan {
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].StrategyID < results[j].StrategyID })

	printScanTable(results)

	if err := saveScanResults(livescanDBPath, results); err != nil {
		log.Printf("Warning: failed to save live-scan status to %s: %v", livescanDBPath, err)
	} else {
		fmt.Printf("\n💾 Live-signal status saved to SQLite: %s (table: livescan_status)\n\n", livescanDBPath)
	}
}

// allStrategiesDeclareSymbols reports whether every strategy in strategies
// declares its own specific symbols (RequiredSymbols() or a Benchmark) — a
// universe-wide strategy like bb-capitulation declares neither. Only when
// this holds is it safe to scope the data refresh and bar-loading to
// runner.RequiredSymbolsFor's result instead of the whole database: the
// union would otherwise silently omit whatever a universe-wide strategy
// actually needs (everything).
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

// buildScanResult reduces a strategy's raw signals down to today's status:
// ENTER if any signal fired on the latest scanned bar, NO_SIGNAL otherwise.
func buildScanResult(strat strategy.Strategy, signals []models.Signal, latestDate string) scanResult {
	seen := make(map[string]bool)
	var parts []string
	for _, sig := range signals {
		if sig.Date != latestDate {
			continue
		}
		dir := sig.Direction
		if dir == "" {
			dir = "LONG"
		}
		key := sig.Symbol + ":" + dir
		if seen[key] {
			continue
		}
		seen[key] = true
		parts = append(parts, key)
	}
	status := "NO_SIGNAL"
	if len(parts) > 0 {
		status = "ENTER"
	}
	return scanResult{
		StrategyID:   strat.ID(),
		StrategyName: strat.Name(),
		Status:       status,
		Symbols:      strings.Join(parts, ", "),
		SignalDate:   latestDate,
	}
}

func printScanTable(results []scanResult) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Strategy", "Status", "Symbols", "Signal Date"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	entering := 0
	for _, r := range results {
		status := "⚪ NO_SIGNAL"
		if r.Status == "ENTER" {
			status = "🟢 ENTER"
			entering++
		}
		table.Append([]string{r.StrategyID, status, truncateSymbols(r.Symbols, 8), r.SignalDate})
	}
	table.Render()
	fmt.Printf("\n%d/%d strategies have an entry signal as of the latest bar.\n", entering, len(results))
}

// truncateSymbols caps the console table's Symbols column at maxShown
// entries — a universe-scanning strategy (e.g. bb-capitulation) can trigger
// on dozens of symbols in one day, which makes the console table unreadable.
// The full, untruncated list is always what's written to livescan.db.
func truncateSymbols(symbols string, maxShown int) string {
	if symbols == "" {
		return ""
	}
	parts := strings.Split(symbols, ", ")
	if len(parts) <= maxShown {
		return symbols
	}
	return fmt.Sprintf("%s, ... (+%d more, see livescan.db)", strings.Join(parts[:maxShown], ", "), len(parts)-maxShown)
}

// saveScanResults upserts each strategy's status into livescan_status, keyed
// by strategy_id — a live scan reflects current market state, so re-running
// it should overwrite each strategy's row rather than accumulate a history
// log (that's what a backtest's own result DB is for).
func saveScanResults(dbPath string, results []scanResult) error {
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

	for _, r := range results {
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
