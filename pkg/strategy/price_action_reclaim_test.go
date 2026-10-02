package strategy

import (
	"math"
	"testing"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestPriceActionReclaimStrategy_GenerateSignals(t *testing.T) {
	s := &PriceActionReclaimStrategy{}

	// Create a sequence of bars
	var bars []models.Bar

	// Generate initial 230 dummy bars
	for i := 0; i < 230; i++ {
		bars = append(bars, models.Bar{
			Idx:    i,
			Date:   time.Now().AddDate(0, 0, i-300).Format("2006-01-02"),
			Open:   100,
			High:   105,
			Low:    100,
			Close:  100,
			Volume: 1000,
			SMA200: 90, // uptrend
		})
	}

	// Generate a support level around 100
	for i := 230; i < 260; i++ {
		bars = append(bars, models.Bar{
			Idx:    i,
			Date:   time.Now().AddDate(0, 0, i-300).Format("2006-01-02"),
			Open:   105,
			High:   110,
			Low:    100,
			Close:  105,
			Volume: 1000,
			SMA200: 90,
		})
	}

	// Breakdown bar (closes below support of 100)
	bars = append(bars, models.Bar{
		Idx:    260,
		Date:   time.Now().AddDate(0, 0, -40).Format("2006-01-02"),
		Open:   105,
		High:   105,
		Low:    95,
		Close:  98,
		Volume: 1000,
		SMA200: 90,
	})

	// Reclaim bar (closes above support)
	bars = append(bars, models.Bar{
		Idx:    261,
		Date:   time.Now().AddDate(0, 0, -39).Format("2006-01-02"),
		Open:   98,
		High:   108,
		Low:    97,
		Close:  105,
		Volume: 1000,
		SMA200: 90,
	})

	barsBySymbol := map[string][]models.Bar{
		"TEST": bars,
	}

	signals := s.GenerateSignals(barsBySymbol)

	if len(signals) != 1 {
		t.Fatalf("Expected 1 signal, got %d", len(signals))
	}

	sig := signals[0]
	if sig.Symbol != "TEST" {
		t.Errorf("Expected symbol TEST, got %s", sig.Symbol)
	}
	if sig.Entry != 1 {
		t.Errorf("Expected Entry=1, got %d", sig.Entry)
	}

	// Expected stop loss multiplier: support (100) * 0.995 / close (105)
	expectedSL := (100.0 * 0.995) / 105.0
	if math.Abs(sig.StopLoss-expectedSL) > 1e-6 {
		t.Errorf("Expected StopLoss %f, got %f", expectedSL, sig.StopLoss)
	}
}
