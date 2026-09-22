package strategy

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

// TestFitDecisionTreeBuyDatesSQL_MatchesGoFallback checks that feature
// extraction via the SQL pipeline (sql/strategies/decision_tree_features)
// produces the same CloudForest-fit BUY dates as the pure-Go
// computeDecisionTreeSamples path, for every symbol with enough history in
// the market DB to fit a tree. This is the parity check for moving
// decisiontree.go's 13-feature rolling-window computation into SQL window
// functions.
func TestFitDecisionTreeBuyDatesSQL_MatchesGoFallback(t *testing.T) {
	db, err := storage.OpenSQLite("../../data/market_history.db")
	if err != nil {
		t.Fatalf("open market db: %v", err)
	}
	defer db.Close()

	for _, symbol := range []string{"MARA", "PDD", "NVDL"} {
		t.Run(symbol, func(t *testing.T) {
			barsBySymbol, _, err := storage.FetchBars(db, "backtest_start", []string{symbol}, "", "")
			if err != nil {
				t.Fatalf("fetch bars: %v", err)
			}
			bars := barsBySymbol[symbol]
			if len(bars) < 260 {
				t.Fatalf("not enough %s bars to test (%d)", symbol, len(bars))
			}

			goBuyDates, err := FitDecisionTreeBuyDates(bars)
			if err != nil {
				t.Fatalf("Go fallback fit failed: %v", err)
			}

			sqlBuyDates, err := FitDecisionTreeBuyDatesSQL("../../data/market_history.db", ":memory:", "../../sql/strategies/decision_tree_features", symbol)
			if err != nil {
				t.Fatalf("SQL pipeline fit failed: %v", err)
			}

			if len(sqlBuyDates) != len(goBuyDates) {
				t.Fatalf("%s: SQL fit produced %d BUY dates, Go fallback produced %d", symbol, len(sqlBuyDates), len(goBuyDates))
			}
			for d := range goBuyDates {
				if !sqlBuyDates[d] {
					t.Errorf("%s: Go fallback predicted BUY on %s, SQL pipeline did not", symbol, d)
				}
			}
		})
	}
}

func TestETFDecisionTreeStrategy_SQLPipeline(t *testing.T) {
	db, err := storage.OpenSQLite("../../data/market_history.db")
	if err != nil {
		t.Fatalf("open market db: %v", err)
	}
	defer db.Close()

	symbol := "MARA"
	barsBySymbol, _, err := storage.FetchBars(db, "backtest_start", []string{symbol}, "", "")
	if err != nil {
		t.Fatalf("fetch bars: %v", err)
	}

	s := &ETFDecisionTreeStrategy{Symbol: symbol, TP: 0.05, SL: 0.08, Hold: 1, PipelineDir: "../../sql/strategies/decision_tree_features"}
	goSigs := s.GenerateSignals(barsBySymbol)
	if len(goSigs) == 0 {
		t.Fatalf("expected non-zero Go-fallback signals")
	}

	s.SetDatabases("../../data/market_history.db", ":memory:")
	sqlSigs := s.GenerateSignals(barsBySymbol)
	if len(sqlSigs) != len(goSigs) {
		t.Fatalf("SQL pipeline produced %d signals, Go fallback produced %d", len(sqlSigs), len(goSigs))
	}
	for _, sig := range sqlSigs {
		if sig.Symbol != symbol {
			t.Errorf("expected symbol %s, got %s", symbol, sig.Symbol)
		}
	}
}
