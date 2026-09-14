package strategy

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// ETFDecisionTreeStrategy is a generic, symbol-parameterized strategy: it fits a
// fresh CloudForest decision tree (see decisiontree.go) against its own symbol's
// bars and trades the tree's BUY predictions with a baked-in TP/SL/hold. One
// instance is registered per qualifying ETF (see etfDecisionTreeResultsFile),
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

// etfDecisionTreeResultsFile is produced by cmd/etf_decision_trees: one row per
// qualifying ETF with its best-found TP/SL/hold from the grid sweep.
// Format: symbol,tp,sl,hold (comma-separated, no header).
const etfDecisionTreeResultsFile = "data/etf_dt_strategies.csv"

func registerETFDecisionTreesFromFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // not generated yet; harmless
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "symbol,") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 4 {
			continue
		}
		symbol := strings.ToUpper(strings.TrimSpace(parts[0]))
		tp, err1 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		sl, err2 := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		hold, err3 := strconv.Atoi(strings.TrimSpace(parts[3]))
		if symbol == "" || err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		Register(&ETFDecisionTreeStrategy{Symbol: symbol, TP: tp, SL: sl, Hold: hold})
	}
}

func init() {
	registerETFDecisionTreesFromFile(etfDecisionTreeResultsFile)
}
