package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/datasource"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func getSymbolsFromTable(settingsDbPath, tableName string, limit int) ([]string, error) {
	db, err := sqlx.Open("sqlite3", settingsDbPath)
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

func readSymbolsFile(filePath string) ([]string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(content), "\n")
	var symbols []string
	for _, l := range lines {
		sym := strings.TrimSpace(l)
		if sym != "" && !strings.HasPrefix(sym, "#") {
			symbols = append(symbols, strings.ToUpper(sym))
		}
	}
	return symbols, nil
}

func fetchWithFallback(ctx context.Context, primary, fallback datasource.DataSource, req datasource.FetchRequest) ([]models.Bar, error) {
	bars, err := primary.Fetch(ctx, req)
	if (err != nil || len(bars) == 0) && fallback != nil {
		bars, err = fallback.Fetch(ctx, req)
	}
	return bars, err
}

func main() {
	targetDb := flag.String("db", "data/market_history.db", "Target SQLite DB path for market history (default: data/market_history.db)")
	settingsDb := flag.String("settings", "data/settings.db", "Settings DB path (for table seed lookups)")
	sourceType := flag.String("source", "yahoo", "Data source provider: yahoo, stooq, csv")
	csvPath := flag.String("csv", "", "Path to CSV file or directory of CSV files (used with -source csv)")
	symbolFlag := flag.String("symbols", "", "Comma-separated list of symbols to download/import (e.g. SPY,QQQ,TQQQ)")
	symbolsFile := flag.String("symbols-file", "", "Path to text file with one symbol per line")
	table := flag.String("table", "leveraged_etf", "Table name in settings.db with symbols (fallback if no symbols specified)")
	limit := flag.Int("limit", 50, "Limit number of symbols (0 for all)")
	years := flag.Int("years", 4, "Number of years of history")
	timeframe := flag.String("timeframe", "1d", "Bar timeframe (1d, 1h, 5m)")
	targetTable := flag.String("target-table", "backtest_start", "Target table name in target SQLite DB")
	forceDownload := flag.Bool("force", false, "Force re-downloading all bars even if already present in database")
	flag.Parse()

	// If default target DB does not exist yet, seed it from existing leveraged_backtest.db if available
	if *targetDb == "data/market_history.db" {
		if _, err := os.Stat(*targetDb); os.IsNotExist(err) {
			if _, errLegacy := os.Stat("data/leveraged_backtest.db"); errLegacy == nil {
				if input, err := os.ReadFile("data/leveraged_backtest.db"); err == nil {
					_ = os.MkdirAll("data", 0755)
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
	} else if *symbolsFile != "" {
		fileSymbols, err := readSymbolsFile(*symbolsFile)
		if err != nil {
			log.Fatalf("Failed to read symbols file %s: %v", *symbolsFile, err)
		}
		symbols = fileSymbols
	} else {
		dbSymbols, err := getSymbolsFromTable(*settingsDb, *table, *limit)
		if err != nil {
			log.Printf("Warning: failed to query settings.db table %s: %v", *table, err)
		}
		symbols = dbSymbols
	}

	if len(symbols) == 0 {
		log.Fatalf("No symbols resolved. Specify -symbols SPY,QQQ or -symbols-file <path> or -table <name>")
	}

	fmt.Printf("Database: %s (Table: %s)\n", *targetDb, *targetTable)
	fmt.Printf("Target Universe: %d symbols: %v\n", len(symbols), symbols)

	client := &http.Client{Timeout: 15 * time.Second}
	var primarySource datasource.DataSource
	var fallbackSource datasource.DataSource

	if strings.ToLower(*sourceType) == "stooq" {
		primarySource = datasource.NewStooqDataSource(client)
	} else {
		primarySource = datasource.NewYahooDataSource(client)
		fallbackSource = datasource.NewStooqDataSource(client)
	}

	now := time.Now().UTC()
	start := now.AddDate(-*years, 0, 0)
	end := now

	reqStartStr := start.Format("2006-01-02")
	reqEndStr := end.Format("2006-01-02")

	fmt.Printf("Target Horizon: %s bars from %s to %s using %s\n",
		*timeframe, reqStartStr, reqEndStr, primarySource.Name())
	fmt.Printf("🔍 Checking %s first for existing cached data...\n\n", filepath.Base(*targetDb))

	totalNewBars := 0
	cachedSymbols := 0
	updatedSymbols := 0

	for idx, sym := range symbols {
		cov, err := storage.GetSymbolDateCoverage(db, *targetTable, sym)
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
			cachedSymbols++
			fmt.Printf("[%d/%d] %-6s : ⚡ Up-to-date in %s (%d bars, %s ➔ %s). 0 missing, skipped remote fetch.\n",
				idx+1, len(symbols), sym, filepath.Base(*targetDb), cov.BarCount, cov.MinDate, cov.MaxDate)
			continue
		}

		// Fetch only the missing window(s)
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

			if err := storage.UpsertBars(db, *targetTable, bars); err != nil {
				log.Printf("[%d/%d] Error saving %s bars to DB: %v", idx+1, len(symbols), sym, err)
				fetchError = true
				continue
			}
			symNewBars += len(bars)
		}

		if fetchError && symNewBars == 0 && cov.BarCount == 0 {
			continue
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

		time.Sleep(120 * time.Millisecond) // rate limit
	}

	fmt.Printf("\n✨ Summary: %s is updated! (%d symbols already up-to-date, %d symbols fetched/updated, %d new bars added).\n   All %d requested symbols are now fully cached in %s.\n",
		*targetDb, cachedSymbols, updatedSymbols, totalNewBars, len(symbols), filepath.Base(*targetDb))
}
