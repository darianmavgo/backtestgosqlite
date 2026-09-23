package download

import (
	"context"
	"flag"
	"fmt"
	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"io"
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

func fetchWithFallback(ctx context.Context, out io.Writer, primary, fallback datasource.DataSource, req datasource.FetchRequest) ([]models.Bar, error) {
	bars, err := primary.Fetch(ctx, req)
	if err == nil && len(bars) > 0 {
		return bars, nil
	}
	if fallback != nil {
		fmt.Fprintf(out, "Primary source (%s) failed for %s: %v. Attempting fallback to %s...\n", primary.Name(), req.Symbol, err, fallback.Name())
		fallbackBars, fallbackErr := fallback.Fetch(ctx, req)
		if fallbackErr == nil && len(fallbackBars) > 0 {
			return fallbackBars, nil
		}
		return nil, fmt.Errorf("%s failed: %w (fallback %s failed: %v)", primary.Name(), err, fallback.Name(), fallbackErr)
	}
	return nil, err
}

// Config holds every setting of a download run. Zero values mean "unset";
// DefaultConfig returns the CLI defaults.
type Config struct {
	DB, SettingsDB, Source, PolygonKey, CSV string
	Symbols, Universe, Table                string
	Limit, Years                            int
	Start, End, Timeframe, TargetTable      string
	Force                                   bool
	Concurrency                             int
	OTM                                     string    // polygon-options: comma-separated % OTM
	Rate, MaxCalls                          int       // polygon-options
	Out                                     io.Writer // progress output; nil discards
}

// Summary reports what a Run did.
type Summary struct {
	Symbols, CachedSymbols, UpdatedSymbols, FailedSymbols, NewBars int
}

// DefaultConfig returns the same defaults the download CLI uses.
func DefaultConfig() Config {
	return Config{
		DB: appenv.MarketDB(), SettingsDB: appenv.RefDB(), Source: "yahoo",
		Table: "leveraged_etf", Limit: 50, Years: 4, Timeframe: "1d",
		TargetTable: "backtest_start",
		Concurrency: runtime.NumCPU(), OTM: "0,2,5", Rate: 5,
	}
}

// Main is the CLI entry point.
func Main() {
	d := DefaultConfig()
	cfg := d
	flag.StringVar(&cfg.DB, "db", d.DB, "Target SQLite DB path for market history (default: APP_FOLDER/data/market_history.db)")
	flag.StringVar(&cfg.SettingsDB, "settings", d.SettingsDB, "Reference DB path (etf_universe lists and symbol tables)")
	flag.StringVar(&cfg.Source, "source", d.Source, "Data source provider: yahoo, polygon, polygon-options, stooq, csv")
	flag.StringVar(&cfg.PolygonKey, "polygon-key", d.PolygonKey, "Polygon.io API key (or set POLYGON_API_KEY in environment or .env)")
	flag.StringVar(&cfg.CSV, "csv", d.CSV, "Path to CSV file or directory of CSV files (used with -source csv)")
	flag.StringVar(&cfg.Symbols, "symbols", d.Symbols, "Comma-separated list of symbols to download/import (e.g. SPY,QQQ,TQQQ)")
	flag.StringVar(&cfg.Universe, "list", d.Universe, "etf_universe list in the settings DB to download (all, 6yr, sweep)")
	flag.StringVar(&cfg.Table, "table", d.Table, "Table name in settings.db with symbols (fallback if no symbols specified)")
	flag.IntVar(&cfg.Limit, "limit", d.Limit, "Limit number of symbols (0 for all)")
	flag.IntVar(&cfg.Years, "years", d.Years, "Number of years of history")
	flag.StringVar(&cfg.Start, "start", d.Start, "Optional start date (YYYY-MM-DD), overrides -years")
	flag.StringVar(&cfg.End, "end", d.End, "Optional end date (YYYY-MM-DD), defaults to now")
	flag.StringVar(&cfg.Timeframe, "timeframe", d.Timeframe, "Bar timeframe (1d, 1h, 5m, 1m)")
	flag.StringVar(&cfg.TargetTable, "target-table", d.TargetTable, "Target table name in target SQLite DB")
	flag.BoolVar(&cfg.Force, "force", d.Force, "Force re-downloading all bars even if already present in database")
	flag.IntVar(&cfg.Concurrency, "concurrency", d.Concurrency, "Number of symbols to fetch concurrently (network-bound; DB writes are serialized internally). Defaults to all CPU cores.")
	flag.StringVar(&cfg.OTM, "otm", d.OTM, "(polygon-options) call strikes to pull, as % OTM vs spot at the roll date (nearest listed strike each)")
	flag.IntVar(&cfg.Rate, "rate", d.Rate, "(polygon-options) API calls per minute; 5 = Polygon free tier, 0 = unthrottled (paid)")
	flag.IntVar(&cfg.MaxCalls, "max-calls", d.MaxCalls, "(polygon-options) stop after this many API calls (0 = no cap); reruns resume, finished contracts are skipped")
	flag.Parse()
	cfg.Out = os.Stdout
	if strings.TrimSpace(cfg.PolygonKey) == "" {
		cfg.PolygonKey = datasource.ResolvePolygonAPIKey() // POLYGON_API_KEY / .env: CLI only, Run never reads the environment
	}
	if _, err := Run(context.Background(), cfg); err != nil {
		log.Fatal(err)
	}
}

// syncWriter serialises writes so concurrent workers can share one io.Writer.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// Run downloads or imports market history per cfg. It uses only cfg (no
// environment variables, .env, or app-folder paths), returns errors rather
// than exiting, writes progress to cfg.Out (nil discards), and stops when ctx
// is cancelled. Empty Table/Timeframe/Source/etc. fall back to the CLI
// defaults; DB must be set, and polygon sources need PolygonKey.
func Run(ctx context.Context, cfg Config) (*Summary, error) {
	var out io.Writer = io.Discard
	if cfg.Out != nil {
		out = &syncWriter{w: cfg.Out}
	}
	if cfg.DB == "" {
		return nil, fmt.Errorf("download: DB is required")
	}
	if cfg.TargetTable == "" {
		cfg.TargetTable = "backtest_start"
	}
	if cfg.Timeframe == "" {
		cfg.Timeframe = "1d"
	}
	if cfg.Source == "" {
		cfg.Source = "yahoo"
	}
	if cfg.Years <= 0 {
		cfg.Years = 4
	}
	if cfg.OTM == "" {
		cfg.OTM = "0,2,5"
	}
	if src := strings.ToLower(cfg.Source); (src == "polygon" || src == "polygon-options") && strings.TrimSpace(cfg.PolygonKey) == "" {
		return nil, fmt.Errorf("download: source %s requires Config.PolygonKey", src)
	}

	db, err := storage.OpenSQLite(cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("Failed to open target DB %s: %v", cfg.DB, err)
	}
	defer db.Close()

	if err := storage.EnsureBarTable(db, cfg.TargetTable); err != nil {
		return nil, fmt.Errorf("Failed to initialize database table schema: %v", err)
	}

	// 0. Option history (Polygon free tier) for one underlying.
	if strings.ToLower(cfg.Source) == "polygon-options" {
		underlying := "VOO"
		if syms := strings.Split(cfg.Symbols, ","); strings.TrimSpace(syms[0]) != "" {
			underlying = strings.ToUpper(strings.TrimSpace(syms[0]))
		}
		otms, err := parseOTMList(cfg.OTM)
		if err != nil {
			return nil, fmt.Errorf("%v", err)
		}
		end := time.Now().UTC()
		start := end.AddDate(-2, 0, 7) // free tier: ~2 years of option history
		if cfg.Start != "" {
			if start, err = time.Parse("2006-01-02", cfg.Start); err != nil {
				return nil, fmt.Errorf("Invalid -start date %q: expected YYYY-MM-DD", cfg.Start)
			}
		}
		if cfg.End != "" {
			if end, err = time.Parse("2006-01-02", cfg.End); err != nil {
				return nil, fmt.Errorf("Invalid -end date %q: expected YYYY-MM-DD", cfg.End)
			}
		}
		if err := downloadOptionHistory(ctx, out, db, cfg.TargetTable, underlying, start, end, otms, cfg.PolygonKey, cfg.Rate, cfg.MaxCalls); err != nil {
			return nil, fmt.Errorf("option download: %v", err)
		}
		return &Summary{}, nil
	}

	// 1. Handle direct CSV ingestion
	if strings.ToLower(cfg.Source) == "csv" || cfg.CSV != "" {
		if cfg.CSV == "" {
			return nil, fmt.Errorf("Please provide CSV file or directory path using -csv <path>")
		}
		fmt.Fprintf(out, "📂 Ingesting historical market data from CSV: %s\n", cfg.CSV)
		csvSource := datasource.NewCSVDataSource(cfg.CSV, nil)
		req := datasource.FetchRequest{
			Symbol:    cfg.Symbols,
			Timeframe: cfg.Timeframe,
		}
		bars, err := csvSource.Fetch(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("Failed to parse CSV %s: %v", cfg.CSV, err)
		}

		if err := storage.UpsertBars(db, cfg.TargetTable, bars); err != nil {
			return nil, fmt.Errorf("Failed to save bars to SQLite DB: %v", err)
		}

		fmt.Fprintf(out, "✅ Successfully ingested %d bars from CSV into %s (%s)\n", len(bars), cfg.DB, cfg.TargetTable)
		return &Summary{NewBars: len(bars)}, nil
	}

	// 2. Resolve symbol list
	var symbols []string
	if cfg.Symbols != "" {
		parts := strings.Split(cfg.Symbols, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(strings.ToUpper(p))
			if trimmed != "" {
				symbols = append(symbols, trimmed)
			}
		}
	} else if cfg.Universe != "" {
		listSymbols, err := readUniverse(cfg.SettingsDB, cfg.Universe)
		if err != nil {
			return nil, fmt.Errorf("Failed to read etf_universe list %q from %s: %v", cfg.Universe, cfg.SettingsDB, err)
		}
		symbols = listSymbols
	} else {
		dbSymbols, err := getSymbolsFromTable(cfg.SettingsDB, cfg.Table, cfg.Limit)
		if err != nil {
			fmt.Fprintf(out, "Warning: failed to query settings.db table %s: %v\n", cfg.Table, err)
		}
		symbols = dbSymbols
	}

	if len(symbols) == 0 {
		return nil, fmt.Errorf("No symbols resolved. Specify -symbols SPY,QQQ or -list <name> or -table <name>")
	}

	fmt.Fprintf(out, "Database: %s (Table: %s)\n", cfg.DB, cfg.TargetTable)
	fmt.Fprintf(out, "Target Universe: %d symbols: %v\n", len(symbols), symbols)

	client := &http.Client{Timeout: 15 * time.Second}
	var primarySource datasource.DataSource
	var fallbackSource datasource.DataSource

	switch strings.ToLower(cfg.Source) {
	case "polygon":
		polySource := datasource.NewPolygonDataSource(cfg.PolygonKey, client)
		if polySource.APIKey == "" {
			return nil, fmt.Errorf("Polygon API key is required. Pass -polygon-key <KEY> or set POLYGON_API_KEY in your environment / .env file")
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
	start := now.AddDate(-cfg.Years, 0, 0)
	end := now

	if cfg.Start != "" {
		if parsed, err := time.Parse("2006-01-02", cfg.Start); err == nil {
			start = parsed.UTC()
		} else {
			return nil, fmt.Errorf("Invalid -start date %q: expected YYYY-MM-DD", cfg.Start)
		}
	}
	if cfg.End != "" {
		if parsed, err := time.Parse("2006-01-02", cfg.End); err == nil {
			end = parsed.UTC()
		} else {
			return nil, fmt.Errorf("Invalid -end date %q: expected YYYY-MM-DD", cfg.End)
		}
	}

	reqStartStr := start.Format("2006-01-02")
	reqEndStr := end.Format("2006-01-02")

	fmt.Fprintf(out, "Target Horizon: %s bars from %s to %s using %s\n",
		cfg.Timeframe, reqStartStr, reqEndStr, primarySource.Name())
	fmt.Fprintf(out, "🔍 Checking %s first for existing cached data...\n\n", filepath.Base(cfg.DB))

	totalNewBars := 0
	cachedSymbols := 0
	updatedSymbols := 0
	failedSymbols := 0

	var mu sync.Mutex // serializes all DB access and shared counters/printing (SQLite allows only one writer at a time)

	processSymbol := func(idx int, sym string) {
		if ctx.Err() != nil {
			return
		}
		mu.Lock()
		cov, err := storage.GetSymbolDateCoverageWithTimeframe(db, cfg.TargetTable, sym, cfg.Timeframe)
		mu.Unlock()
		if err != nil {
			fmt.Fprintf(out, "[%d/%d] Error checking database coverage for %s: %v\n", idx+1, len(symbols), sym, err)
		}

		// Determine missing date windows
		var missingWindows []datasource.FetchRequest

		if cov.BarCount == 0 || cfg.Force {
			// Symbol is completely missing from DB (or forced full refresh)
			missingWindows = append(missingWindows, datasource.FetchRequest{
				Symbol:    sym,
				StartDate: start,
				EndDate:   end,
				Timeframe: cfg.Timeframe,
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
						Timeframe: cfg.Timeframe,
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
						Timeframe: cfg.Timeframe,
					})
				}
			}
		}

		// If DB already has all requested data, skip remote fetch!
		if len(missingWindows) == 0 {
			mu.Lock()
			cachedSymbols++
			fmt.Fprintf(out, "[%d/%d] %-6s : ⚡ Up-to-date in %s (%d bars, %s ➔ %s). 0 missing, skipped remote fetch.\n",
				idx+1, len(symbols), sym, filepath.Base(cfg.DB), cov.BarCount, cov.MinDate, cov.MaxDate)
			mu.Unlock()
			return
		}

		// Fetch only the missing window(s) — network I/O happens outside the lock so
		// multiple symbols can be in flight concurrently.
		symNewBars := 0
		fetchError := false

		for _, winReq := range missingWindows {
			bars, err := fetchWithFallback(ctx, out, primarySource, fallbackSource, winReq)
			if err != nil {
				fmt.Fprintf(out, "[%d/%d] Failed fetching missing data for %s (%s to %s): %v\n",
					idx+1, len(symbols), sym, winReq.StartDate.Format("2006-01-02"), winReq.EndDate.Format("2006-01-02"), err)
				fetchError = true
				continue
			}
			if len(bars) == 0 {
				continue
			}

			mu.Lock()
			err = storage.UpsertBars(db, cfg.TargetTable, bars)
			mu.Unlock()
			if err != nil {
				fmt.Fprintf(out, "[%d/%d] Error saving %s bars to DB: %v\n", idx+1, len(symbols), sym, err)
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
			fmt.Fprintf(out, "[%d/%d] %-6s : 📥 Not in DB. Pulled %d bars (%s ➔ %s) from %s ➔ %s\n",
				idx+1, len(symbols), sym, symNewBars, reqStartStr, reqEndStr, primarySource.Name(), filepath.Base(cfg.DB))
		} else if symNewBars > 0 {
			fmt.Fprintf(out, "[%d/%d] %-6s : 🔄 Found %d bars in DB (%s ➔ %s). Pulled %d missing bars from %s ➔ %s\n",
				idx+1, len(symbols), sym, cov.BarCount, cov.MinDate, cov.MaxDate, symNewBars, primarySource.Name(), filepath.Base(cfg.DB))
		} else {
			fmt.Fprintf(out, "[%d/%d] %-6s : ⚡ Found %d bars in DB (%s ➔ %s). Checked %s (no new closed bars available).\n",
				idx+1, len(symbols), sym, cov.BarCount, cov.MinDate, cov.MaxDate, primarySource.Name())
		}
	}

	workers := cfg.Concurrency
	if workers < 1 {
		workers = 1
	}
	if workers == 1 {
		for idx, sym := range symbols {
			processSymbol(idx, sym)
			select { // gentle rate limit for serial mode
			case <-ctx.Done():
			case <-time.After(120 * time.Millisecond):
			}
		}
	} else {
		fmt.Fprintf(out, "🚀 Downloading with %d concurrent workers...\n\n", workers)
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
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("download cancelled: %w", err)
	}

	fmt.Fprintf(out, "\n✨ Summary: %s is updated! (%d symbols already up-to-date, %d symbols fetched/updated, %d failed, %d new bars added).\n   %d requested symbols processed against %s.\n",
		cfg.DB, cachedSymbols, updatedSymbols, failedSymbols, totalNewBars, len(symbols), filepath.Base(cfg.DB))
	return &Summary{Symbols: len(symbols), CachedSymbols: cachedSymbols, UpdatedSymbols: updatedSymbols, FailedSymbols: failedSymbols, NewBars: totalNewBars}, nil
}
