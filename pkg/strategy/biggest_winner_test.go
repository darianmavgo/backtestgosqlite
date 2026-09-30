package strategy

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestBiggestWinner_GenerateSignals(t *testing.T) {
	s := &BiggestWinnerStrategy{}

	// Week 1: 2024-W01 (Jan 1 to Jan 5)
	// A goes from 10 to 15 (50% return)
	// B goes from 20 to 22 (10% return)

	// Week 2: 2024-W02 (Jan 8 to Jan 12)
	// Strategy should buy A on Monday (Jan 8), sell A on Friday (Jan 12)
	// A goes from 15 to 12 (-20% return)
	// B goes from 22 to 33 (50% return)

	// Week 3: 2024-W03 (Jan 15 to Jan 19)
	// Strategy should buy B on Monday (Jan 15), sell B on Friday (Jan 19)

	barsBySymbol := map[string][]models.Bar{
		"A": {
			// Week 1
			{Idx: 1, Symbol: "A", Date: "2024-01-01", Open: 10, Close: 11}, // Monday
			{Idx: 2, Symbol: "A", Date: "2024-01-05", Open: 14, Close: 15}, // Friday
			// Week 2
			{Idx: 3, Symbol: "A", Date: "2024-01-08", Open: 15, Close: 14}, // Monday
			{Idx: 4, Symbol: "A", Date: "2024-01-12", Open: 13, Close: 12}, // Friday
			// Week 3
			{Idx: 5, Symbol: "A", Date: "2024-01-15", Open: 12, Close: 12}, // Monday
			{Idx: 6, Symbol: "A", Date: "2024-01-19", Open: 12, Close: 12}, // Friday
		},
		"B": {
			// Week 1
			{Idx: 1, Symbol: "B", Date: "2024-01-01", Open: 20, Close: 21}, // Monday
			{Idx: 2, Symbol: "B", Date: "2024-01-05", Open: 21, Close: 22}, // Friday
			// Week 2
			{Idx: 3, Symbol: "B", Date: "2024-01-08", Open: 22, Close: 23}, // Monday
			{Idx: 4, Symbol: "B", Date: "2024-01-12", Open: 32, Close: 33}, // Friday
			// Week 3
			{Idx: 5, Symbol: "B", Date: "2024-01-15", Open: 33, Close: 34}, // Monday
			{Idx: 6, Symbol: "B", Date: "2024-01-19", Open: 35, Close: 36}, // Friday
		},
	}

	signals := s.GenerateSignals(barsBySymbol)

	// Expecting 4 signals:
	// 1. Buy A on 2024-01-08
	// 2. Sell A on 2024-01-12
	// 3. Buy B on 2024-01-15
	// 4. Sell B on 2024-01-19

	if len(signals) != 4 {
		t.Fatalf("Expected 4 signals, got %d", len(signals))
	}

	expectedSignals := []struct {
		Date   string
		Symbol string
		Entry  int
	}{
		{"2024-01-08", "A", 1},
		{"2024-01-12", "A", -1},
		{"2024-01-15", "B", 1},
		{"2024-01-19", "B", -1},
	}

	for i, sig := range signals {
		if sig.Date != expectedSignals[i].Date {
			t.Errorf("Signal %d date mismatch: expected %s, got %s", i, expectedSignals[i].Date, sig.Date)
		}
		if sig.Symbol != expectedSignals[i].Symbol {
			t.Errorf("Signal %d symbol mismatch: expected %s, got %s", i, expectedSignals[i].Symbol, sig.Symbol)
		}
		if sig.Entry != expectedSignals[i].Entry {
			t.Errorf("Signal %d entry mismatch: expected %d, got %d", i, expectedSignals[i].Entry, sig.Entry)
		}
	}
}
