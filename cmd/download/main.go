package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/datasource"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

func getSymbolsFromTable(settingsDbPath, tableName string, limit int) ([]string, error) {
	db, err := sqlx.Open("sqlite", settingsDbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var symbols []string
	query := fmt.Sprintf("SELECT DISTINCT symbol FROM %s WHERE symbol != ''", tableName)
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	err = db.Select(&symbols, query)
	return symbols, err
}

func readUniverse(settingsDbPath, list string) ([]string, error) {
	db, err := refdb.Open(settingsDbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return refdb.Universe(db, list)
}

func fetchWithFallback(ctx context.Context, primary, fallback datasource.DataSource, req datasource.FetchRequest) ([]models.Bar, error) {
	bars, err := primary.Fetch(ctx, req)
	if err == nil && len(bars) > 0 {
		return bars, nil
	}
	if fallback != nil {
		log.Printf("Primary source (%s) failed for %s: %v. Attempting fallback to %s...", primary.Name(), req.Symbol, err, fallback.Name())
		fallbackBars, fallbackErr := fallback.Fetch(ctx, req)
		if fallbackErr == nil && len(fallbackBars) > 0 {
			return fallbackBars, nil
		}
		return nil, fmt.Errorf("%s failed: %w (fallback %s failed: %v)", primary.Name(), err, fallback.Name(), fallbackErr)
	}
	return nil, err
}

func main() {
	targetDb := flag.String("db", appenv.MarketDB(), "Target SQLite DB path for market history (default: APP_FOLDER/data/market_history.db)")
	settingsDb := flag.String("settings", appenv.RefDB(), "Reference DB path (etf_universe lists and symbol tables)")
	sourceType := flag.String("source", "yahoo", "Data source provider: yahoo, polygon, stooq, csv")
	polygonKey := flag.String("polygon-key", "", "Polygon.io API key (or set POLYGON_API_KEY in environment or .env)")
	csvPath := flag.String("csv", "", "Path to CSV file or directory of CSV files (used with -source csv)")
	symbolFlag := flag.String("symbols", "", "Comma-separated list of symbols to download/import (e.g. SPY,QQQ,TQQQ)")
	universeList := flag.String("list", "", "etf_universe list in the settings DB to download (all, 6yr, sweep)")
	table := flag.String("table", "leveraged_etf", "Table name in settings.db with symbols (fallback if no symbols specified)")
	limit := flag.Int("limit", 50, "Limit number of symbols (0 for all)")
	years := flag.Int("years", 4, "Number of years of history")
	startFlag := flag.String("start", "", "Optional start date (YYYY-MM-DD), overrides -years")
	endFlag := flag.String("end", "", "Optional end date (YYYY-MM-DD), defaults to now")
	timeframe := flag.String("timeframe", "1d", "Bar timeframe (1d, 1h, 5m, 1m)")
	targetTable := flag.String("target-table", "backtest_start", "Target table name in target SQLite DB")
	forceDownload := flag.Bool("force", false, "Force re-downloading all bars even if already present in database")
	seedDb := flag.String("seed-db", appenv.DataFile("leveraged_backtest.db"), "Legacy database to seed from if target DB doesn't exist")
	concurrency := flag.Int("concurrency", runtime.NumCPU(), "Number of symbols to fetch concurrently (network-bound; DB writes are serialized internally). Defaults to all CPU cores.")
	flag.Parse()

	// If default target DB does not exist yet, seed it from existing leveraged_backtest.db if available
	if *targetDb == appenv.MarketDB() {
		if _, err := os.Stat(*targetDb); os.IsNotExist(err) {
			if _, errLegacy := os.Stat(*seedDb); errLegacy == nil {
				if input, err := os.ReadFile(*seedDb); err == nil {
					_ = os.MkdirAll(appenv.Data(), 0755)
					_ = os.WriteFile(*targetDb, input, 0644)
					fmt.Printf("📦 Initialized %s from existing historical data cache.\n", *targetDb)
				}
			}
		}
	}

	db, err := storage.OpenSQLite(*targetDb)
	if err != nil {
		log.Fatalf("Failed to open target DB %s: %v", *targetDb, err)
	}
	defer db.Close()

	if err := storage.EnsureBarTable(db, *targetTable); err != nil {
		log.Fatalf("Failed to initialize database table schema: %v", err)
	}

	ctx := context.Background()

	// 1. Handle direct CSV ingestion
	if strings.ToLower(*sourceType) == "csv" || *csvPath != "" {
		if *csvPath == "" {
			log.Fatalf("Please provide CSV file or directory path using -csv <path>")
		}
		fmt.Printf("📂 Ingesting historical market data from CSV: %s\n", *csvPath)
		csvSource := datasource.NewCSVDataSource(*csvPath, nil)
		req := datasource.FetchRequest{
			Symbol:    *symbolFlag,
			Timeframe: *timeframe,
		}
		bars, err := csvSource.Fetch(ctx, req)
		if err != nil {
			log.Fatalf("Failed to parse CSV %s: %v", *csvPath, err)
		}

		if err := storage.UpsertBars(db, *targetTable, bars); err != nil {
			log.Fatalf("Failed to save bars to SQLite DB: %v", err)
		}

		fmt.Printf("✅ Successfully ingested %d bars from CSV into %s (%s)\n", len(bars), *targetDb, *targetTable)
		return
	}

	// 2. Resolve symbol list
	var symbols []string
	if *symbolFlag != "" {
		parts := strings.Split(*symbolFlag, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(strings.ToUpper(p))
			if trimmed != "" {
				symbols = append(symbols, trimmed)
			}
		}
	} else if *universeList != "" {
		listSymbols, err := readUniverse(*settingsDb, *universeList)
		if err != nil {
			log.Fatalf("Failed to read etf_universe list %q from %s: %v", *universeList, *settingsDb, err)
		}
		symbols = listSymbols
	} else {
		dbSymbols, err := getSymbolsFromTable(*settingsDb, *table, *limit)
		if err != nil {
			log.Printf("Warning: failed to query settings.db table %s: %v", *table, err)
		}
		symbols = dbSymbols
	}

	if len(symbols) == 0 {
		log.Fatalf("No symbols resolved. Specify -symbols SPY,QQQ or -list <name> or -table <name>")
	}

	fmt.Printf("Database: %s (Table: %s)\n", *targetDb, *targetTable)
	fmt.Printf("Target Universe: %d symbols: %v\n", len(symbols), symbols)

	client := &http.Client{Timeout: 15 * time.Second}
	var primarySource datasource.DataSource
	var fallbackSource datasource.DataSource

	switch strings.ToLower(*sourceType) {
	case "polygon":
		polySource := datasource.NewPolygonDataSource(*polygonKey, client)
		if polySource.APIKey == "" {
			log.Fatalf("Polygon API key is required. Pass -polygon-key <KEY> or set POLYGON_API_KEY in your environment / .env file")
		}
		primarySource = polySource
		fallbackSource = datasource.NewYahooDataSource(client)
	case "stooq":
		primarySource = datasource.NewStooqDataSource(client)
	default:
		primarySource = datasource.NewYahooDataSource(client)
		fallbackSource = datasource.NewStooqDataSource(client)
	}

	now := time.Now().UTC()
	start := now.AddDate(-*years, 0, 0)
	end := now

	if *startFlag != "" {
		if parsed, err := time.Parse("2006-01-02", *startFlag); err == nil {
			start = parsed.UTC()
		} else {
			log.Fatalf("Invalid -start date %q: expected YYYY-MM-DD", *startFlag)
		}
	}
	if *endFlag != "" {
		if parsed, err := time.Parse("2006-01-02", *endFlag); err == nil {
			end = parsed.UTC()
		} else {
			log.Fatalf("Invalid -end date %q: expected YYYY-MM-DD", *endFlag)
		}
	}

	reqStartStr := start.Format("2006-01-02")
	reqEndStr := end.Format("2006-01-02")

	fmt.Printf("Target Horizon: %s bars from %s to %s using %s\n",
		*timeframe, reqStartStr, reqEndStr, primarySource.Name())
	fmt.Printf("🔍 Checking %s first for existing cached data...\n\n", filepath.Base(*targetDb))

	totalNewBars := 0
	cachedSymbols := 0
	updatedSymbols := 0
	failedSymbols := 0

	var mu sync.Mutex // serializes all DB access and shared counters/printing (SQLite allows only one writer at a time)

	processSymbol := func(idx int, sym string) {
		mu.Lock()
		cov, err := storage.GetSymbolDateCoverageWithTimeframe(db, *targetTable, sym, *timeframe)
		mu.Unlock()
		if err != nil {
			log.Printf("[%d/%d] Error checking database coverage for %s: %v", idx+1, len(symbols), sym, err)
		}

		// Determine missing date windows
		var missingWindows []datasource.FetchRequest

		if cov.BarCount == 0 || *forceDownload {
			// Symbol is completely missing from DB (or forced full refresh)
			missingWindows = append(missingWindows, datasource.FetchRequest{
				Symbol:    sym,
				StartDate: start,
				EndDate:   end,
				Timeframe: *timeframe,
			})
		} else {
			// Symbol already exists in DB from cov.MinDate to cov.MaxDate
			// 1. Check if older historical bars are missing (start is before cov.MinDate by more than a weekend/holiday)
			if reqStartStr < cov.MinDate {
				dbMin, err := time.Parse("2006-01-02", cov.MinDate)
				if err == nil && dbMin.Sub(start) > 4*24*time.Hour {
					missingWindows = append(missingWindows, datasource.FetchRequest{
						Symbol:    sym,
						StartDate: start,
						EndDate:   dbMin,
						Timeframe: *timeframe,
					})
				}
			}

			// 2. Check if newer historical bars are missing (end is after cov.MaxDate)
			if cov.MaxDate < reqEndStr {
				dbMax, err := time.Parse("2006-01-02", cov.MaxDate)
				if err == nil && cov.MaxDate != reqEndStr {
					missingWindows = append(missingWindows, datasource.FetchRequest{
						Symbol:    sym,
						StartDate: dbMax,
						EndDate:   end,
						Timeframe: *timeframe,
					})
				}
			}
		}

		// If DB already has all requested data, skip remote fetch!
		if len(missingWindows) == 0 {
			mu.Lock()
			cachedSymbols++
			fmt.Printf("[%d/%d] %-6s : ⚡ Up-to-date in %s (%d bars, %s ➔ %s). 0 missing, skipped remote fetch.\n",
				idx+1, len(symbols), sym, filepath.Base(*targetDb), cov.BarCount, cov.MinDate, cov.MaxDate)
			mu.Unlock()
			return
		}

		// Fetch only the missing window(s) — network I/O happens outside the lock so
		// multiple symbols can be in flight concurrently.
		symNewBars := 0
		fetchError := false

		for _, winReq := range missingWindows {
			bars, err := fetchWithFallback(ctx, primarySource, fallbackSource, winReq)
			if err != nil {
				log.Printf("[%d/%d] Failed fetching missing data for %s (%s to %s): %v",
					idx+1, len(symbols), sym, winReq.StartDate.Format("2006-01-02"), winReq.EndDate.Format("2006-01-02"), err)
				fetchError = true
				continue
			}
			if len(bars) == 0 {
				continue
			}

			mu.Lock()
			err = storage.UpsertBars(db, *targetTable, bars)
			mu.Unlock()
			if err != nil {
				log.Printf("[%d/%d] Error saving %s bars to DB: %v", idx+1, len(symbols), sym, err)
				fetchError = true
				continue
			}
			symNewBars += len(bars)
		}

		mu.Lock()
		defer mu.Unlock()

		if fetchError && symNewBars == 0 && cov.BarCount == 0 {
			failedSymbols++
			return
		}

		totalNewBars += symNewBars
		updatedSymbols++

		if cov.BarCount == 0 {
			fmt.Printf("[%d/%d] %-6s : 📥 Not in DB. Pulled %d bars (%s ➔ %s) from %s ➔ %s\n",
				idx+1, len(symbols), sym, symNewBars, reqStartStr, reqEndStr, primarySource.Name(), filepath.Base(*targetDb))
		} else if symNewBars > 0 {
			fmt.Printf("[%d/%d] %-6s : 🔄 Found %d bars in DB (%s ➔ %s). Pulled %d missing bars from %s ➔ %s\n",
				idx+1, len(symbols), sym, cov.BarCount, cov.MinDate, cov.MaxDate, symNewBars, primarySource.Name(), filepath.Base(*targetDb))
		} else {
			fmt.Printf("[%d/%d] %-6s : ⚡ Found %d bars in DB (%s ➔ %s). Checked %s (no new closed bars available).\n",
				idx+1, len(symbols), sym, cov.BarCount, cov.MinDate, cov.MaxDate, primarySource.Name())
		}
	}

	workers := *concurrency
	if workers < 1 {
		workers = 1
	}
	if workers == 1 {
		for idx, sym := range symbols {
			processSymbol(idx, sym)
			time.Sleep(120 * time.Millisecond) // gentle rate limit for serial mode
		}
	} else {
		fmt.Printf("🚀 Downloading with %d concurrent workers...\n\n", workers)
		jobs := make(chan int, len(symbols))
		for idx := range symbols {
			jobs <- idx
		}
		close(jobs)

		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for idx := range jobs {
					processSymbol(idx, symbols[idx])
				}
			}()
		}
		wg.Wait()
	}

	fmt.Printf("\n✨ Summary: %s is updated! (%d symbols already up-to-date, %d symbols fetched/updated, %d failed, %d new bars added).\n   %d requested symbols processed against %s.\n",
		*targetDb, cachedSymbols, updatedSymbols, failedSymbols, totalNewBars, len(symbols), filepath.Base(*targetDb))
}
