package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/datasource"
	"github.com/darianmavgo/backtestgosqlite/internal/storage"
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

func main() {
	targetDb := flag.String("db", "data/leveraged_backtest.db", "Target SQLite DB path")
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
	flag.Parse()

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

	fmt.Printf("Fetched %d symbols to download: %v\n", len(symbols), symbols)

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
	defaultStart := now.AddDate(-*years, 0, 0)
	end := now

	fmt.Printf("Downloading %s bars ending %s for %d symbols using %s...\n\n",
		*timeframe, end.Format("2006-01-02"), len(symbols), primarySource.Name())

	totalBars := 0
	successfulSymbols := 0

	for idx, sym := range symbols {
		// Calculate delta update start date by querying DB for symbol's last date
		symStart := defaultStart
		var lastDateStr string
		err := db.QueryRow("SELECT MAX(substr(Date, 1, 10)) FROM "+*targetTable+" WHERE symbol = ?", sym).Scan(&lastDateStr)
		if err == nil && lastDateStr != "" {
			if parsed, err := time.Parse("2006-01-02", lastDateStr); err == nil {
				// We already have data up to `parsed`. Start downloading from the next day.
				symStart = parsed.AddDate(0, 0, 1)
			}
		}

		if symStart.After(end) {
			fmt.Printf("[%d/%d] %-6s : Data is already up-to-date (Last: %s)\n", idx+1, len(symbols), sym, lastDateStr)
			successfulSymbols++
			continue
		}

		req := datasource.FetchRequest{
			Symbol:    sym,
			StartDate: symStart,
			EndDate:   end,
			Timeframe: *timeframe,
		}

		bars, err := primarySource.Fetch(ctx, req)
		if (err != nil || len(bars) == 0) && fallbackSource != nil {
			// Fallback
			bars, err = fallbackSource.Fetch(ctx, req)
		}

		if err != nil || len(bars) == 0 {
			log.Printf("[%d/%d] Failed %s: %v", idx+1, len(symbols), sym, err)
			continue
		}

		if err := storage.UpsertBars(db, *targetTable, bars); err != nil {
			log.Printf("[%d/%d] Error saving %s to database: %v", idx+1, len(symbols), sym, err)
			continue
		}

		successfulSymbols++
		totalBars += len(bars)
		fmt.Printf("[%d/%d] %-6s : %4d bars saved (Total: %d bars)\n", idx+1, len(symbols), sym, len(bars), totalBars)
		time.Sleep(120 * time.Millisecond) // rate limit
	}

	fmt.Printf("\n✨ Done! Downloaded %d total bars across %d/%d symbols into %s (%s).\n",
		totalBars, successfulSymbols, len(symbols), *targetDb, *targetTable)
}
