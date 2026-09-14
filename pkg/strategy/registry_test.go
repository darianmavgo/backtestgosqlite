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
