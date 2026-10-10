package runner

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
)

// MarketSource is one market_history database a run reads.
type MarketSource struct {
	Label  string // "Daily bars" or "Hourly bars"
	Path   string // absolute where it can be resolved
	Detail string // size and last update, or why it is not there
	Use    string // what the run uses it for
}

// MarketSources names the databases a backtest reads bars from: the daily
// market database and the hourly one beside it (appenv.BarDB), which limit
// entries use to decide fills.
func MarketSources(marketDB string) []MarketSource {
	return MarketSourcesFor(marketDB, true)
}

// MarketSourcesFor is MarketSources for a command that reads the hourly
// database only when hourly is true; otherwise that source is still named, with
// its use stated as not read, so the output says what the numbers do not rest on.
func MarketSourcesFor(marketDB string, hourly bool) []MarketSource {
	one := func(label, path, use string) MarketSource {
		s := MarketSource{Label: label, Path: path, Use: use}
		if abs, err := filepath.Abs(path); err == nil {
			s.Path = abs
		}
		if fi, err := os.Stat(path); err != nil {
			s.Detail = "not found"
		} else {
			s.Detail = fmt.Sprintf("%.0f MB, updated %s", float64(fi.Size())/1e6, fi.ModTime().Format("2006-01-02 15:04"))
		}
		return s
	}
	hourlyUse := "limit-entry fills and exits"
	if DailyFills {
		hourlyUse = "not read: -daily-fills judges limit entries on daily bars"
	} else if !hourly {
		hourlyUse = "not read here: limit entries are simulated on daily bars"
	}
	return []MarketSource{
		one("Daily bars", marketDB, "signals, simulation"),
		one("Hourly bars", appenv.BarDB(marketDB, "1h"), hourlyUse),
	}
}

// PrintMarketSources writes the databases a run reads, one line each.
func PrintMarketSources(w io.Writer, marketDB, table string) {
	PrintMarketSourcesFor(w, marketDB, table, true)
}

// PrintMarketSourcesFor is PrintMarketSources for a command that may not read the hourly database.
func PrintMarketSourcesFor(w io.Writer, marketDB, table string, hourly bool) {
	fmt.Fprintf(w, "Market data (table %s):\n", table)
	for _, s := range MarketSourcesFor(marketDB, hourly) {
		fmt.Fprintf(w, "  %-11s %s (%s; %s)\n", s.Label, s.Path, s.Detail, s.Use)
	}
}

// DailyFills turns the hourly bars off: limit entries are then judged on the
// daily bar alone, which books a same-day exit whenever the day's range touched
// both the limit and the target, in either order. It is for comparing with the
// default only (-daily-fills on backtest and gridsearch).
var DailyFills bool

// LoadFillBars loads the hourly bars that decide limit fills for symbols, from
// the hourly database beside marketDB. It returns nil when DailyFills is set.
// Symbols with no hourly bars at all are named in the warning: their fills
// fall back to the daily bar.
func LoadFillBars(marketDB string, symbols []string) (simulator.Intraday, error) {
	if DailyFills {
		return nil, nil
	}
	in, err := LoadIntraday(marketDB, symbols)
	if err != nil {
		return nil, err
	}
	var none []string
	for _, sym := range symbols {
		if len(in[sym]) == 0 {
			none = append(none, sym)
		}
	}
	if len(none) > 0 {
		sort.Strings(none)
		log.Printf("Warning: no hourly bars in %s for %s; their limit fills use daily bars (market_history update -hourly)", appenv.BarDB(marketDB, "1h"), strings.Join(none, ","))
	}
	return in, nil
}
