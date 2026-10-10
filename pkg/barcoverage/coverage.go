// Package barcoverage judges how complete the stored bar history is for a set
// of symbols. `strategy coverage` reports it and `market_history update` uses
// it to decide which symbols to download again.
package barcoverage

import (
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/olekukonko/tablewriter"

	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

// calendarSymbol is the session calendar for daily completeness: a day SPY
// traded is a session every symbol that was listed should have a bar for.
const calendarSymbol = "SPY"

// CoverageOptions selects what coverage reports on.
type CoverageOptions struct {
	Symbols []string // relevant symbols
	DailyDB string   // market_history.db, the session calendar and the daily bars
	BarDB   string   // database holding the bars to judge: DailyDB, or the hourly database
	Hourly  bool
	Start   string // first session, YYYY-MM-DD
	End     string // last session; "" = latest bar
}

// SymbolCoverage is one symbol's completeness over its own listed window.
type SymbolCoverage struct {
	Symbol      string
	First, Last string  // first and last session with a bar (daily bars, or hourly bars when Hourly)
	Expected    int     // sessions the symbol should have a bar for
	Have        int     // of those, sessions with a bar
	Missing     int     // Expected - Have
	LongestGap  int     // longest run of consecutive missing sessions
	BarsPerDay  float64 // hourly only: average bars on a session that has any
	Bars        int
	Note        string
}

// Pct is the share of expected sessions that have a bar.
func (c SymbolCoverage) Pct() float64 {
	if c.Expected == 0 {
		return 0
	}
	return 100 * float64(c.Have) / float64(c.Expected)
}

// Coverage judges, for each symbol, how many sessions in the window have bars.
// A session is a day SPY has a daily bar (any symbol's own days when SPY is
// absent). A symbol is only expected from its first to its last daily bar, so
// a later listing is not counted as missing history. Daily mode compares the
// symbol's daily bars with that; hourly mode asks which of the symbol's daily
// sessions have at least one hourly bar.
func Coverage(opts CoverageOptions) ([]SymbolCoverage, error) {
	start := opts.Start
	if start == "" {
		start = "2021-01-01"
	}
	daily, err := storage.OpenSQLite(opts.DailyDB)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", opts.DailyDB, err)
	}
	defer daily.Close()

	sessionsOf := func(symbol string) ([]string, error) {
		q := `SELECT DISTINCT substr(Date, 1, 10) FROM backtest_start
			WHERE symbol = ? AND length(Date) = 10 AND substr(Date, 1, 10) >= ?`
		args := []any{symbol, start}
		if opts.End != "" {
			q += ` AND substr(Date, 1, 10) <= ?`
			args = append(args, opts.End)
		}
		var days []string
		err := daily.Select(&days, q+` ORDER BY 1`, args...)
		return days, err
	}

	calendar, err := sessionsOf(calendarSymbol)
	if err != nil {
		return nil, err
	}
	own := map[string][]string{}
	for _, sym := range opts.Symbols {
		if own[sym], err = sessionsOf(sym); err != nil {
			return nil, err
		}
	}
	if len(calendar) == 0 { // no SPY: the union of the symbols' own days
		set := map[string]bool{}
		for _, days := range own {
			for _, d := range days {
				set[d] = true
			}
		}
		for d := range set {
			calendar = append(calendar, d)
		}
		sort.Strings(calendar)
	}

	bars := daily
	if opts.Hourly {
		bars, err = storage.OpenSQLite(opts.BarDB)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", opts.BarDB, err)
		}
		defer bars.Close()
	}

	out := make([]SymbolCoverage, 0, len(opts.Symbols))
	for _, sym := range opts.Symbols {
		c := SymbolCoverage{Symbol: sym}
		days := own[sym]
		if len(days) == 0 {
			c.Note = "no daily bars in window"
			out = append(out, c)
			continue
		}
		listed, last := days[0], days[len(days)-1]

		// Per-session bar counts in the database being judged.
		type dayCount struct {
			Day string `db:"day"`
			N   int    `db:"n"`
		}
		var counts []dayCount
		q := `SELECT substr(Date, 1, 10) AS day, COUNT(*) AS n FROM backtest_start
			WHERE symbol = ? AND substr(Date, 1, 10) >= ? AND substr(Date, 1, 10) <= ?`
		if opts.Hourly {
			q += ` AND length(Date) > 10`
		} else {
			q += ` AND length(Date) = 10`
		}
		if err := bars.Select(&counts, q+` GROUP BY day`, sym, listed, last); err != nil {
			return nil, err
		}
		have := map[string]int{}
		for _, dc := range counts {
			have[dc.Day] = dc.N
			c.Bars += dc.N
		}

		run := 0
		for _, d := range calendar {
			if d < listed || d > last {
				continue
			}
			c.Expected++
			if have[d] > 0 {
				c.Have++
				run = 0
				if c.First == "" {
					c.First = d
				}
				c.Last = d
			} else {
				c.Missing++
				if run++; run > c.LongestGap {
					c.LongestGap = run
				}
			}
		}
		if c.Have > 0 {
			c.BarsPerDay = float64(c.Bars) / float64(c.Have)
		}
		if c.Have == 0 {
			c.Note = "no bars"
		} else if c.First > listed {
			c.Note = "starts " + shortDays(listed, c.First) + " after listing"
		}
		out = append(out, c)
	}
	return out, nil
}

func shortDays(from, to string) string {
	a, e1 := time.Parse("2006-01-02", from)
	b, e2 := time.Parse("2006-01-02", to)
	if e1 != nil || e2 != nil {
		return to
	}
	return fmt.Sprintf("%dd", int(b.Sub(a).Hours()/24))
}

// WriteCoverage prints the report table.
func WriteCoverage(w io.Writer, opts CoverageOptions, rows []SymbolCoverage) {
	kind := "daily"
	if opts.Hourly {
		kind = "hourly"
	}
	fmt.Fprintf(w, "\n%s bar completeness from %s (%d symbols, bars in %s)\n", kind, opts.Start, len(rows), opts.BarDB)
	fmt.Fprintf(w, "Expected = sessions %s traded between the symbol's first and last daily bar.\n", calendarSymbol)
	table := tablewriter.NewWriter(w)
	header := []string{"SYMBOL", "FIRST", "LAST", "EXPECTED", "HAVE", "MISSING", "LONGEST GAP", "COVERAGE"}
	if opts.Hourly {
		header = append(header, "BARS/DAY")
	}
	header = append(header, "NOTE")
	table.SetHeader(header)
	table.SetBorder(true)
	var expected, have int
	for _, c := range rows {
		expected += c.Expected
		have += c.Have
		row := []string{c.Symbol, c.First, c.Last, fmt.Sprint(c.Expected), fmt.Sprint(c.Have), fmt.Sprint(c.Missing),
			fmt.Sprint(c.LongestGap), fmt.Sprintf("%.1f%%", c.Pct())}
		if opts.Hourly {
			row = append(row, fmt.Sprintf("%.1f", c.BarsPerDay))
		}
		table.Append(append(row, c.Note))
	}
	table.Render()
	if expected > 0 {
		fmt.Fprintf(w, "All symbols: %d of %d sessions covered (%.1f%%)\n", have, expected, 100*float64(have)/float64(expected))
	}
}
