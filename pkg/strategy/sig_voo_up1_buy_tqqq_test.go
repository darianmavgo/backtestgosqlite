package strategy

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestSigVooUp1BuyTqqq_UpAndVolumeUp(t *testing.T) {
	voo := []models.Bar{
		{Date: "2026-01-02", Close: 100, Volume: 1000},
		{Date: "2026-01-03", Close: 101, Volume: 1500}, // up, vol up   → signal
		{Date: "2026-01-04", Close: 102, Volume: 1200}, // up, vol down → no
		{Date: "2026-01-05", Close: 101, Volume: 1900}, // down, vol up → no
		{Date: "2026-01-06", Close: 101, Volume: 2000}, // flat, vol up → no
		{Date: "2026-01-07", Close: 103, Volume: 2500}, // up, vol up   → signal
		{Date: "2026-01-08", Close: 104, Volume: 0},    // missing volume → no
	}
	tqqq := make([]models.Bar, len(voo))
	for i, b := range voo {
		tqqq[i] = models.Bar{Date: b.Date, Close: 50, Open: 50, High: 51, Low: 49}
	}
	s := NewSigVooUp1BuyTqqq()
	sigs := s.GenerateSignals(map[string][]models.Bar{"VOO": voo, "TQQQ": tqqq})
	if len(sigs) != 2 || sigs[0].Date != "2026-01-03" || sigs[1].Date != "2026-01-07" {
		t.Fatalf("signals = %+v", sigs)
	}
	g := sigs[0]
	if g.Symbol != "TQQQ" || g.TakeProfit != 50*1.08 || g.StopLoss != 50*0.90 || g.BuyLimit != 50 {
		t.Fatalf("bad signal: %+v", g)
	}
	cfg := s.DefaultConfig()
	if cfg.TakeProfitPct != 0.08 || cfg.StopLossPct != 0.90 {
		t.Fatalf("cfg TP=%v SL=%v", cfg.TakeProfitPct, cfg.StopLossPct)
	}
}
