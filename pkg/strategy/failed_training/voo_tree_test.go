//go:build ignore

package strategy

import (
	"fmt"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestVOOTreeStrategyRegistration(t *testing.T) {
	strat, exists := Get("voo_tree")
	if !exists {
		t.Fatal("voo_tree not registered")
	}
	if strat.ID() != "voo_tree" {
		t.Fatalf("ID = %s", strat.ID())
	}
	cfg := strat.DefaultConfig()
	if cfg.AllocationPct != 0.65 {
		t.Errorf("AllocationPct = %v, want 0.65", cfg.AllocationPct)
	}
	if cfg.HoldingWindow != vooTreeHoldDays {
		t.Errorf("HoldingWindow = %d, want %d", cfg.HoldingWindow, vooTreeHoldDays)
	}
	req, ok := strat.(RequiredSymbolsProvider)
	if !ok || len(req.RequiredSymbols()) != 1 || req.RequiredSymbols()[0] != "VOO" {
		t.Errorf("RequiredSymbols = %v, want [VOO]", req.RequiredSymbols())
	}
}

func TestVOOTreeYearHelpers(t *testing.T) {
	if !vooTreeIsTrainYear(2021) || !vooTreeIsTrainYear(2022) {
		t.Fatal("2021 and 2022 should be train years")
	}
	if vooTreeIsTrainYear(2020) || vooTreeIsTrainYear(2023) {
		t.Fatal("2020/2023 must not be train years")
	}
	for _, y := range []int{2020, 2023, 2024, 2025, 2026} {
		if !vooTreeIsEvalYear(y) {
			t.Errorf("%d should be an eval year", y)
		}
	}
	if vooTreeIsEvalYear(2021) || vooTreeIsEvalYear(2022) {
		t.Fatal("train years must not be eval years")
	}
}

func TestVOOTreeGenerateSignals_SkipsTrainYears(t *testing.T) {
	strat, ok := Get("voo_tree")
	if !ok {
		t.Fatal("voo_tree not registered")
	}
	bars := make([]models.Bar, 0, 2100)
	y, m, d := 2019, 1, 2
	price, vol := 250.0, 5_000_000.0
	for len(bars) < 2100 {
		date := fmt.Sprintf("%04d-%02d-%02d", y, m, d)
		o := price * 0.998
		c := price
		bars = append(bars, models.Bar{
			Symbol: "VOO", Date: date, Open: o, High: c * 1.01, Low: o * 0.99, Close: c, Volume: int64(vol),
		})
		if len(bars)%7 == 0 {
			price *= 1.004
			vol *= 1.02
		} else if len(bars)%5 == 0 {
			price *= 0.997
			vol *= 0.98
		} else {
			price *= 1.0005
		}
		d++
		if d > 28 {
			d = 1
			m++
		}
		if m > 12 {
			m = 1
			y++
		}
		if y > 2026 {
			break
		}
	}
	sigs := strat.GenerateSignals(map[string][]models.Bar{"VOO": bars})
	for _, sig := range sigs {
		yr := vooTreeDateYear(sig.Date)
		if vooTreeIsTrainYear(yr) {
			t.Fatalf("signal on train year %s", sig.Date)
		}
		if !vooTreeIsEvalYear(yr) {
			t.Fatalf("signal on non-eval year %s", sig.Date)
		}
		if sig.Symbol != "VOO" {
			t.Fatalf("symbol = %s", sig.Symbol)
		}
	}
}
