package hoursim_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/hoursim"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/realbars"
	"github.com/darianmavgo/backtestgosqlite/pkg/runner"
	"github.com/darianmavgo/backtestgosqlite/pkg/sessions"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/stratreg"
)

// TestParityWithDailySimulator plays the same real signals through the daily simulator and the
// hourly one and prints how they differ. The two are expected to disagree (that is the point of
// the hourly engine), so the test checks the hourly run's own guarantees and reports the gap.
//
// HOURSIM_PARITY_DB is a market database holding hourly rows (the backtestgo one by default) and
// HOURSIM_PARITY_STRATEGIES a comma list of strategy ids; the test is skipped without the data.
func TestParityWithDailySimulator(t *testing.T) {
	src := os.Getenv("HOURSIM_PARITY_DB")
	if src == "" {
		src = filepath.Join("..", "..", "..", "backtestgo", "data", "market_history_hourly.db")
	}
	if fi, err := os.Stat(src); err != nil || fi.Size() == 0 {
		t.Skipf("hourly market data %s not present", src)
	}
	ids := strings.Split(envOr("HOURSIM_PARITY_STRATEGIES", "streak-voo-buy-tecl,streak-shpu-down3"), ",")

	t.Setenv("APP_FOLDER", t.TempDir())
	stratreg.RegisterFamiliesAt(realbars.StrategiesDB(t))

	var strats []strategy.Strategy
	symSet := map[string]bool{}
	for _, id := range ids {
		s, ok := strategy.Get(strings.TrimSpace(id))
		if !ok {
			t.Skipf("strategy %q is not in the strategies database", id)
		}
		strats = append(strats, s)
		for _, sym := range s.(strategy.RequiredSymbolsProvider).RequiredSymbols() {
			symSet[sym] = true
		}
	}
	var symbols []string
	for s := range symSet {
		symbols = append(symbols, s)
	}
	sort.Strings(symbols)

	market := filepath.Join(t.TempDir(), "market_history_hourly.db") // hourly bars and their sessions share this file
	db, err := storage.OpenSQLite(market)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.EnsureBarTable(db, "backtest_start"); err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(fmt.Sprintf("ATTACH DATABASE '%s' AS src", strings.ReplaceAll(src, "'", "''"))); err != nil {
		t.Fatal(err)
	}
	quoted := "'" + strings.Join(symbols, "','") + "'"
	if _, err := db.Exec(`INSERT INTO backtest_start (Date, timeframe, asset_class, open, high, low, close, "Adj Close", volume, symbol)
		SELECT substr(replace(Date, 'T', ' '), 1, 19), '1h', 'equity', open, high, low, close, "Adj Close", volume, symbol
		FROM src.backtest_start WHERE timeframe = '1h' AND symbol IN (` + quoted + `)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DETACH DATABASE src"); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Derive(db, "backtest_start"); err != nil {
		t.Fatal(err)
	}
	daily, dates, err := storage.FetchBars(db, "backtest_start", symbols, "", "")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	hourly, err := runner.LoadIntraday(market, symbols)
	if err != nil {
		t.Fatal(err)
	}
	if len(dates) < 100 {
		t.Skipf("only %d sessions copied", len(dates))
	}

	t.Logf("%-24s %-9s %6s %6s %8s %8s %8s", "strategy", "engine", "trades", "win%", "return%", "maxDD%", "daily-bar sessions")
	for _, s := range strats {
		s.SetDatabases(market, filepath.Join(t.TempDir(), "calc.db"))
		signals := s.GenerateSignals(daily)
		if len(signals) == 0 {
			t.Logf("%-24s no signals", s.ID())
			continue
		}
		cfg := s.DefaultConfig()

		old := simulator.NewPortfolioSimulator(cfg, 100000)
		if cfg.EntryLimitPct > 0 {
			old.Intraday = hourly
		}
		oldReport, oldTrades, _ := old.Run(signals, daily, dates)

		out, err := hoursim.Run(hoursim.Input{Signals: signals, Daily: daily, Hourly: hourly, Dates: dates, Config: cfg, Capital: 100000})
		if err != nil {
			t.Fatalf("%s: %v", s.ID(), err)
		}
		fallback := 0
		for _, ds := range out.DailySessions {
			fallback += len(ds)
		}
		t.Logf("%-24s %-9s %6d %6.1f %8.1f %8.1f", s.ID(), "daily", len(oldTrades), 100*oldReport.WinRate, 100*oldReport.TotalReturnPct, 100*oldReport.MaxDrawdownPct)
		t.Logf("%-24s %-9s %6d %6.1f %8.1f %8.1f %8d", s.ID(), "hourly", len(out.Trades), 100*out.Report.WinRate, 100*out.Report.TotalReturnPct, 100*out.Report.MaxDrawdownPct, fallback)
		t.Logf("%-24s trades only in daily: %d, only in hourly: %d, same entry day and symbol: %d",
			s.ID(), onlyIn(oldTrades, out.Trades), onlyIn(out.Trades, oldTrades), same(oldTrades, out.Trades))

		if os.Getenv("HOURSIM_PARITY_VERBOSE") != "" {
			logPairs(t, s.ID(), oldTrades, out.Trades)
		}
		for _, tr := range out.Trades {
			if tr.ExitDate < tr.EntryDate {
				t.Errorf("%s: exits before it enters: %+v", s.ID(), tr)
			}
		}
		if len(out.Curve) != len(dates) {
			t.Errorf("%s: curve has %d points for %d sessions", s.ID(), len(out.Curve), len(dates))
		}
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func key(t models.Trade) string { return t.Symbol + "@" + t.EntryDate }

func same(a, b []models.Trade) int {
	have := map[string]bool{}
	for _, t := range b {
		have[key(t)] = true
	}
	n := 0
	for _, t := range a {
		if have[key(t)] {
			n++
		}
	}
	return n
}

func onlyIn(a, b []models.Trade) int { return len(a) - same(a, b) }

// logPairs prints each trade both engines took on the same symbol and entry day, side by side.
func logPairs(t *testing.T, id string, daily, hourly []models.Trade) {
	byKey := map[string]models.Trade{}
	for _, tr := range hourly {
		byKey[key(tr)] = tr
	}
	for _, d := range daily {
		h, ok := byKey[key(d)]
		if !ok {
			t.Logf("%s %s %s daily only: %s at %.2f -> %s at %.2f %s", id, d.Symbol, d.EntryDate, d.EntryDate, d.EntryPrice, d.ExitDate, d.ExitPrice, d.ExitReason)
			continue
		}
		t.Logf("%s %s %s  daily: %d sh %.2f -> %s %.2f %-13s | hourly: %d sh %.2f -> %s %.2f %-13s", id, d.Symbol, d.EntryDate,
			d.Shares, d.EntryPrice, d.ExitDate, d.ExitPrice, d.ExitReason, h.Shares, h.EntryPrice, h.ExitDate, h.ExitPrice, h.ExitReason)
	}
}
