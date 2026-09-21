package strategy

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestSigQqqUp1BuySqqq_SignalsAndConfig(t *testing.T) {
	qqq := []models.Bar{
		{Date: "2026-01-02", Close: 100, Volume: 1000},
		{Date: "2026-01-03", Close: 101, Volume: 1500}, // up, vol up   → signal
		{Date: "2026-01-04", Close: 100, Volume: 1800}, // down, vol up → no
		{Date: "2026-01-05", Close: 102, Volume: 1700}, // up, vol down → no
	}
	sqqq := make([]models.Bar, len(qqq))
	for i, b := range qqq {
		sqqq[i] = models.Bar{Date: b.Date, Close: 20, Open: 20, High: 21, Low: 19}
	}
	s := NewSigQqqUp1BuySqqq()
	sigs := s.GenerateSignals(map[string][]models.Bar{"QQQ": qqq, "SQQQ": sqqq})
	if len(sigs) != 1 || sigs[0].Date != "2026-01-03" || sigs[0].Symbol != "SQQQ" {
		t.Fatalf("signals = %+v", sigs)
	}
	g := sigs[0]
	if g.TakeProfit != 20*1.08 || g.StopLoss != 0 || g.HoldDaysOverride != 1 {
		t.Fatalf("bad signal: %+v", g)
	}
	if cfg := s.DefaultConfig(); cfg.StopLossPct != 0 || cfg.HoldingWindow != 1 {
		t.Fatalf("cfg stop=%v hold=%v", cfg.StopLossPct, cfg.HoldingWindow)
	}
}
