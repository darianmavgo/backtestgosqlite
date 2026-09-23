package strategy

import (
	"testing"
)

func TestAllStrategiesHaveAllocationPct(t *testing.T) {
	strats := List()
	if len(strats) == 0 {
		t.Fatalf("No strategies registered")
	}

	for _, s := range strats {
		cfg := s.DefaultConfig()
		if cfg.AllocationPct <= 0 || cfg.AllocationPct > 1.0 {
			t.Errorf("Strategy '%s' (%s) has invalid AllocationPct: %f (must be > 0 and <= 1.0)",
				s.ID(), s.Name(), cfg.AllocationPct)
		} else {
			t.Logf("✓ Strategy '%-20s': AllocationPct = %.2f", s.ID(), cfg.AllocationPct)
		}
	}
}

// Strategies come only from Go files in pkg/strategy itself: a SQL pipeline
// folder registers only when a Go strategy in this package owns it.
func TestSQLPipelinesRegisterOnlyWithGoStrategy(t *testing.T) {
	AutoRegisterSQLStrategies("../..", "../../data/market_history.db")

	for _, dir := range []string{"shared_account", "bb_capitulation"} {
		if _, ok := Get(dir + "-sql"); ok {
			t.Errorf("%s-sql must not be registered (no Go strategy in pkg/strategy)", dir)
		}
	}
	for _, dir := range []string{"gld_decline", "sig_voo_buy_tecl", "sig_voo_buy_spxu"} {
		if _, ok := Get(dir + "-sql"); !ok {
			t.Errorf("%s-sql should be registered (owned by a Go strategy)", dir)
		}
	}
}
