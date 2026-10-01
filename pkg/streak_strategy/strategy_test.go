package streak_strategy

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

func TestValidateRowRejectsBadConfig(t *testing.T) {
	ok := sampleRow()
	if err := ValidateRow(ok); err != nil {
		t.Fatal(err)
	}
	cases := []refdb.StreakStrategy{
		func() refdb.StreakStrategy { r := ok; r.SignalSymbol = "TQQQ'; DROP"; return r }(),
		func() refdb.StreakStrategy { r := ok; r.TradeSymbol = ""; return r }(),
		func() refdb.StreakStrategy { r := ok; r.Direction = "sideways"; return r }(),
		func() refdb.StreakStrategy { r := ok; r.Regime = "QQQ>=SMA50"; return r }(),
		func() refdb.StreakStrategy { r := ok; r.ID = "streak_strategy"; return r }(),
	}
	for _, row := range cases {
		if err := ValidateRow(row); err == nil {
			t.Errorf("expected error for %+v", row)
		}
	}
}

func TestRegisterFrom(t *testing.T) {
	before := registryIDs()
	RegisterFrom(filepath.Join(t.TempDir(), "missing.db"))
	if n := len(registryIDs()); n != len(before) {
		t.Fatalf("missing DB registered %d new strategies", n-len(before))
	}

	empty := filepath.Join(t.TempDir(), "empty.db")
	db, err := refdb.Open(empty)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	RegisterFrom(empty)
	if n := len(registryIDs()); n != len(before) {
		t.Fatalf("empty table registered %d new strategies", n-len(before))
	}

	row := sampleRow()
	db, err = refdb.Open(empty)
	if err != nil {
		t.Fatal(err)
	}
	if err := refdb.UpsertStreakStrategies(db, []refdb.StreakStrategy{row}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	RegisterFrom(empty)

	got, ok := strategy.Get(row.ID)
	if !ok {
		t.Fatalf("%s was not registered", row.ID)
	}
	ss, ok := got.(*Strategy)
	if !ok {
		t.Fatalf("got %T", got)
	}
	if syms := ss.RequiredSymbols(); len(syms) != 2 || syms[0] != "VOO" || syms[1] != "TQQQ" {
		t.Fatalf("RequiredSymbols = %v", syms)
	}
	cfg := ss.DefaultConfig()
	if math.Abs(cfg.StopLossPct-0.92) > 1e-9 || math.Abs(cfg.TakeProfitPct-0.03) > 1e-9 || cfg.TargetPct != 1.03 {
		t.Fatalf("config TP/SL = tp %v target %v stop %v", cfg.TakeProfitPct, cfg.TargetPct, cfg.StopLossPct)
	}
	if cfg.Benchmark != "VOO" || cfg.TradeSymbol != "TQQQ" || cfg.DeclineDays != 2 || cfg.HoldingWindow != 15 {
		t.Fatalf("config symbols/days = %+v", cfg)
	}
}

func TestSQLSignalsMatchStreakSignals(t *testing.T) {
	market := filepath.Join(t.TempDir(), "market.db")
	calc := filepath.Join(t.TempDir(), "calc.db")
	voo, tqqq := upSeries()
	gld := downSeries()
	if err := writeMarket(market, voo, tqqq, gld); err != nil {
		t.Fatal(err)
	}

	rally := sampleRow()
	compareSQL(t, market, calc, rally, voo, tqqq)

	dropAll := refdb.StreakStrategy{
		ID: "streak-gld-down2-gld", Name: "GLD down2 → GLD",
		SignalSymbol: "GLD", TradeSymbol: "GLD", Direction: "drop",
		SignalDays: 2, HoldDays: 8, TakeProfitPct: 0.02, StopLossPct: 0.05,
		Regime: "All Regimes", AllocationPct: 0.65, CashYield: 0.045,
	}
	compareSQL(t, market, calc, dropAll, gld, gld)

	dropReg := dropAll
	dropReg.ID = "streak-gld-down2-gld-sma"
	dropReg.Regime = "GLD>=SMA200"
	compareSQL(t, market, calc, dropReg, gld, gld)

	// The regime filter must drop at least one down-streak bar that All Regimes kept.
	all := signalDates(t, market, calc, dropAll, gld, gld)
	reg := signalDates(t, market, calc, dropReg, gld, gld)
	if len(all) == 0 {
		t.Fatal("All Regimes produced no GLD signals")
	}
	if len(reg) >= len(all) {
		t.Fatalf("regime filter did not reject a bar: all %v regime %v", all, reg)
	}
}

func compareSQL(t *testing.T, market, calc string, row refdb.StreakStrategy, signal, trade []models.Bar) {
	t.Helper()
	sqlDates, sqlByDate := runSQL(t, market, calc, row)
	goSigs := StreakSignals(signal, trade, row.SignalDays, row.Direction, row.Regime, row.TakeProfitPct, row.StopLossPct, row.HoldDays, row.TradeSymbol, row.ID)
	if len(sqlDates) != len(goSigs) {
		t.Fatalf("%s sql dates %v (%d) go %d", row.ID, sqlDates, len(sqlDates), len(goSigs))
	}
	for _, g := range goSigs {
		s, ok := sqlByDate[g.Date]
		if !ok {
			t.Fatalf("%s missing sql signal on %s", row.ID, g.Date)
		}
		if s.Symbol != g.Symbol || s.HoldDaysOverride != g.HoldDaysOverride {
			t.Fatalf("%s %s sql %+v go %+v", row.ID, g.Date, s, g)
		}
		if math.Abs(s.TakeProfit-g.TakeProfit) > 1e-6 || math.Abs(s.StopLoss-g.StopLoss) > 1e-6 {
			t.Fatalf("%s %s tp/sl sql %v/%v go %v/%v", row.ID, g.Date, s.TakeProfit, s.StopLoss, g.TakeProfit, g.StopLoss)
		}
	}
}

func signalDates(t *testing.T, market, calc string, row refdb.StreakStrategy, signal, trade []models.Bar) []string {
	t.Helper()
	dates, _ := runSQL(t, market, calc, row)
	goSigs := StreakSignals(signal, trade, row.SignalDays, row.Direction, row.Regime, row.TakeProfitPct, row.StopLossPct, row.HoldDays, row.TradeSymbol, row.ID)
	if len(dates) != len(goSigs) {
		t.Fatalf("dates diverged before regime check")
	}
	return dates
}

func runSQL(t *testing.T, market, calc string, row refdb.StreakStrategy) ([]string, map[string]models.Signal) {
	t.Helper()
	s := &Strategy{Row: row}
	s.SetDatabases(market, calc)
	sigs := s.GenerateSignals(nil)
	by := map[string]models.Signal{}
	var dates []string
	for _, sig := range sigs {
		by[sig.Date] = sig
		dates = append(dates, sig.Date)
	}
	return dates, by
}

func sampleRow() refdb.StreakStrategy {
	return refdb.StreakStrategy{
		ID: "streak-voo-up5-tqqq", Name: "VOO up5 → TQQQ",
		SignalSymbol: "VOO", TradeSymbol: "TQQQ", Direction: "rally",
		SignalDays: 2, HoldDays: 15, TakeProfitPct: 0.03, StopLossPct: 0.08,
		Regime: "All Regimes", AllocationPct: 0.65, CashYield: 0.045,
	}
}

func upSeries() (voo, tqqq []models.Bar) {
	// Two-day up streaks on i=2,3,4 and i=7.
	closes := []float64{10, 11, 12, 13, 14, 13, 14, 15}
	trade := []float64{100, 110, 120, 130, 140, 150, 160, 170}
	for i, c := range closes {
		d := dateAt(i)
		voo = append(voo, models.Bar{Idx: i, Symbol: "VOO", Date: d, Open: c, High: c, Low: c, Close: c, Volume: 1000})
		tqqq = append(tqqq, models.Bar{Idx: i, Symbol: "TQQQ", Date: d, Open: trade[i], High: trade[i], Low: trade[i], Close: trade[i], Volume: 1000})
	}
	applySMA200(voo)
	return voo, tqqq
}

func downSeries() []models.Bar {
	closes := []float64{10, 11, 12, 9, 8, 11, 10.5, 10.3}
	var gld []models.Bar
	for i, c := range closes {
		gld = append(gld, models.Bar{Idx: i, Symbol: "GLD", Date: dateAt(i), Open: c, High: c, Low: c, Close: c, Volume: 1000})
	}
	applySMA200(gld)
	return gld
}

func dateAt(i int) string {
	return fmt.Sprintf("2024-01-%02d", i+2)
}

func applySMA200(bars []models.Bar) {
	for i := range bars {
		start := i - 199
		if start < 0 {
			start = 0
		}
		var sum float64
		for j := start; j <= i; j++ {
			sum += bars[j].Close
		}
		bars[i].SMA200 = sum / float64(i-start+1)
	}
}

func writeMarket(path string, series ...[]models.Bar) error {
	db, err := sqlx.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE backtest_start (
		idx INTEGER, symbol TEXT, Date TEXT, open REAL, high REAL, low REAL, close REAL, volume INTEGER
	)`); err != nil {
		return err
	}
	for _, bars := range series {
		for _, b := range bars {
			if _, err := db.Exec(`INSERT INTO backtest_start (idx, symbol, Date, open, high, low, close, volume) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				b.Idx, b.Symbol, b.Date, b.Open, b.High, b.Low, b.Close, b.Volume); err != nil {
				return err
			}
		}
	}
	return nil
}

func registryIDs() map[string]bool {
	out := map[string]bool{}
	for _, s := range strategy.List() {
		out[s.ID()] = true
	}
	return out
}
