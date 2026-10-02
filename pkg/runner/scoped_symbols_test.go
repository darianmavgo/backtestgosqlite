package runner

import (
	"reflect"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/hold_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

func TestScopedSymbolsNarrowsDeclaredStrategies(t *testing.T) {
	a := &hold_strategy.Strategy{Row: refdb.HoldStrategy{ID: "h-a", Name: "a", Symbol: "VOO", AllocationPct: 1}}
	b := &hold_strategy.Strategy{Row: refdb.HoldStrategy{ID: "h-b", Name: "b", Symbol: "QQQ", AllocationPct: 1}}
	got, ok := ScopedSymbols([]strategy.Strategy{a, b}, "")
	if !ok || !reflect.DeepEqual(got, []string{"QQQ", "VOO"}) {
		t.Fatalf("got %v, %v", got, ok)
	}
}
