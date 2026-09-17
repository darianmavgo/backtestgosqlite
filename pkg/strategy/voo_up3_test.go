package strategy

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestVOOUpStreakDates_ThreeUpDays(t *testing.T) {
	voo := []models.Bar{
		{Date: "2026-01-01", Close: 100},
		{Date: "2026-01-02", Close: 101}, // 1 up
		{Date: "2026-01-03", Close: 102}, // 2 up
		{Date: "2026-01-04", Close: 103}, // 3 up → signal
		{Date: "2026-01-05", Close: 102}, // down, reset
		{Date: "2026-01-06", Close: 104}, // 1 up
		{Date: "2026-01-07", Close: 105}, // 2 up
		{Date: "2026-01-08", Close: 106}, // 3 up → signal
		{Date: "2026-01-09", Close: 107}, // 4 up → signal (streak still >= 3)
	}
	got := VOOUpStreakDates(voo, 3)
	want := []string{"2026-01-04", "2026-01-08", "2026-01-09"}
	if len(got) != len(want) {
		t.Fatalf("dates = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("dates = %v, want %v", got, want)
		}
	}
}

func TestVOOUpStreakSignals_UsesTradeSymbolClose(t *testing.T) {
	voo := []models.Bar{
		{Date: "2026-01-01", Close: 100},
		{Date: "2026-01-02", Close: 101},
		{Date: "2026-01-03", Close: 102},
		{Date: "2026-01-04", Close: 103},
	}
	tecl := []models.Bar{
		{Date: "2026-01-01", Open: 50, High: 51, Low: 49, Close: 50},
		{Date: "2026-01-02", Open: 50, High: 52, Low: 50, Close: 51},
		{Date: "2026-01-03", Open: 51, High: 53, Low: 51, Close: 52},
		{Date: "2026-01-04", Open: 52, High: 55, Low: 52, Close: 54},
	}
	sigs := VOOUpStreakSignals("TECL", voo, tecl, 3, 0.05, 0.06, 8)
	if len(sigs) != 1 {
		t.Fatalf("len(sigs) = %d, want 1", len(sigs))
	}
	if sigs[0].Symbol != "TECL" || sigs[0].Date != "2026-01-04" {
		t.Fatalf("signal = %+v", sigs[0])
	}
	if sigs[0].BuyLimit != 54 {
		t.Errorf("BuyLimit = %v, want TECL close 54", sigs[0].BuyLimit)
	}
	if sigs[0].TakeProfit != 54*1.05 {
		t.Errorf("TakeProfit = %v, want %v", sigs[0].TakeProfit, 54*1.05)
	}
	if sigs[0].HoldDaysOverride != 8 {
		t.Errorf("HoldDaysOverride = %d, want 8", sigs[0].HoldDaysOverride)
	}
}

func TestVOOUp3StrategyRegistration(t *testing.T) {
	s, ok := Get("voo-up3")
	if !ok {
		t.Fatal("voo-up3 not registered")
	}
	cfg := s.DefaultConfig()
	if cfg.AllocationPct != 0.65 {
		t.Errorf("AllocationPct = %v", cfg.AllocationPct)
	}
	req := s.(RequiredSymbolsProvider).RequiredSymbols()
	if len(req) != 2 || req[0] != "VOO" {
		t.Errorf("RequiredSymbols = %v, want [VOO, tradeSymbol]", req)
	}
}
