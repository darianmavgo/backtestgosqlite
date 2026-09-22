package strategy

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

// treeBounceParity loads real bars for symbol from the market DB, runs the
// pure-Go TreeBounceSignals fallback directly and the given strategy's real
// GenerateSignals (which prefers its SQL pipeline once SetDatabases is
// called) side by side, and asserts they produce identical entry dates. This
// is the parity check for moving mara_tree/nvdl_tree/pdd_tree's SMA200/ATR14
// rolling-window math from Go loops (pkg/strategy/mara_tree.go's
// TreeBounceSignals) into SQL window functions (sql/strategies/<dir>).
func treeBounceParity(t *testing.T, symbol string, strat Strategy, tp, sl float64, hold int) {
	t.Helper()

	db, err := storage.OpenSQLite("../../data/market_history.db")
	if err != nil {
		t.Fatalf("open market db: %v", err)
	}
	defer db.Close()

	barsBySymbol, _, err := storage.FetchBars(db, "backtest_start", []string{symbol}, "", "")
	if err != nil {
		t.Fatalf("fetch bars: %v", err)
	}
	bars, ok := barsBySymbol[symbol]
	if !ok || len(bars) < 250 {
		t.Fatalf("not enough %s bars in market db to test (%d)", symbol, len(bars))
	}

	goSignals := TreeBounceSignals(symbol, bars, tp, sl, hold)
	if len(goSignals) == 0 {
		t.Fatalf("expected at least one Go-fallback signal for %s", symbol)
	}
	goDates := make(map[string]bool, len(goSignals))
	for _, sig := range goSignals {
		goDates[sig.Date] = true
	}

	AutoRegisterSQLStrategies("../..", "../../data/market_history.db")
	strat.SetDatabases("../../data/market_history.db", ":memory:")
	sqlSignals := strat.GenerateSignals(barsBySymbol)

	if len(sqlSignals) != len(goSignals) {
		t.Fatalf("%s: SQL pipeline produced %d signals, Go fallback produced %d", symbol, len(sqlSignals), len(goSignals))
	}
	for _, sig := range sqlSignals {
		if sig.Symbol != symbol {
			t.Errorf("%s: expected symbol %s, got %s", symbol, symbol, sig.Symbol)
		}
		if !goDates[sig.Date] {
			t.Errorf("%s: SQL pipeline signaled on %s, which the Go fallback did not", symbol, sig.Date)
		}
	}
}

func TestMARATree_SQLPipeline_MatchesGoFallback(t *testing.T) {
	treeBounceParity(t, "MARA", NewMARATreeStrategy(), 0.05, 0.08, 1)
}

func TestNVDLTree_SQLPipeline_MatchesGoFallback(t *testing.T) {
	treeBounceParity(t, "NVDL", NewNVDLTreeStrategy(), 0.15, 0.07, 5)
}

func TestPDDTree_SQLPipeline_MatchesGoFallback(t *testing.T) {
	treeBounceParity(t, "PDD", NewPDDTreeStrategy(), 0.05, 0.06, 3)
}

func TestTreeBounceCombo_SQLPipeline(t *testing.T) {
	AutoRegisterSQLStrategies("../..", "../../data/market_history.db")
	s := NewTreeBounceCombo()
	s.SetDatabases("../../data/market_history.db", ":memory:")

	sigs := s.GenerateSignals(map[string][]models.Bar{})
	if len(sigs) == 0 {
		t.Fatalf("expected non-zero signals generated for TreeBounceCombo via SQL pipeline, got 0")
	}

	bySymbol := map[string]int{}
	for _, sig := range sigs {
		bySymbol[sig.Symbol]++
		if sig.Direction != "LONG" {
			t.Errorf("expected LONG direction, got %s", sig.Direction)
		}
	}
	for _, sym := range []string{"MARA", "PDD", "NVDL"} {
		if bySymbol[sym] == 0 {
			t.Errorf("expected at least one signal for leg %s, got 0", sym)
		}
	}
}
