package strategy

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestSigVooBuySpxu_GenerateSignals(t *testing.T) {
	AutoRegisterSQLStrategies("../..", "../../data/market_history.db")
	s := NewSigVooBuySpxu()
	s.SetDatabases("../../data/market_history.db", ":memory:")
	sigs := s.GenerateSignals(map[string][]models.Bar{})
	if len(sigs) == 0 {
		t.Fatalf("expected non-zero signals for SigVooBuySpxu, got 0")
	}
	for _, sig := range sigs {
		if sig.Symbol != "SPXU" {
			t.Fatalf("unexpected non-SPXU signal: %+v", sig)
		}
	}
}
