package tree_strategy

import (
	"fmt"
	"github.com/jmoiron/sqlx"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

const pipelineDir = "sql/strategies/tree_strategy"

type Strategy struct {
	Row          refdb.TreeStrategy
	marketDBPath string
	calcDBPath   string
	PipelineDir  string
}

func (s *Strategy) ID() string { return s.Row.ID }

func (s *Strategy) Name() string { return s.Row.Name }

func (s *Strategy) Description() string {
	return fmt.Sprintf("Decision Tree %s on %s. Coil <= %.2f, Bounce [%.2f, %.2f]. Hold %dd, TP +%.1f%%, SL -%.1f%%.",
		s.Row.Name, s.Row.SignalSymbol, s.Row.CoilRangeMax, s.Row.SMABounceMin, s.Row.SMABounceMax,
		s.Row.HoldDays, s.Row.TakeProfitPct*100, s.Row.StopLossPct*100)
}

func (s *Strategy) Validate() error { return ValidateRow(s.Row) }

func (s *Strategy) RequiredSymbols() []string {
	return []string{s.Row.SignalSymbol}
}

func (s *Strategy) MinHistoryBars() int {
	return 203 // 200 SMA + some slack
}

func (s *Strategy) DefaultConfig() strategy.StrategyConfig {
	tp := s.Row.TakeProfitPct
	var target, stop float64
	if tp > 0 {
		target = 1 + tp
	}
	if s.Row.StopLossPct > 0 {
		stop = 1 - s.Row.StopLossPct
	}
	return strategy.StrategyConfig{
		ID:                s.ID(),
		Name:              s.Name(),
		Description:       s.Description(),
		Benchmark:         s.Row.SignalSymbol,
		TradeSymbol:       s.Row.TradeSymbol,
		Timeframe:         "1d",
		PositionSizing:    "fixed_pct",
		AllocationPct:     s.Row.AllocationPct,
		TargetPct:         target,
		TakeProfitPct:     tp,
		StopLossPct:       stop,
		HoldingWindow:     s.Row.HoldDays,
		PositionCap:       1,
		CashYieldAnnual:   s.Row.CashYield,
		SlippagePct:       s.Row.SlippagePct,
		NextDayLimitEntry: s.Row.NextDayLimit != 0,

		// Tree-specific parameters mapped to the generic config
		TreeCoilMax: s.Row.CoilRangeMax,
		TreeSMAMin:  s.Row.SMABounceMin,
		TreeSMAMax:  s.Row.SMABounceMax,
	}
}

// ParameterSpace is the exit grid gridsearch tries: hold, take-profit and stop,
// from strategy_family_param, with the row's values included. The entries are
// the row's own coil and bounce rule, run once, so only exits are searched.
func (s *Strategy) ParameterSpace() strategy.ParameterSpace {
	cfg := s.DefaultConfig()
	ax := strategy.LoadFamilyAxes("tree")
	return strategy.ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      []string{cfg.TradeSymbol},
		SignalSymbol: cfg.Benchmark,
		Direction:    s.Row.Direction,
		FixedEntries: true,
		SignalDays:   []int{1},
		HoldDays:     strategy.UnionInts(ax.Ints("hold_days"), []int{s.Row.HoldDays}),
		TakeProfits:  strategy.UnionFloats(ax.Nums["take_profit_pct"], []float64{s.Row.TakeProfitPct}),
		StopLosses:   strategy.UnionFloats(ax.Nums["stop_loss_pct"], []float64{s.Row.StopLossPct}),
		Regimes:      []string{"All Regimes"},
		Allocations:  []float64{s.Row.AllocationPct},
		CashYield:    s.Row.CashYield,
		Baseline: strategy.BaselineParams{
			HoldDays:   s.Row.HoldDays,
			TakeProfit: s.Row.TakeProfitPct,
			StopLoss:   s.Row.StopLossPct,
			Allocation: s.Row.AllocationPct,
		},
	}
}

func (s *Strategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

// GenerateSignals runs the shared SQL pipeline (sql/strategies/tree_strategy).
func (s *Strategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	dir := s.PipelineDir
	if dir == "" {
		dir = pipelineDir
	}
	return strategy.RunPipeline(s.ID(), s.Name(), s.Description(), dir, s.DefaultConfig(), s.marketDBPath, s.calcDBPath, "limit", barsBySymbol)
}

func ValidateRow(row refdb.TreeStrategy) error {
	if strings.TrimSpace(row.ID) == "" {
		return fmt.Errorf("id is empty")
	}
	if row.HoldDays < 1 {
		return fmt.Errorf("hold_days %d", row.HoldDays)
	}
	return nil
}

// NewFamily returns the tree_strategy family reading the reference DB at path().
// Rows are not registered one by one: a row is read and built when
// strategy.Get asks for its id.
func NewFamily(path func() string) *strategy.RowFamily {
	return &strategy.RowFamily{
		FamilyName: "tree",
		Table:      "tree_strategy",
		Path:       path,
		Build: func(db *sqlx.DB, id string) (strategy.Strategy, bool, error) {
			row, ok, err := refdb.TreeStrategyByID(db, id)
			if err != nil || !ok {
				return nil, false, err
			}
			if err := ValidateRow(row); err != nil {
				return nil, false, err
			}
			return &Strategy{Row: row}, true, nil
		},
	}
}

// Register adds the tree family, reading refdb.DefaultPath. It reads no rows.
func Register() {
	RegisterFrom(refdb.DefaultPath)
}

// RegisterFrom adds the tree family reading the reference DB at path,
// replacing any earlier tree family (tests point it at a temp DB).
func RegisterFrom(path string) {
	strategy.RegisterFamily(NewFamily(func() string { return path }))
}
