package strategy

import (
	"math"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

const floatEps = 1e-9

func approxEq(a, b float64) bool { return math.Abs(a-b) < floatEps }

func makeBar(date string, close float64) models.Bar {
	return models.Bar{Date: date, Open: close, High: close * 1.01, Low: close * 0.99, Close: close, Volume: 1000000}
}

func makeBarSMA(date string, close, sma200 float64) models.Bar {
	b := makeBar(date, close)
	b.SMA200 = sma200
	return b
}

func TestVOOTECLCombo_ID(t *testing.T) {
	s := &VOOTECLCombo{}
	if got := s.ID(); got != "voo-tecl-combo" {
		t.Errorf("ID() = %q, want %q", got, "voo-tecl-combo")
	}
}

func TestVOOTECLCombo_Validate(t *testing.T) {
	s := &VOOTECLCombo{}
	if err := s.Validate(); err != nil {
		t.Errorf("Validate() returned error: %v", err)
	}
}

func TestConsecutiveDrops(t *testing.T) {
	bars := []models.Bar{
		makeBar("2024-01-01", 100),
		makeBar("2024-01-02", 99),
		makeBar("2024-01-03", 98),
		makeBar("2024-01-04", 97),
	}
	if !consecutiveDrops(bars, 3, 3) {
		t.Error("expected 3 consecutive drops at index 3")
	}
	if consecutiveDrops(bars, 2, 3) {
		t.Error("index 2 does not have 3 preceding bars for comparison")
	}
	// Flat close breaks the streak
	bars[2].Close = 99 // same as prev
	if consecutiveDrops(bars, 3, 3) {
		t.Error("flat close should break streak")
	}
}

func TestConsecutiveRallies(t *testing.T) {
	bars := []models.Bar{
		makeBar("2024-01-01", 97),
		makeBar("2024-01-02", 98),
		makeBar("2024-01-03", 99),
		makeBar("2024-01-04", 100),
	}
	if !consecutiveRallies(bars, 3, 3) {
		t.Error("expected 3 consecutive rallies at index 3")
	}
}

func TestGenerateSignals_LongSignalEmitted(t *testing.T) {
	vooBars := []models.Bar{
		makeBar("2024-01-01", 400),
		makeBar("2024-01-02", 399),
		makeBar("2024-01-03", 398),
		makeBar("2024-01-04", 397), // 3-day drop ends here
	}
	teclBars := []models.Bar{
		makeBar("2024-01-01", 50),
		makeBar("2024-01-02", 49),
		makeBar("2024-01-03", 48),
		makeBar("2024-01-04", 47),
	}
	spxuBars := []models.Bar{
		makeBar("2024-01-01", 20),
		makeBar("2024-01-02", 20),
		makeBar("2024-01-03", 20),
		makeBar("2024-01-04", 20),
	}

	s := &VOOTECLCombo{}
	sigs := s.GenerateSignals(map[string][]models.Bar{
		"VOO": vooBars, "TECL": teclBars, "SPXU": spxuBars,
	})

	if len(sigs) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(sigs))
	}
	sig := sigs[0]
	if sig.Symbol != "TECL" {
		t.Errorf("symbol = %q, want TECL", sig.Symbol)
	}
	if sig.Direction != "LONG" {
		t.Errorf("direction = %q, want LONG", sig.Direction)
	}
	if sig.HoldDaysOverride != 8 {
		t.Errorf("HoldDaysOverride = %d, want 8", sig.HoldDaysOverride)
	}
	wantTP := 47.0 * 1.05
	if sig.TakeProfit != wantTP {
		t.Errorf("TakeProfit = %.4f, want %.4f", sig.TakeProfit, wantTP)
	}
	if sig.StopLoss != 0 {
		t.Errorf("long StopLoss should be 0, got %.4f", sig.StopLoss)
	}
}

func TestGenerateSignals_ShortBlockedAboveSMA200(t *testing.T) {
	// 3 up-closes but VOO > SMA200 — short must NOT fire.
	vooBars := []models.Bar{
		makeBarSMA("2024-01-01", 395, 380),
		makeBarSMA("2024-01-02", 396, 381),
		makeBarSMA("2024-01-03", 397, 382),
		makeBarSMA("2024-01-04", 398, 383), // VOO > SMA200, rally → no short
	}
	teclBars := []models.Bar{makeBar("2024-01-01", 50), makeBar("2024-01-02", 50), makeBar("2024-01-03", 50), makeBar("2024-01-04", 50)}
	spxuBars := []models.Bar{makeBar("2024-01-01", 20), makeBar("2024-01-02", 20), makeBar("2024-01-03", 20), makeBar("2024-01-04", 20)}

	s := &VOOTECLCombo{}
	sigs := s.GenerateSignals(map[string][]models.Bar{"VOO": vooBars, "TECL": teclBars, "SPXU": spxuBars})
	for _, sig := range sigs {
		if sig.Symbol == "SPXU" {
			t.Errorf("SPXU signal should not fire when VOO > SMA200, got one on %s", sig.Date)
		}
	}
}

func TestGenerateSignals_ShortFiresBelowSMA200(t *testing.T) {
	// 3 up-closes and VOO < SMA200 — short MUST fire.
	vooBars := []models.Bar{
		makeBarSMA("2024-01-01", 350, 400),
		makeBarSMA("2024-01-02", 351, 401),
		makeBarSMA("2024-01-03", 352, 402),
		makeBarSMA("2024-01-04", 353, 403), // VOO < SMA200 ✓
	}
	teclBars := []models.Bar{makeBar("2024-01-01", 50), makeBar("2024-01-02", 50), makeBar("2024-01-03", 50), makeBar("2024-01-04", 50)}
	spxuBars := []models.Bar{makeBar("2024-01-01", 20), makeBar("2024-01-02", 20), makeBar("2024-01-03", 20), makeBar("2024-01-04", 20)}

	s := &VOOTECLCombo{}
	sigs := s.GenerateSignals(map[string][]models.Bar{"VOO": vooBars, "TECL": teclBars, "SPXU": spxuBars})

	var found bool
	for _, sig := range sigs {
		if sig.Symbol == "SPXU" && sig.Direction == "SHORT" {
			found = true
			if sig.HoldDaysOverride != 2 {
				t.Errorf("short HoldDaysOverride = %d, want 2", sig.HoldDaysOverride)
			}
			wantTP := 20.0 * 1.06
			if !approxEq(sig.TakeProfit, wantTP) {
				t.Errorf("short TakeProfit = %.10f, want %.10f", sig.TakeProfit, wantTP)
			}
			wantSL := 20.0 * 0.95
			if !approxEq(sig.StopLoss, wantSL) {
				t.Errorf("short StopLoss = %.10f, want %.10f", sig.StopLoss, wantSL)
			}
		}
	}
	if !found {
		t.Error("expected an SPXU SHORT signal when VOO < SMA200, got none")
	}
}

func TestGenerateSignals_LongPriorityOverShort(t *testing.T) {
	// 3-day drop → LONG fires. Even though the same day has bearish regime context,
	// a drop cannot simultaneously be a rally, so LONG wins trivially.
	// Verify no SHORT is emitted when LONG is present.
	vooBars := []models.Bar{
		makeBarSMA("2024-01-01", 400, 420), // VOO < SMA200
		makeBarSMA("2024-01-02", 399, 421),
		makeBarSMA("2024-01-03", 398, 422),
		makeBarSMA("2024-01-04", 397, 423), // 3-day drop
	}
	teclBars := []models.Bar{makeBar("2024-01-01", 50), makeBar("2024-01-02", 50), makeBar("2024-01-03", 50), makeBar("2024-01-04", 50)}
	spxuBars := []models.Bar{makeBar("2024-01-01", 20), makeBar("2024-01-02", 20), makeBar("2024-01-03", 20), makeBar("2024-01-04", 20)}

	s := &VOOTECLCombo{}
	sigs := s.GenerateSignals(map[string][]models.Bar{"VOO": vooBars, "TECL": teclBars, "SPXU": spxuBars})

	for _, sig := range sigs {
		if sig.Direction == "SHORT" {
			t.Errorf("no SHORT should be emitted on a drop day, got %s on %s", sig.Symbol, sig.Date)
		}
	}
}

func TestGenerateSignals_InsufficientBars(t *testing.T) {
	s := &VOOTECLCombo{}
	sigs := s.GenerateSignals(map[string][]models.Bar{
		"VOO":  {makeBar("2024-01-01", 400), makeBar("2024-01-02", 399)},
		"TECL": {makeBar("2024-01-01", 50)},
		"SPXU": {makeBar("2024-01-01", 20)},
	})
	if len(sigs) != 0 {
		t.Errorf("expected 0 signals with < 4 VOO bars, got %d", len(sigs))
	}
}
