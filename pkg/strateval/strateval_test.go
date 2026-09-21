package strateval

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSplitDatesNoLeak(t *testing.T) {
	dates := []string{
		"2020-01-02", "2021-01-04", "2022-01-03",
		"2023-01-03", "2024-06-03", "2024-12-31",
		"2025-01-02", "2025-06-02", "2025-12-31",
	}
	sp, err := SplitDates(dates, 12)
	if err != nil {
		t.Fatal(err)
	}
	if sp.ISEnd >= sp.OOSStart {
		t.Fatalf("ISEnd %s >= OOSStart %s", sp.ISEnd, sp.OOSStart)
	}
	for _, d := range FilterDatesInclusive(dates, sp.ISStart, sp.ISEnd) {
		if d >= sp.OOSStart {
			t.Fatalf("IS leak %s", d)
		}
	}
}

func TestAssignTier(t *testing.T) {
	g := DefaultGates()
	is := Metrics{MaxDD: 0.10, Sharpe: 1, Trades: 100, AvgTradePct: 0.01}
	oos := Metrics{MaxDD: 0.12, Sharpe: 0.5, Trades: 40, AvgTradePct: 0.01}
	tier, _ := AssignTier(is, oos, g, false)
	if tier != "A" {
		t.Fatalf("want A got %s", tier)
	}
	bad := Metrics{MaxDD: 0.5, Sharpe: -0.2, Trades: 5, AvgTradePct: -0.01}
	tier, _ = AssignTier(is, bad, g, true)
	if tier != "D" {
		t.Fatalf("want D got %s", tier)
	}
}

func TestStoreAndAllowlistDiff(t *testing.T) {
	s, err := OpenStore(filepath.Join(t.TempDir(), "e.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	row := EvalRow{
		RunID: "r1", StrategyID: "demo_a", ParamsJSON: "{}",
		Split: Split{ISStart: "a", ISEnd: "b", OOSStart: "c", OOSEnd: "d"},
		IS: Metrics{Sharpe: 1}, OOS: Metrics{Sharpe: 0.8, Trades: 40, AvgTradePct: 0.01, MaxDD: 0.1},
		Tier: "A", Reasons: []string{"ok"}, CreatedAt: time.Now().UTC(),
	}
	if err := s.Insert(row); err != nil {
		t.Fatal(err)
	}
	row.StrategyID = "live_bad"
	row.Tier = "D"
	if err := s.Insert(row); err != nil {
		t.Fatal(err)
	}
	got, err := s.LatestByStrategy("r1")
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%d err=%v", len(got), err)
	}
	add, demote := AllowlistDiff(got, []string{"live_bad"})
	if len(add) != 1 || add[0] != "demo_a" {
		t.Fatalf("add=%v", add)
	}
	if len(demote) != 1 || demote[0] != "live_bad" {
		t.Fatalf("demote=%v", demote)
	}
}
