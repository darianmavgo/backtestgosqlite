package strategy_test

import (
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/runner"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/hold_bail_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/hold_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/markov_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/streak_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/tree_strategy"
)

func TestDBStrategies(t *testing.T) {
	// 1. Register all strategies
	refdb.DefaultPath = "../../refdata/strategies.db"
	strategy.AutoRegisterSQLStrategies("../../", "../../data/market_history.db")
	streak_strategy.Register()
	tree_strategy.Register()
	hold_bail_strategy.Register()
	hold_strategy.Register()
	markov_strategy.Register()

	// 2. Group all strategies by their underlying type
	strategiesByType := make(map[string][]strategy.Strategy)
	for _, s := range strategy.List() {
		// Get underlying type name (e.g. "streak_strategy.Strategy")
		typ := reflect.TypeOf(s)
		if typ.Kind() == reflect.Ptr {
			typ = typ.Elem()
		}
		typeName := typ.PkgPath() + "." + typ.Name()
		strategiesByType[typeName] = append(strategiesByType[typeName], s)
	}

	// 3. For each type, pick up to 5 and test GenerateSignals
	for typeName, list := range strategiesByType {
		t.Run(typeName, func(t *testing.T) {
			count := 0
			for _, s := range list {
				if count >= 5 {
					break
				}
				count++
				t.Logf("Testing strategy: %s (%s)", s.ID(), s.Name())
				
				// Generate dummy data
				barsBySymbol := make(map[string][]models.Bar)
				for _, reqSym := range runner.RequiredSymbolsFor([]strategy.Strategy{s}, "") {
					if reqSym == "" {
						continue
					}
					var bars []models.Bar
					price := 100.0
					for i := 0; i < 200; i++ {
						price = price * (1.0 + (rand.Float64()-0.5)*0.05)
						bars = append(bars, models.Bar{
							Idx:    i,
							Symbol: reqSym,
							Date:   time.Now().AddDate(0, 0, -200+i).Format("2006-01-02"),
							Open:   price,
							High:   price * 1.02,
							Low:    price * 0.98,
							Close:  price,
							Volume: 1000000,
						})
					}
					barsBySymbol[reqSym] = bars
				}
				
				// Execute to ensure no panic and valid signals
				// Some strategies like markov_strategy might return 0 signals with dummy data, which is fine.
				// We mainly want to ensure the DB config matches the schema and it doesn't break during evaluation.
				signals := s.GenerateSignals(barsBySymbol)
				t.Logf("  -> Generated %d signals", len(signals))
			}
		})
	}
}
