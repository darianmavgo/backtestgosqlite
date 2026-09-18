package strategy

import (
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
)

// ETFDecisionTreeStrategy is a generic, symbol-parameterized strategy: it fits a
// fresh CloudForest decision tree (see decisiontree.go) against its own symbol's
// bars and trades the tree's BUY predictions with a baked-in TP/SL/hold. One
// instance is registered per qualifying ETF (see registerETFDecisionTrees),
// avoiding a hand-written Go file per ticker.
type ETFDecisionTreeStrategy struct {
	Symbol string
	TP     float64
	SL     float64
	Hold   int
}

func (s *ETFDecisionTreeStrategy) ID() string { return "dt_" + strings.ToLower(s.Symbol) }

func (s *ETFDecisionTreeStrategy) Name() string {
	return s.Symbol + " Decision Tree (CloudForest, auto-fit)"
}

func (s *ETFDecisionTreeStrategy) Description() string {
	return "Long " + s.Symbol + " on a CloudForest decision tree fit against its own price history " +
		"(same feature set/architecture reverse-engineered for MARA), trading BUY-predicted days."
}

func (s *ETFDecisionTreeStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *ETFDecisionTreeStrategy) RequiredSymbols() []string {
	return []string{s.Symbol}
}

func (s *ETFDecisionTreeStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          s.Symbol,
		Timeframe:          "1d",
		PositionSizing:     "fixed_pct",
		AllocationPct:      0.65,
		TargetPct:          1.0 + s.TP,
		TakeProfitPct:      s.TP,
		StopLossPct:        1.0 - s.SL,
		HoldingWindow:      s.Hold,
		PositionCap:        1,
		CashYieldAnnual:    0.045,
		SlippagePct:        0.0005,
		CommissionPerShare: 0.0001,
	}
}

func (s *ETFDecisionTreeStrategy) SetDatabases(marketDBPath, calcDBPath string) {}

func (s *ETFDecisionTreeStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var bars []models.Bar
	var ok bool
	for sym, b := range barsBySymbol {
		if strings.EqualFold(sym, s.Symbol) {
			bars = b
			ok = true
			break
		}
	}
	if !ok {
		return nil
	}
	sigs, err := DecisionTreeSignals(s.Symbol, bars, s.TP, s.SL, s.Hold)
	if err != nil {
		return nil
	}
	return sigs
}

// registerETFDecisionTrees registers one ETFDecisionTreeStrategy per row of the
// reference DB's etf_dt_strategies table (written by cmd/etf_decision_trees).
// A missing/empty reference DB is harmless: nothing is registered.
func registerETFDecisionTrees() {
	for _, r := range loadDTStrategies() {
		Register(&ETFDecisionTreeStrategy{Symbol: strings.ToUpper(r.Symbol), TP: r.TP, SL: r.SL, Hold: r.Hold})
	}
}

func loadDTStrategies() []refdb.DTStrategy {
	db, err := refdb.OpenExisting(refdb.DefaultPath)
	if err != nil || db == nil {
		return nil
	}
	defer db.Close()
	rows, _ := refdb.DTStrategies(db) // no table yet = none
	return rows
}

func init() {
	registerETFDecisionTrees()
}

// RankedETFDecisionTreeIDs returns dt_<symbol> IDs in recorded score order
// (highest first). limit <= 0 returns the full ranked list.
// Used by stack-eval to overlay only the strongest auto-fit trees instead
// of all 500+ registered dt_* strategies.
func RankedETFDecisionTreeIDs(limit int) []string {
	var ids []string
	for _, r := range loadDTStrategies() {
		ids = append(ids, "dt_"+strings.ToLower(r.Symbol))
		if limit > 0 && len(ids) >= limit {
			break
		}
	}
	return ids
}
