// Package dailybars keeps long-history daily bars for the few symbols a model needs more years
// of than the hourly market database holds (the HMM on QQQ needs 2010 onward; hourly bars go
// back about ten years and cost a decade of downloads). Bars come from the Yahoo source and
// land in their own small database, daily_bars.db; the hourly database is never written.
//
// The closes are dividend- and split-adjusted by the source, and an adjusted history is
// rescaled whenever a dividend is paid, so Run always replaces the whole history from start.
package dailybars

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/calendar"
	"github.com/darianmavgo/backtestgosqlite/pkg/datasource"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/jmoiron/sqlx"
)

// Source is recorded on every row so a mixed history is never mistaken for one provider.
const Source = "yahoo"

// DefaultStart is where the history begins: after the 2008-09 crash and the QQQ ticker change,
// with 16 years of sessions.
const DefaultStart = "2010-01-01"

const schema = `CREATE TABLE IF NOT EXISTS daily_bar (
	symbol    TEXT NOT NULL,
	date      TEXT NOT NULL,
	close     REAL NOT NULL,
	adj_close REAL NOT NULL,
	volume    INTEGER NOT NULL,
	source    TEXT NOT NULL,
	PRIMARY KEY (symbol, date)
) WITHOUT ROWID`

// Config holds every setting of a run.
type Config struct {
	DB      string   // daily bars database (default appenv.DailyBarsDB())
	Symbols []string // default QQQ
	Start   string   // YYYY-MM-DD (default DefaultStart)
	DryRun  bool     // fetch and report, write nothing
	Now     time.Time
}

// Result reports one symbol.
type Result struct {
	Symbol string
	Bars   int
	First  string
	Last   string
}

// Run downloads each symbol's daily bars from Start through the last completed session and
// replaces that symbol's stored history with them. A symbol that fails leaves its stored rows
// untouched and the error is returned after the others have been tried.
func Run(ctx context.Context, cfg Config, out io.Writer) ([]Result, error) {
	if cfg.DB == "" {
		cfg.DB = appenv.DailyBarsDB()
	}
	if len(cfg.Symbols) == 0 {
		cfg.Symbols = []string{"QQQ"}
	}
	if cfg.Start == "" {
		cfg.Start = DefaultStart
	}
	if cfg.Now.IsZero() {
		cfg.Now = time.Now()
	}
	start, err := time.Parse("2006-01-02", cfg.Start)
	if err != nil {
		return nil, fmt.Errorf("daily_bars: bad start %q: %w", cfg.Start, err)
	}
	last := calendar.LastCompletedSession(cfg.Now).Format("2006-01-02")

	var db *sqlx.DB
	if !cfg.DryRun {
		if db, err = Open(cfg.DB); err != nil {
			return nil, err
		}
		defer db.Close()
	}

	src := datasource.NewYahooDataSource(nil)
	var results []Result
	var errs []error
	for _, sym := range cfg.Symbols {
		sym = strings.ToUpper(strings.TrimSpace(sym))
		if sym == "" {
			continue
		}
		bars, err := src.Fetch(ctx, datasource.FetchRequest{Symbol: sym, StartDate: start, EndDate: cfg.Now.Add(24 * time.Hour), Timeframe: "1d", AssetClass: "equity"})
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", sym, err))
			continue
		}
		bars = completed(bars, last)
		if len(bars) == 0 {
			errs = append(errs, fmt.Errorf("%s: no completed daily bars from %s", sym, cfg.Start))
			continue
		}
		res := Result{Symbol: sym, Bars: len(bars), First: bars[0].Date, Last: bars[len(bars)-1].Date}
		if !cfg.DryRun {
			if err := Replace(db, sym, bars); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", sym, err))
				continue
			}
		}
		verb := "stored"
		if cfg.DryRun {
			verb = "would store"
		}
		fmt.Fprintf(out, "[daily_bars] %s: %s %d bars %s..%s\n", sym, verb, res.Bars, res.First, res.Last)
		results = append(results, res)
	}
	return results, errors.Join(errs...)
}

// completed keeps the bars on or before the last completed session, in date order, one per day.
// A bar for today is still forming and would change by tomorrow.
func completed(bars []models.Bar, last string) []models.Bar {
	seen := map[string]bool{}
	var out []models.Bar
	for _, b := range bars {
		d := b.Date
		if len(d) > 10 {
			d = d[:10]
		}
		if d > last || seen[d] || b.Close <= 0 || b.AdjClose <= 0 {
			continue
		}
		seen[d] = true
		b.Date = d
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

// Open opens (creating if needed) the daily bars database.
func Open(path string) (*sqlx.DB, error) {
	db, err := storage.OpenSQLite(path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("daily_bars: create schema: %w", err)
	}
	return db, nil
}

// Replace swaps one symbol's stored history for bars, in one transaction.
func Replace(db *sqlx.DB, symbol string, bars []models.Bar) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM daily_bar WHERE symbol = ?`, symbol); err != nil {
		return err
	}
	stmt, err := tx.Preparex(`INSERT INTO daily_bar (symbol, date, close, adj_close, volume, source) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, b := range bars {
		if _, err := stmt.Exec(symbol, b.Date, b.Close, b.AdjClose, b.Volume, Source); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Main is the daily_bars command.
func Main() { os.Exit(RunCLI(os.Args[1:], os.Stdout, os.Stderr)) }

// RunCLI is Main without os.Args/os.Exit. It returns the exit code.
func RunCLI(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("daily_bars", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, `daily_bars: download long-history daily bars (default QQQ from 2010) into daily_bars.db.

Replaces each named symbol's stored history with a fresh download, through the last completed
session. Writes only daily_bars.db, never the hourly market database. -dry-run downloads and
reports but writes nothing.`)
		fs.PrintDefaults()
	}
	db := fs.String("db", appenv.DailyBarsDB(), "daily bars database")
	symbols := fs.String("symbols", "QQQ", "comma-separated symbols")
	start := fs.String("start", DefaultStart, "first date, YYYY-MM-DD")
	dry := fs.Bool("dry-run", false, "download and report, write nothing")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	syms := append(strings.Split(*symbols, ","), fs.Args()...)
	if _, err := Run(context.Background(), Config{DB: *db, Symbols: syms, Start: *start, DryRun: *dry}, stdout); err != nil {
		fmt.Fprintln(stderr, "daily_bars:", err)
		return 1
	}
	return 0
}
