package strategy

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

// streakStrategyParity loads real bars for the signal+trade symbol pair from
// the market DB, runs strat's Go fallback and its SQL pipeline side by side,
// and asserts identical entry dates. Parity check for moving
// UpVolumeUpDates/VOOUpStreakDates' two-symbol join out of Go and into SQL.
func streakStrategyParity(t *testing.T, strat Strategy, symbols []string) {
	t.Helper()

	db, err := storage.OpenSQLite("../../data/market_history.db")
	if err != nil {
		t.Fatalf("open market db: %v", err)
	}
	defer db.Close()

	barsBySymbol, _, err := storage.FetchBars(db, "backtest_start", symbols, "", "")
	if err != nil {
		t.Fatalf("fetch bars: %v", err)
	}

	goSignals := strat.GenerateSignals(barsBySymbol)
	if len(goSignals) == 0 {
		t.Fatalf("expected at least one Go-fallback signal")
	}
	goDates := make(map[string]bool, len(goSignals))
	for _, sig := range goSignals {
		goDates[sig.Date] = true
	}

	AutoRegisterSQLStrategies("../..", "../../data/market_history.db")
	strat.SetDatabases("../../data/market_history.db", ":memory:")
	sqlSignals := strat.GenerateSignals(barsBySymbol)

	if len(sqlSignals) != len(goSignals) {
		t.Fatalf("SQL pipeline produced %d signals, Go fallback produced %d", len(sqlSignals), len(goSignals))
	}
	for _, sig := range sqlSignals {
		if !goDates[sig.Date] {
			t.Errorf("SQL pipeline signaled on %s, which the Go fallback did not", sig.Date)
		}
	}
}

func TestSigVooUp1BuyTqqq_SQLPipeline_MatchesGoFallback(t *testing.T) {
	streakStrategyParity(t, NewSigVooUp1BuyTqqq(), []string{"VOO", "TQQQ"})
}

func TestSigQqqUp1BuySqqq_SQLPipeline_MatchesGoFallback(t *testing.T) {
	streakStrategyParity(t, NewSigQqqUp1BuySqqq(), []string{"QQQ", "SQQQ"})
}

func TestSigQqqUp1BuyTqqq_SQLPipeline_MatchesGoFallback(t *testing.T) {
	streakStrategyParity(t, NewSigQqqUp1BuyTqqq(), []string{"QQQ", "TQQQ"})
}

func TestVOOUp3_SQLPipeline_MatchesGoFallback(t *testing.T) {
	streakStrategyParity(t, NewVOOUp3Strategy(), []string{"VOO", "TQQQ"})
}
