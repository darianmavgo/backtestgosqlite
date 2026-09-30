package strategy

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func yrBars(sym string, yearOpen, yearClose float64, holdYear bool) []models.Bar {
	bars := []models.Bar{
		{Symbol: sym, Date: "2020-01-02", Open: yearOpen, High: yearOpen, Low: yearOpen, Close: yearOpen},
		{Symbol: sym, Date: "2020-12-31", Open: yearClose, High: yearClose, Low: yearClose, Close: yearClose},
	}
	if holdYear {
		bars = append(bars,
			models.Bar{Symbol: sym, Date: "2021-01-04", Open: 50, High: 50, Low: 50, Close: 50},
			models.Bar{Symbol: sym, Date: "2021-12-31", Open: 40, High: 40, Low: 40, Close: 40},
		)
	}
	return bars
}

func TestAnnualWinnerLongShortAndInverse(t *testing.T) {
	// TQQQ was the 2020 winner. SQQQ is its 3x inverse. ZZZZ has no pair.
	bars := map[string][]models.Bar{
		"TQQQ": yrBars("TQQQ", 10, 40, true), // +300%
		"SQQQ": yrBars("SQQQ", 10, 4, true),  // -60%
		"SPY":  yrBars("SPY", 10, 12, true),  // +20%
	}

	long := annualWinnerSignals(bars, annualLong)
	if len(long) != 2 || long[0].Symbol != "TQQQ" || long[0].Date != "2021-01-04" || long[0].Entry != 1 || long[0].Direction != "" {
		t.Fatalf("long entry: %+v", long)
	}
	if long[1].Symbol != "TQQQ" || long[1].Date != "2021-12-31" || long[1].Entry != -1 {
		t.Fatalf("long exit: %+v", long[1])
	}

	short := annualWinnerSignals(bars, annualShort)
	if len(short) != 2 || short[0].Symbol != "TQQQ" || short[0].Direction != "SHORT" || short[0].Entry != 1 {
		t.Fatalf("short entry: %+v", short)
	}
	if short[1].Direction != "SHORT" || short[1].Entry != -1 {
		t.Fatalf("short exit: %+v", short[1])
	}

	inv := annualWinnerSignals(bars, annualInverse)
	if len(inv) != 2 || inv[0].Symbol != "SQQQ" || inv[0].Date != "2021-01-04" || inv[0].Direction != "" || inv[0].Close != 50 {
		t.Fatalf("inverse entry: %+v", inv)
	}
	if inv[1].Symbol != "SQQQ" || inv[1].Entry != -1 {
		t.Fatalf("inverse exit: %+v", inv[1])
	}
}

func TestAnnualInverseSkipsWinnerWithNoPair(t *testing.T) {
	bars := map[string][]models.Bar{
		"ZZZZ": yrBars("ZZZZ", 10, 80, true),
		"SPY":  yrBars("SPY", 10, 12, true),
	}
	inv := annualWinnerSignals(bars, annualInverse)
	if len(inv) != 0 {
		t.Fatalf("expected cash year, got %+v", inv)
	}
	long := annualWinnerSignals(bars, annualLong)
	if len(long) != 2 || long[0].Symbol != "ZZZZ" {
		t.Fatalf("long should still take ZZZZ: %+v", long)
	}
}

func TestAnnualInverseSkipsWhenPairMissesEntryDate(t *testing.T) {
	bars := map[string][]models.Bar{
		"TQQQ": yrBars("TQQQ", 10, 40, true),
		"SQQQ": {
			{Symbol: "SQQQ", Date: "2020-01-02", Open: 10, Close: 10},
			{Symbol: "SQQQ", Date: "2020-12-31", Open: 4, Close: 4},
			// No 2021-01-04 bar. Listed later in the hold year.
			{Symbol: "SQQQ", Date: "2021-06-01", Open: 20, Close: 20},
			{Symbol: "SQQQ", Date: "2021-12-31", Open: 15, Close: 15},
		},
	}
	if got := annualWinnerSignals(bars, annualInverse); len(got) != 0 {
		t.Fatalf("expected skip, got %+v", got)
	}
}

func TestInverseETFPairsRoundTrip(t *testing.T) {
	for a, b := range inverseETF {
		back, ok := InverseETF(b)
		if !ok || back != a {
			t.Errorf("%s → %s → %s", a, b, back)
		}
	}
	if _, ok := InverseETF("TSLL"); ok {
		t.Error("single-name bull should not have a guessed inverse")
	}
}
