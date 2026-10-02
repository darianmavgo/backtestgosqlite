// Package stratreg registers every strategy source (SQL pipelines plus the
// refdata-driven families) so any command resolves the same ids as backtest.
package stratreg

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/hold_bail_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/hold_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/markov_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/streak_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/tree_strategy"
)

// RegisterAll registers SQL pipelines found under root/sql/strategies (calc
// databases at db) and the refdata families. It mirrors the sequence in
// pkg/backtest.
func RegisterAll(root, db string) {
	strategy.AutoRegisterSQLStrategies(root, db)
	RegisterFamilies()
}

// RegisterFamilies registers the row-backed families (streak, tree, hold_bail,
// hold, markov). It reads no rows: a member is built when strategy.Get asks.
func RegisterFamilies() {
	streak_strategy.Register()
	tree_strategy.Register()
	hold_bail_strategy.Register()
	hold_strategy.Register()
	markov_strategy.Register()
}
