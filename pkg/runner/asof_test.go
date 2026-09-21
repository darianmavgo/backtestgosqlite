package runner

import (
	"testing"
	"time"
)

func TestLastCompletedEquitySessionMondayMorning(t *testing.T) {
	mon := time.Date(2026, 9, 21, 10, 0, 0, 0, time.FixedZone("ET", -4*3600))
	got := LastCompletedEquitySession(mon).Format("2006-01-02")
	if got != "2026-09-18" {
		t.Fatalf("got %s want 2026-09-18", got)
	}
}

func TestResolveLiveAsOfStaleNeverAllowed(t *testing.T) {
	now := time.Date(2026, 9, 21, 10, 0, 0, 0, time.FixedZone("ET", -4*3600))
	_, _, err := ResolveLiveAsOf("2026-09-16", now)
	if err == nil {
		t.Fatal("expected stale error")
	}
}

func TestResolveStrategies(t *testing.T) {
	_, err := ResolveStrategies("", "")
	if err == nil {
		t.Fatal("expected error on empty")
	}
}
