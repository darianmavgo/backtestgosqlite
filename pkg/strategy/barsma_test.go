package strategy

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/realbars"
)

func mean(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// Real GOOGL closes: the slice table's averages must equal a plain trailing
// mean of the real closes, even when only the last bars are handed in.
func TestLoadBarSMAFromSliceTable(t *testing.T) {
	market := realbars.Copy(t, "GOOGL")
	closes := realbars.Closes(t, market, "GOOGL")
	dates := realbars.Dates(t, market, "GOOGL")
	if len(closes) < 260 {
		t.Skipf("GOOGL has only %d bars", len(closes))
	}

	const handedIn = 10
	var in []models.Bar
	for i := len(closes) - handedIn; i < len(closes); i++ {
		in = append(in, models.Bar{Symbol: "GOOGL", Date: dates[i], Close: closes[i]})
	}
	bars := map[string][]models.Bar{"GOOGL": in}
	if err := LoadBarSMA(market, filepath.Join(t.TempDir(), "calc.db"), bars); err != nil {
		t.Fatal(err)
	}
	for k, b := range bars["GOOGL"] {
		i := len(closes) - handedIn + k
		want50 := mean(closes[i-49 : i+1])
		want200 := mean(closes[i-199 : i+1])
		if math.Abs(b.SMA50-want50) > 1e-9 || math.Abs(b.SMA200-want200) > 1e-9 {
			t.Fatalf("%s: SMA50 %v want %v, SMA200 %v want %v", b.Date, b.SMA50, want50, b.SMA200, want200)
		}
	}
}
