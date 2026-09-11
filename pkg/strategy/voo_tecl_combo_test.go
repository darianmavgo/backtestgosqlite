package strategy

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestVOOTECLSPXUCombo_GenerateSignals(t *testing.T) {
	AutoRegisterSQLStrategies("../..", "../../data/market_history.db")
	s := NewVOOTECLSPXUCombo()
	sigs := s.GenerateSignals(map[string][]models.Bar{})
	if len(sigs) == 0 {
		t.Fatalf("expected non-zero signals generated for VOOTECLSPXUCombo, got 0")
	}

	hasLong := false
	hasShort := false
	for _, sig := range sigs {
		if sig.Direction == "LONG" {
			hasLong = true
		}
		if sig.Direction == "SHORT" {
			hasShort = true
		}
	}

	if !hasLong {
		t.Errorf("expected at least one LONG signal")
	}
	if !hasShort {
		t.Errorf("expected at least one SHORT signal")
	}
}
