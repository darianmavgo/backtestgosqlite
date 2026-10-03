package hold_bail_strategy

import (
	"fmt"
	"github.com/jmoiron/sqlx"
	"strconv"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

const pipelineDir = "sql/strategies/hold_bail_strategy"

type Strategy struct {
	Row          refdb.HoldBailStrategy
	marketDBPath string
	calcDBPath   string
}

func (s *Strategy) ID() string { return s.Row.ID }

// Family is the strategy family, which names its result database.
func (s *Strategy) Family() string { return "hold_bail" }

func (s *Strategy) Name() string { return s.Row.Name }

func (s *Strategy) Description() string {
	return fmt.Sprintf("Buys and holds %s with %.0f%% trailing stop. Hops back in when Close > SMA%d.",
		s.Row.Symbol, s.Row.TrailingStopPct*100, s.Row.SMAReentryPeriod)
}

func (s *Strategy) RequiredSymbols() []string { return []string{s.Row.Symbol} }

func (s *Strategy) DefaultConfig() strategy.StrategyConfig {
	return strategy.StrategyConfig{
		ID:                  s.ID(),
		Name:                s.Name(),
		Description:         s.Description(),
		Benchmark:           "SPY",
		PositionSizing:      "fixed_pct",
		AllocationPct:       s.Row.AllocationPct,
		TargetPct:           999.0, // no arbitrary take-profit; rides entire multi-month/year bull runs
		StopLossPct:         0.001, // arbitrary small stop loss just in case, but actual is trailing stop
		UseTrailingStop:     true,
		TrailingStopPct:     s.Row.TrailingStopPct,
		ReentryCooldownDays: 1,     // minimum 1-day breath after exit to prevent same-day re-whipsaw
		HoldingWindow:       99999, // indefinite hold until trailing stop
		PositionCap:         1,
		CashYieldAnnual:     s.Row.CashYield,
		SlippagePct:         s.Row.SlippagePct,
		CommissionPerShare:  0.0001,
	}
}

func (s *Strategy) Validate() error { return strategy.ValidateConfig(s.DefaultConfig()) }

func (s *Strategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

// GenerateSignals runs the hold_bail_strategy SQL pipeline: enter on the first
// bar, then on every bar whose close is above its SMA of Row.SMAReentryPeriod.
func (s *Strategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	period := s.Row.SMAReentryPeriod
	if period < 1 {
		period = 20
	}
	cfg := s.DefaultConfig()
	cfg.TradeSymbol = strings.ToUpper(strings.TrimSpace(s.Row.Symbol))
	cfg.SQLParams = map[string]string{
		"SMA_PERIOD":    strconv.Itoa(period),
		"SMA_PRECEDING": strconv.Itoa(period - 1),
	}
	return strategy.RunPipeline(s.ID(), s.Name(), s.Description(), pipelineDir, cfg, s.marketDBPath, s.calcDBPath, "market", barsBySymbol)
}

// ParameterSpace is the single point of the row. No hold_bail column is gridsearchable
// (see strategy_family_param), so a sweep runs the row once.
func (s *Strategy) ParameterSpace() strategy.ParameterSpace {
	cfg := s.DefaultConfig()
	return strategy.ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      []string{s.Row.Symbol},
		SignalSymbol: s.Row.Symbol,
		Direction:    "long",
		FixedEntries: true,
		SignalDays:   []int{1},
		HoldDays:     []int{cfg.HoldingWindow},
		TakeProfits:  []float64{0},
		StopLosses:   []float64{0},
		Regimes:      []string{"All Regimes"},
		Allocations:  []float64{s.Row.AllocationPct},
		CashYield:    s.Row.CashYield,
		Baseline: strategy.BaselineParams{
			HoldDays:   cfg.HoldingWindow,
			Allocation: s.Row.AllocationPct,
		},
	}
}

// NewFamily returns the hold_bail_strategy family reading the reference DB at path().
// Rows are not registered one by one: a row is read and built when
// strategy.Get asks for its id.
func NewFamily(path func() string) *strategy.RowFamily {
	return &strategy.RowFamily{
		FamilyName: "hold_bail",
		Table:      "hold_bail_strategy",
		Path:       path,
		Build: func(db *sqlx.DB, id string) (strategy.Strategy, bool, error) {
			row, ok, err := refdb.HoldBailStrategyByID(db, id)
			if err != nil || !ok {
				return nil, false, err
			}
			if err := (&Strategy{Row: row}).Validate(); err != nil {
				return nil, false, err
			}
			return &Strategy{Row: row}, true, nil
		},
	}
}

// Register adds the hold_bail family, reading refdb.DefaultPath. It reads no rows.
func Register() {
	RegisterFrom(refdb.DefaultPath)
}

// RegisterFrom adds the hold_bail family reading the reference DB at path,
// replacing any earlier hold_bail family (tests point it at a temp DB).
func RegisterFrom(path string) {
	strategy.RegisterFamily(NewFamily(func() string { return path }))
}
