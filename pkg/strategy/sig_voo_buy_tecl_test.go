package strategy

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestSigVooBuyTecl_GenerateSignals(t *testing.T) {
	AutoRegisterSQLStrategies("../..", "../../data/market_history.db")
	s := NewSigVooBuyTecl()
	s.SetDatabases("../../data/market_history.db", ":memory:")
	sigs := s.GenerateSignals(map[string][]models.Bar{})
	if len(sigs) == 0 {
		t.Fatalf("expected non-zero signals generated for SigVooBuyTecl, got 0")
	}

	hasLong := false
	for _, sig := range sigs {
		if sig.Direction == "LONG" {
			hasLong = true
		}
		if sig.Direction == "SHORT" || sig.Symbol == "SPXU" {
			t.Fatalf("unexpected SPXU/SHORT signal: %+v", sig)
		}
	}

	if !hasLong {
		t.Errorf("expected at least one LONG signal")
	}
}
