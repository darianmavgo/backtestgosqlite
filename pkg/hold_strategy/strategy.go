package hold_strategy

import (
	"fmt"
	"github.com/jmoiron/sqlx"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

const pipelineDir = "sql/strategies/hold_strategy"

type Strategy struct {
	Row          refdb.HoldStrategy
	marketDBPath string
	calcDBPath   string
	reinvest     bool
}

func (s *Strategy) ID() string { return s.Row.ID }

// Family is the strategy family, which names its result database.
func (s *Strategy) Family() string { return "hold" }

func (s *Strategy) Name() string { return s.Row.Name }

func (s *Strategy) Description() string {
	desc := fmt.Sprintf("Buys and holds %s on the first available historical bar.", s.Row.Symbol)
	if s.Row.TrailingStopPct > 0 {
		desc = fmt.Sprintf("Buys and holds %s with %.0f%% trailing stop.", s.Row.Symbol, s.Row.TrailingStopPct*100)
		if s.Row.SMAReentryPeriod > 0 {
			desc += fmt.Sprintf(" Hops back in when Close > SMA%d.", s.Row.SMAReentryPeriod)
		}
	}
	if s.Row.TotalReturn > 0 {
		desc += " Simulated on dividend-adjusted prices (total return)."
	}
	return desc
}

func (s *Strategy) RequiredSymbols() []string { return []string{s.Row.Symbol} }

func (s *Strategy) UsesTotalReturn() bool { return s.Row.TotalReturn > 0 }

func (s *Strategy) DefaultConfig() strategy.StrategyConfig {
	cfg := strategy.StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "VOO",
		TradeSymbol:        strings.ToUpper(strings.TrimSpace(s.Row.Symbol)),
		TargetPct:          999.0,  // Never exit via profit target
		StopLossPct:        0.0001, // Never exit via stop loss
		HoldingWindow:      99999,  // Never exit via time limit
		PositionCap:        1,
		AllocationPct:      s.Row.AllocationPct,
		CashYieldAnnual:    s.Row.CashYield,
		SlippagePct:        s.Row.SlippagePct,
		CommissionPerShare: 0.0001,
	}
	if s.Row.TrailingStopPct > 0 {
		// A bailing row keeps the setup of the old hold_bail family so its results do not move.
		cfg.Benchmark = "SPY"
		cfg.PositionSizing = "fixed_pct"
		cfg.StopLossPct = 0.001
		cfg.UseTrailingStop = true
		cfg.TrailingStopPct = s.Row.TrailingStopPct
		cfg.ReentryCooldownDays = 1 // a 1-day breath after an exit prevents a same-day re-whipsaw
	}
	return cfg
}

// PipelineDir is the directory of .sql files that calculates this strategy's
// signals; `backtest stale` compares its newest file to a result's time.
func (s *Strategy) PipelineDir() string { return filepath.Join(appenv.Folder(), pipelineDir) }

func (s *Strategy) Validate() error { return strategy.ValidateConfig(s.DefaultConfig()) }

func (s *Strategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

// GenerateSignals runs the hold_strategy pipeline: an entry on the first bar of
// the run and, when Row.SMAReentryPeriod is set, on every bar closing above that
// average. Row.TrailingStopPct, when set, is the simulator's exit.
func (s *Strategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	cfg := s.DefaultConfig()
	period := max(s.Row.SMAReentryPeriod, 0)
	cfg.SQLParams = map[string]string{
		"TOTAL_RETURN":  totalReturnFlag(s.Row.TotalReturn > 0 && s.reinvest),
		"SMA_PERIOD":    strconv.Itoa(period),
		"SMA_PRECEDING": strconv.Itoa(max(period-1, 0)),
	}
	return strategy.RunPipeline(s.ID(), s.Name(), s.Description(), pipelineDir, cfg, s.marketDBPath, s.calcDBPath, "market", barsBySymbol)
}

// SetReinvestDividends is called by the runner before GenerateSignals: a total
// return run simulates on dividend-adjusted prices only when dividends are reinvested.
func (s *Strategy) SetReinvestDividends(reinvest bool) { s.reinvest = reinvest }

func totalReturnFlag(on bool) string {
	if on {
		return "1"
	}
	return "0"
}

// ParameterSpace is the single point of the row. No hold column is gridsearchable
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

// NewFamily returns the hold_strategy family reading the reference DB at path().
// Rows are not registered one by one: a row is read and built when
// strategy.Get asks for its id.
func NewFamily(path func() string) *strategy.RowFamily {
	return &strategy.RowFamily{
		FamilyName: "hold",
		Table:      "hold_strategy",
		Path:       path,
		Build: func(db *sqlx.DB, id string) (strategy.Strategy, bool, error) {
			row, ok, err := refdb.HoldStrategyByID(db, id)
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

// Register adds the hold family, reading refdb.DefaultPath. It reads no rows.
func Register() {
	RegisterFrom(refdb.DefaultPath)
}

// RegisterFrom adds the hold family reading the reference DB at path,
// replacing any earlier hold family (tests point it at a temp DB).
func RegisterFrom(path string) {
	strategy.RegisterFamily(NewFamily(func() string { return path }))
}
