package strategy_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/hold_bail_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/hold_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/markov_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/streak_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/tree_strategy"
)

func TestGooglStrategies(t *testing.T) {
	refdb.DefaultPath = "../../refdata/settings.db"
	strategy.AutoRegisterSQLStrategies("../../", "../../data/market_history.db")
	streak_strategy.Register()
	tree_strategy.Register()
	hold_bail_strategy.Register()
	hold_strategy.Register()
	markov_strategy.Register()

	// Define the IDs of the GOOGL strategies to test
	googlIDs := []string{
		"hold_bail_googl",
		"googl-buy-hold",
		"markov_hmm_googl",
		"streak-googl-down3-googl",
		"googl_tree",
	}

	// Generate 200 days of dummy price data for GOOGL
	barsBySymbol := make(map[string][]models.Bar)
	var bars []models.Bar
	price := 100.0
	for i := 0; i < 200; i++ {
		price = price * (1.0 + (rand.Float64()-0.5)*0.05)
		bars = append(bars, models.Bar{
			Idx:    i,
			Symbol: "GOOGL",
			Date:   time.Now().AddDate(0, 0, -200+i).Format("2006-01-02"),
			Open:   price,
			High:   price * 1.02,
			Low:    price * 0.98,
			Close:  price,
			Volume: 1000000,
		})
	}
	barsBySymbol["GOOGL"] = bars

	for _, id := range googlIDs {
		t.Run(id, func(t *testing.T) {
			s, ok := strategy.Get(id)
			if !ok {
				t.Fatalf("Strategy %q not found in registry", id)
			}
			t.Logf("Testing strategy: %s (%s)", s.ID(), s.Name())
			
			signals := s.GenerateSignals(barsBySymbol)
			t.Logf("  -> Generated %d signals", len(signals))
		})
	}
}
