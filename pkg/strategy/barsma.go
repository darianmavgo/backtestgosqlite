package strategy

import (
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/jmoiron/sqlx"
)

var barSMASymbol = regexp.MustCompile(`^[A-Za-z0-9.^=_-]+$`)

// LoadBarSMA fills Bar.SMA50 and Bar.SMA200 for every bar in barsBySymbol.
// The averages are calculated by SQL (sql/stages/bar_sma) into the bar_sma
// slice table of the calc database, from the full history in the market
// database, so a windowed bar set still gets the true averages. Go only copies
// the slice table's values onto the bars.
func LoadBarSMA(marketDBPath, calcDBPath string, barsBySymbol map[string][]models.Bar) error {
	if marketDBPath == "" || calcDBPath == "" {
		return fmt.Errorf("bar_sma needs both a market and a calc database path")
	}
	symbols := make([]string, 0, len(barsBySymbol))
	for sym := range barsBySymbol {
		symbols = append(symbols, sym)
	}
	if len(symbols) == 0 {
		return nil
	}

	db, err := OpenCalcDB(marketDBPath, calcDBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := BuildBarSMA(db, symbols); err != nil {
		return err
	}

	type row struct {
		Symbol string  `db:"symbol"`
		Date   string  `db:"date"`
		SMA50  float64 `db:"sma50"`
		SMA200 float64 `db:"sma200"`
	}
	var rows []row
	if err := db.Select(&rows, "SELECT symbol, date, sma50, sma200 FROM bar_sma"); err != nil {
		return err
	}
	at := make(map[string]map[string]int, len(barsBySymbol))
	for sym, bars := range barsBySymbol {
		m := make(map[string]int, len(bars))
		for i := range bars {
			m[bars[i].Date] = i
		}
		at[sym] = m
	}
	for _, r := range rows {
		if i, ok := at[r.Symbol][r.Date]; ok {
			barsBySymbol[r.Symbol][i].SMA50 = r.SMA50
			barsBySymbol[r.Symbol][i].SMA200 = r.SMA200
		}
	}
	return nil
}

// loadBarSMAOrWarn is LoadBarSMA for strategies whose databases may be unset
// (tests pass bars with SMA already filled); it never aborts signal generation.
func loadBarSMAOrWarn(id, marketDBPath, calcDBPath string, barsBySymbol map[string][]models.Bar) {
	if marketDBPath == "" || calcDBPath == "" {
		return
	}
	if err := LoadBarSMA(marketDBPath, calcDBPath, barsBySymbol); err != nil {
		log.Printf("[%s] bar_sma: %v (SMA200 filter unavailable)", id, err)
	}
}

// OpenCalcDB opens the calc database with the market database attached and
// exposed as the backtest_start view, the setup every SQL stage expects.
func OpenCalcDB(marketDBPath, calcDBPath string) (*sqlx.DB, error) {
	return openAttachedCalcDB(marketDBPath, calcDBPath)
}

// SQLSymbolList quotes symbols for an IN (...) list in a stage. Names with
// characters outside letters, digits and . ^ = _ - are skipped. It returns ""
// when nothing is left.
func SQLSymbolList(symbols []string) string {
	var quoted []string
	for _, sym := range symbols {
		if barSMASymbol.MatchString(sym) {
			quoted = append(quoted, "'"+sym+"'")
		}
	}
	sort.Strings(quoted)
	return strings.Join(quoted, ",")
}

// BuildBarSMA runs the sql/stages/bar_sma stage in db, filling the bar_sma
// slice table for symbols.
func BuildBarSMA(db *sqlx.DB, symbols []string) error {
	list := SQLSymbolList(symbols)
	if list == "" {
		return nil
	}
	return storage.RunStage(db, "bar_sma", map[string]string{"__SYMBOL_LIST__": list})
}
