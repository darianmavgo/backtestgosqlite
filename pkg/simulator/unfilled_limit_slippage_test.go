package simulator

import (
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// strategyFlag limits the check to specific registry ids. Empty runs every
// registered strategy. Example:
//
//	go test ./pkg/simulator -run TestUnfilledBuyLimitSlippage
//	go test ./pkg/simulator -run TestUnfilledBuyLimitSlippage -strategy mara_tree,sig-voo-buy-tecl
var strategyFlag = flag.String("strategy", "", "comma-separated strategy ids to check; empty checks every registered strategy")

// TestUnfilledBuyLimitSlippage confirms slippage on the orders a strategy
// actually generated. Signals come from the latest reports/<id>[_N].db for
// each registered strategy (or -strategy). Prices come from
// data/market_history.db. Nothing is mocked.
//
// Next-day limit orders fill on the following session only when that
// session trades at or below the limit: at the limit, or at the open when
// the open is already through it. The booked entry is that fill times
// (1+SlippagePct). When the session's low stays above the limit, the order
// is not filled, so the slipped price is never charged. A same-day limit
// order is held to the same rule on the signal session.
func TestUnfilledBuyLimitSlippage(t *testing.T) {
	// Report databases live outside the test binary, so a cached PASS would
	// ignore a newer backtest. Setenv marks this test uncacheable.
	t.Setenv("BACKTEST_SLIPPAGE_CHECK", "1")

	root := moduleRoot(t)
	marketPath := filepath.Join(root, "data", "market_history.db")
	reportsDir := filepath.Join(root, "reports")
	if _, err := os.Stat(marketPath); err != nil {
		t.Skipf("market db not found at %s", marketPath)
	}
	if _, err := os.Stat(reportsDir); err != nil {
		t.Skipf("reports dir not found at %s", reportsDir)
	}
	strategy.AutoRegisterSQLStrategies(root, marketPath)

	selected, strict := selectStrategies(t)
	market, err := openReadOnly(marketPath)
	if err != nil {
		t.Fatalf("open market db: %v", err)
	}
	defer market.Close()
	bars := &barCache{db: market}

	var checked int
	for _, strat := range selected {
		strat := strat
		t.Run(strat.ID(), func(t *testing.T) {
			checked += confirmUnfilledLimitSlippage(t, strat, reportsDir, bars, strict)
		})
	}
	if checked == 0 {
		t.Fatal("no buy-limit session in the selected strategies' latest report databases had a limit the market never traded")
	}
	if !t.Failed() {
		t.Logf("confirmed %d unfilled buy-limit sessions", checked)
	}
}

// confirmUnfilledLimitSlippage returns how many sessions it proved did not
// enter because the limit never traded.
func confirmUnfilledLimitSlippage(t *testing.T, strat strategy.Strategy, reportsDir string, bars *barCache, strict bool) int {
	t.Helper()
	dbPath, err := latestReportDB(reportsDir, strat.ID())
	if err != nil {
		if strict {
			t.Errorf("strategy %s: %v", strat.ID(), err)
		} else {
			t.Logf("skip %s: %v", strat.ID(), err)
		}
		return 0
	}

	signals, err := loadReportSignals(dbPath, strat.ID())
	if err != nil {
		t.Errorf("strategy %s: read %s: %v", strat.ID(), dbPath, err)
		return 0
	}
	if len(signals) == 0 {
		if strict {
			t.Errorf("strategy %s: %s has no signals", strat.ID(), dbPath)
		} else {
			t.Logf("skip %s: %s has no signals", strat.ID(), filepath.Base(dbPath))
		}
		return 0
	}

	cfg := strat.DefaultConfig()
	bySym, err := bars.load(signalSymbols(signals))
	if err != nil {
		t.Errorf("strategy %s: load bars: %v", strat.ID(), err)
		return 0
	}

	sessions := classifyLimitSessions(cfg, signals, bySym)
	unfilled := 0
	for _, sess := range sessions {
		if sess.allUnmet() {
			unfilled++
		}
	}
	if unfilled == 0 {
		if strict {
			t.Errorf("strategy %s: %s has no buy-limit session the market never traded", strat.ID(), filepath.Base(dbPath))
		} else {
			t.Logf("skip %s: no unfilled buy-limit session in %s", strat.ID(), filepath.Base(dbPath))
		}
		return 0
	}

	entries := simulateEntries(cfg, signals, bySym)
	fails := 0
	priced := 0
	for key, sess := range sessions {
		got := entries[key]
		if sess.allUnmet() {
			if len(got) == 0 {
				continue
			}
			fails++
			t.Errorf("%s %s %s: limit never traded (low above %s) so no entry should exist, including the slipped buy %s; simulator opened %s",
				strat.ID(), key.symbol, key.date, formatPrices(sess.limits()), formatPrices(sess.slippedUnmet(cfg.SlippagePct)), formatEntries(got))
			if fails >= 8 {
				t.Errorf("%s: stopping after %d unfilled-limit mismatches", strat.ID(), fails)
				break
			}
			continue
		}
		for _, entry := range got {
			priced++
			if sess.matchesSlippage(entry.price, cfg.SlippagePct) {
				continue
			}
			fails++
			t.Errorf("%s %s %s: entry %.6f, want fill×(1+%.6f slippage) = %s",
				strat.ID(), key.symbol, key.date, entry.price, cfg.SlippagePct, formatPrices(sess.slippedFills(cfg.SlippagePct)))
			if fails >= 8 {
				t.Errorf("%s: stopping after %d slippage mismatches", strat.ID(), fails)
				break
			}
		}
	}
	if fails > 0 {
		return unfilled
	}
	t.Logf("%s: %s, %d unfilled sessions stayed flat, %d fills matched slippage %.6f",
		strat.ID(), filepath.Base(dbPath), unfilled, priced, cfg.SlippagePct)
	return unfilled
}

type sessionKey struct {
	symbol string
	date   string
}

type limitAttempt struct {
	met   bool
	fill  float64 // price the order would book before slippage
	limit float64
}

type limitSession struct {
	attempts []limitAttempt
}

func (s limitSession) allUnmet() bool {
	if len(s.attempts) == 0 {
		return false
	}
	for _, a := range s.attempts {
		if a.met {
			return false
		}
	}
	return true
}

func (s limitSession) limits() []float64 {
	out := make([]float64, len(s.attempts))
	for i, a := range s.attempts {
		out[i] = a.limit
	}
	return out
}

func (s limitSession) slippedUnmet(slip float64) []float64 {
	var out []float64
	for _, a := range s.attempts {
		if !a.met {
			out = append(out, a.limit*(1+slip))
		}
	}
	return out
}

func (s limitSession) slippedFills(slip float64) []float64 {
	var out []float64
	seen := map[float64]bool{}
	for _, a := range s.attempts {
		if !a.met {
			continue
		}
		px := a.fill * (1 + slip)
		if seen[px] {
			continue
		}
		seen[px] = true
		out = append(out, px)
	}
	return out
}

func (s limitSession) matchesSlippage(entry, slip float64) bool {
	for _, want := range s.slippedFills(slip) {
		if nearPrice(entry, want) {
			return true
		}
	}
	return false
}

// classifyLimitSessions groups orders by the session that can fill them.
// A limit is met only when that session's low prints at or below the limit.
// Next-day strategies use the following bar and improve the fill to the open
// when the open is already through the limit. Same-day strategies use the
// signal bar and fill at the limit.
func classifyLimitSessions(cfg strategy.StrategyConfig, signals []models.Signal, bySym map[string][]models.Bar) map[sessionKey]*limitSession {
	out := map[sessionKey]*limitSession{}
	add := func(key sessionKey, a limitAttempt) {
		sess := out[key]
		if sess == nil {
			sess = &limitSession{}
			out[key] = sess
		}
		sess.attempts = append(sess.attempts, a)
	}
	for _, sig := range signals {
		limit := sig.BuyLimit
		if limit <= 0 {
			limit = sig.Close
		}
		if limit <= 0 {
			continue
		}
		symBars := bySym[strings.ToUpper(sig.Symbol)]
		orderType := strings.ToLower(sig.OrderType)
		if orderType == "" {
			orderType = "limit"
		}
		if orderType == "market" {
			bar, ok := barOn(symBars, sig.Date)
			if !ok {
				continue
			}
			add(sessionKey{strings.ToUpper(sig.Symbol), bar.Date}, limitAttempt{met: true, fill: limit, limit: limit})
			continue
		}
		if cfg.NextDayLimitEntry {
			next, ok := nextBarAfter(symBars, sig.Date)
			if !ok {
				continue
			}
			met := next.Low <= limit
			fill := limit
			if next.Open > 0 && next.Open < limit {
				fill = next.Open
			}
			add(sessionKey{strings.ToUpper(sig.Symbol), next.Date}, limitAttempt{met: met, fill: fill, limit: limit})
			continue
		}
		bar, ok := barOn(symBars, sig.Date)
		if !ok {
			continue
		}
		add(sessionKey{strings.ToUpper(sig.Symbol), bar.Date}, limitAttempt{met: bar.Low <= limit, fill: limit, limit: limit})
	}
	return out
}

type bookedEntry struct {
	price float64
}

func simulateEntries(cfg strategy.StrategyConfig, signals []models.Signal, bySym map[string][]models.Bar) map[sessionKey][]bookedEntry {
	dates := map[string]bool{}
	for _, bars := range bySym {
		for _, b := range bars {
			dates[b.Date] = true
		}
	}
	sorted := make([]string, 0, len(dates))
	for d := range dates {
		sorted = append(sorted, d)
	}
	sort.Strings(sorted)

	sim := NewPortfolioSimulator(cfg, 100000)
	_, trades, _ := sim.Run(signals, bySym, sorted)
	out := map[sessionKey][]bookedEntry{}
	add := func(sym, date string, price float64) {
		if date == "" || price <= 0 {
			return
		}
		key := sessionKey{strings.ToUpper(sym), date[:min(10, len(date))]}
		out[key] = append(out[key], bookedEntry{price: price})
	}
	for _, tr := range trades {
		add(tr.Symbol, tr.EntryDate, tr.EntryPrice)
	}
	for sym, pos := range sim.Positions {
		if pos == nil {
			continue
		}
		add(sym, pos.EntryDate, pos.EntryPrice)
	}
	return out
}

func signalSymbols(signals []models.Signal) []string {
	seen := map[string]bool{}
	var out []string
	for _, sig := range signals {
		sym := strings.ToUpper(sig.Symbol)
		if sym == "" || seen[sym] {
			continue
		}
		seen[sym] = true
		out = append(out, sym)
	}
	sort.Strings(out)
	return out
}

type barCache struct {
	db    *sqlx.DB
	cache map[string][]models.Bar
}

func (c *barCache) load(symbols []string) (map[string][]models.Bar, error) {
	if c.cache == nil {
		c.cache = map[string][]models.Bar{}
	}
	out := make(map[string][]models.Bar, len(symbols))
	for _, sym := range symbols {
		if bars, ok := c.cache[sym]; ok {
			out[sym] = bars
			continue
		}
		var rows []models.Bar
		err := c.db.Select(&rows, `
			SELECT symbol,
			       substr(Date, 1, 10) AS Date,
			       COALESCE(open, 0) AS open,
			       COALESCE(high, 0) AS high,
			       COALESCE(low, 0) AS low,
			       COALESCE(close, 0) AS close,
			       COALESCE(volume, 0) AS volume
			FROM backtest_start
			WHERE symbol = ? AND length(Date) = 10 AND (timeframe IS NULL OR timeframe = '' OR timeframe = '1d')
			ORDER BY substr(Date, 1, 10)`, sym)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", sym, err)
		}
		for i := range rows {
			rows[i].Symbol = strings.ToUpper(rows[i].Symbol)
			if len(rows[i].Date) >= 10 {
				rows[i].Date = rows[i].Date[:10]
			}
		}
		c.cache[sym] = rows
		out[sym] = rows
	}
	return out, nil
}

func barOn(bars []models.Bar, date string) (models.Bar, bool) {
	if len(date) >= 10 {
		date = date[:10]
	}
	i := sort.Search(len(bars), func(i int) bool { return bars[i].Date >= date })
	if i >= len(bars) || bars[i].Date != date {
		return models.Bar{}, false
	}
	return bars[i], true
}

func loadReportSignals(dbPath, strategyID string) ([]models.Signal, error) {
	db, err := openReadOnly(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var rows []struct {
		Symbol     string  `db:"symbol"`
		Date       string  `db:"date"`
		OrderType  string  `db:"order_type"`
		Direction  string  `db:"direction"`
		EntryPrice float64 `db:"entry_price"`
		TakeProfit float64 `db:"take_profit"`
		StopLoss   float64 `db:"stop_loss"`
	}
	err = db.Select(&rows, `
		SELECT symbol,
		       substr(date, 1, 10) AS date,
		       COALESCE(order_type, '') AS order_type,
		       COALESCE(direction, '') AS direction,
		       COALESCE(entry_price, 0) AS entry_price,
		       COALESCE(take_profit, 0) AS take_profit,
		       COALESCE(stop_loss, 0) AS stop_loss
		FROM signals
		WHERE COALESCE(entry_price, 0) > 0
		ORDER BY date, symbol`)
	if err != nil {
		return nil, err
	}
	out := make([]models.Signal, 0, len(rows))
	for _, row := range rows {
		date := row.Date
		if len(date) >= 10 {
			date = date[:10]
		}
		out = append(out, models.Signal{
			Symbol:     strings.ToUpper(row.Symbol),
			Date:       date,
			Close:      row.EntryPrice,
			BuyLimit:   row.EntryPrice,
			OrderType:  row.OrderType,
			Direction:  row.Direction,
			TakeProfit: row.TakeProfit,
			StopLoss:   row.StopLoss,
			StrategyID: strategyID,
			Entry:      1,
		})
	}
	return out, nil
}

// latestReportDB is the highest run increment of reports/<id>.db that still
// has signals. <id>.db is increment 1; <id>_N.db is increment N.
func latestReportDB(reportsDir, id string) (string, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	entries, err := os.ReadDir(reportsDir)
	if err != nil {
		return "", err
	}
	type candidate struct {
		path string
		inc  int
	}
	var cands []candidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), ".db")
		inc, ok := reportIncrement(stem, id)
		if !ok {
			continue
		}
		cands = append(cands, candidate{path: filepath.Join(reportsDir, entry.Name()), inc: inc})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].inc > cands[j].inc })
	var lastErr error
	for _, c := range cands {
		n, err := countSignals(c.path)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", filepath.Base(c.path), err)
			continue
		}
		if n == 0 {
			lastErr = fmt.Errorf("%s has no signals", filepath.Base(c.path))
			continue
		}
		return c.path, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("no reports/%s.db or reports/%s_N.db", id, id)
}

func reportIncrement(stem, id string) (int, bool) {
	if stem == id {
		return 1, true
	}
	prefix := id + "_"
	if !strings.HasPrefix(stem, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(stem, prefix))
	if err != nil || n < 2 {
		return 0, false
	}
	return n, true
}

func countSignals(dbPath string) (int, error) {
	db, err := openReadOnly(dbPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var name string
	if err := db.Get(&name, `SELECT name FROM sqlite_master WHERE type='table' AND name='signals'`); err != nil {
		return 0, fmt.Errorf("no signals table")
	}
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM signals`); err != nil {
		return 0, err
	}
	return n, nil
}

func openReadOnly(path string) (*sqlx.DB, error) {
	return sqlx.Open("sqlite", "file:"+path+"?mode=ro")
}

func selectStrategies(t *testing.T) ([]strategy.Strategy, bool) {
	t.Helper()
	raw := strings.TrimSpace(*strategyFlag)
	if raw == "" {
		all := strategy.List()
		if len(all) == 0 {
			t.Fatal("no strategies registered")
		}
		return all, false
	}
	var out []strategy.Strategy
	seen := map[string]bool{}
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		strat, ok := strategy.Get(tok)
		if !ok {
			t.Errorf("strategy %q is not registered", tok)
			continue
		}
		if seen[strat.ID()] {
			continue
		}
		seen[strat.ID()] = true
		out = append(out, strat)
	}
	if len(out) == 0 {
		t.Fatal("no registered strategies matched -strategy")
	}
	return out, true
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test file")
		}
		dir = parent
	}
}

func nearPrice(got, want float64) bool {
	diff := math.Abs(got - want)
	scale := math.Max(1, math.Max(math.Abs(got), math.Abs(want)))
	return diff <= 1e-8*scale
}

func formatPrices(prices []float64) string {
	parts := make([]string, len(prices))
	for i, p := range prices {
		parts[i] = strconv.FormatFloat(p, 'f', 4, 64)
	}
	return strings.Join(parts, ", ")
}

func formatEntries(entries []bookedEntry) string {
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = strconv.FormatFloat(e.price, 'f', 4, 64)
	}
	return strings.Join(parts, ", ")
}
