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

func TestSessionsSkipExchangeHolidays(t *testing.T) {
	et := time.FixedZone("ET", -5*3600)
	// Wed 2025-11-26 evening: Thursday is Thanksgiving, so the next session is Friday.
	evening := time.Date(2025, 11, 26, 18, 0, 0, 0, et)
	if got := LastCompletedEquitySession(evening).Format("2006-01-02"); got != "2025-11-26" {
		t.Errorf("last completed %s, want 2025-11-26", got)
	}
	if got := NextEquitySession(LastCompletedEquitySession(evening)).Format("2006-01-02"); got != "2025-11-28" {
		t.Errorf("next session %s, want 2025-11-28 (Thanksgiving is closed)", got)
	}
	// Friday 2026-04-03 is Good Friday: on Saturday the last completed session is Thursday.
	sat := time.Date(2026, 4, 4, 12, 0, 0, 0, et)
	if got := LastCompletedEquitySession(sat).Format("2006-01-02"); got != "2026-04-02" {
		t.Errorf("last completed on Good Friday weekend %s, want 2026-04-02", got)
	}
	if got := NextEquitySession(LastCompletedEquitySession(sat)).Format("2006-01-02"); got != "2026-04-06" {
		t.Errorf("next session %s, want Monday 2026-04-06", got)
	}
}
