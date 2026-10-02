package strategy

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/realbars"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/jmoiron/sqlx"
)

// Real bars for a handful of leveraged ETFs and their inverses. The expected
// winner each year is worked out here from the real first open and last close,
// independently of the SQL pipeline.
var annualSymbols = []string{"TQQQ", "SQQQ", "SOXL", "SOXS", "SPY", "SH", "QQQ", "PSQ", "NVDL", "TSLL", "GOOGL"}

// annualPairs lists each pair both ways, as the pipeline does.
var annualPairs = map[string]string{
	"TQQQ": "SQQQ", "SQQQ": "TQQQ", "SOXL": "SOXS", "SOXS": "SOXL",
	"SPY": "SH", "SH": "SPY", "QQQ": "PSQ", "PSQ": "QQQ",
}

type yearPick struct{ symbol, entryDate, exitDate string }

func expectedPicks(t *testing.T, market string) map[string]yearPick {
	t.Helper()
	db, err := storage.OpenSQLiteReadOnly(market)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var bars []struct {
		Symbol string  `db:"symbol"`
		Date   string  `db:"date"`
		Open   float64 `db:"open"`
		Close  float64 `db:"close"`
	}
	if err := db.Select(&bars, `SELECT symbol, substr(Date,1,10) AS date, open, close FROM backtest_start
		WHERE length(Date) = 10 AND substr(Date,1,10) >= '2021-01-01' ORDER BY symbol, Date`); err != nil {
		t.Fatal(err)
	}
	type span struct {
		firstDate, lastDate  string
		firstOpen, lastClose float64
	}
	years := map[string]map[string]*span{} // year -> symbol -> span
	for _, b := range bars {
		y := b.Date[:4]
		if years[y] == nil {
			years[y] = map[string]*span{}
		}
		sp := years[y][b.Symbol]
		if sp == nil {
			years[y][b.Symbol] = &span{b.Date, b.Date, b.Open, b.Close}
			continue
		}
		sp.lastDate, sp.lastClose = b.Date, b.Close
	}
	out := map[string]yearPick{}
	for y := 2022; y <= 2025; y++ {
		prev, cur := years[fmt.Sprint(y-1)], years[fmt.Sprint(y)]
		best, bestRet := "", -1e18
		syms := make([]string, 0, len(prev))
		for s := range prev {
			syms = append(syms, s)
		}
		sort.Strings(syms)
		for _, s := range syms {
			if _, ok := cur[s]; !ok || prev[s].firstOpen == 0 {
				continue
			}
			if r := (prev[s].lastClose - prev[s].firstOpen) / prev[s].firstOpen; r > bestRet {
				best, bestRet = s, r
			}
		}
		if best != "" {
			out[fmt.Sprint(y)] = yearPick{best, cur[best].firstDate, cur[best].lastDate}
		}
	}
	return out
}

func TestAnnualWinnerLongShortAndInverseOnRealBars(t *testing.T) {
	market := realbars.Copy(t, annualSymbols...)
	picks := expectedPicks(t, market)
	if len(picks) < 3 {
		t.Fatalf("only %d years had a winner: %v", len(picks), picks)
	}
	db, err := storage.OpenSQLite(market)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bars, _, err := storage.FetchBars(db, "backtest_start", annualSymbols, "2021-01-01", "")
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		strat Strategy
		side  string
	}{
		{&BiggestWinnerStrategy{}, "long"}, {&BiggestWinnerShortStrategy{}, "short"}, {&BiggestWinnerInverseStrategy{}, "inverse"},
	} {
		c.strat.SetDatabases(market, filepath.Join(t.TempDir(), "calc.db"))
		got := map[string][]string{} // year -> "date|symbol|entry|direction"
		for _, s := range c.strat.GenerateSignals(bars) {
			got[s.Date[:4]] = append(got[s.Date[:4]], fmt.Sprintf("%s|%s|%d|%s", s.Date, s.Symbol, s.Entry, s.Direction))
		}
		dir := "LONG"
		if c.side == "short" {
			dir = "SHORT"
		}
		for y, p := range picks {
			var want []string
			trade := p.symbol
			if c.side == "inverse" {
				inv, ok := annualPairs[p.symbol]
				if !ok {
					if len(got[y]) != 0 {
						t.Errorf("%s %s: %s has no pair, want a cash year, got %v", c.side, y, p.symbol, got[y])
					}
					continue
				}
				trade = inv
			}
			want = append(want, fmt.Sprintf("%s|%s|1|%s", p.entryDate, trade, dir))
			if p.exitDate != p.entryDate {
				want = append(want, fmt.Sprintf("%s|%s|-1|%s", p.exitDate, trade, dir))
			}
			if c.side == "inverse" {
				// the pair must also have traded on the entry date, or the year is skipped
				if len(got[y]) == 0 {
					continue
				}
			}
			if strings.Join(got[y], ",") != strings.Join(want, ",") {
				t.Errorf("%s %s: got %v want %v", c.side, y, got[y], want)
			}
		}
	}
}

// The matched-leverage pairs are data in the pipeline: every pair is listed both
// ways and single-name leveraged ETFs are not paired.
func TestInversePairsRoundTrip(t *testing.T) {
	text, err := readPipelineFile("sql/strategies/annual_winner", "02_inverse_pairs.sql")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sqlx.Open("sqlite", filepath.Join(t.TempDir(), "pairs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE aw_inverse (symbol TEXT PRIMARY KEY, inverse TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(text)); err != nil {
		t.Fatal(err)
	}
	var bad []string
	if err := db.Select(&bad, `SELECT a.symbol || '->' || a.inverse FROM aw_inverse a
		LEFT JOIN aw_inverse b ON b.symbol = a.inverse AND b.inverse = a.symbol WHERE b.symbol IS NULL`); err != nil || len(bad) != 0 {
		t.Fatalf("pairs that do not round trip: %v (err %v)", bad, err)
	}
	var n int
	if err := db.Get(&n, "SELECT COUNT(*) FROM aw_inverse WHERE symbol IN ('TSLL', 'NVDL')"); err != nil || n != 0 {
		t.Fatalf("single-name bull ETFs must not have a guessed inverse (%d, err %v)", n, err)
	}
}
