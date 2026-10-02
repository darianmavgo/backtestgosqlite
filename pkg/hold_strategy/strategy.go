package hold_strategy

import (
	"fmt"
	"github.com/jmoiron/sqlx"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

type Strategy struct {
	Row refdb.HoldStrategy
}

func (s *Strategy) ID() string { return s.Row.ID }

func (s *Strategy) Name() string { return s.Row.Name }

func (s *Strategy) Description() string {
	desc := fmt.Sprintf("Buys and holds %s on the first available historical bar.", s.Row.Symbol)
	if s.Row.TotalReturn > 0 {
		desc += " Simulated on dividend-adjusted prices (total return)."
	}
	return desc
}

func (s *Strategy) RequiredSymbols() []string { return []string{s.Row.Symbol} }

func (s *Strategy) UsesTotalReturn() bool { return s.Row.TotalReturn > 0 }

func (s *Strategy) DefaultConfig() strategy.StrategyConfig {
	return strategy.StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "VOO",
		TargetPct:          999.0,  // Never exit via profit target
		StopLossPct:        0.0001, // Never exit via stop loss
		HoldingWindow:      99999,  // Never exit via time limit
		PositionCap:        1,
		AllocationPct:      s.Row.AllocationPct,
		CashYieldAnnual:    s.Row.CashYield,
		SlippagePct:        s.Row.SlippagePct,
		CommissionPerShare: 0.0001,
	}
}

func (s *Strategy) Validate() error { return strategy.ValidateConfig(s.DefaultConfig()) }

func (s *Strategy) SetDatabases(marketDBPath, calcDBPath string) {}

func (s *Strategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var bars []models.Bar
	for sym, b := range barsBySymbol {
		if strings.EqualFold(sym, s.Row.Symbol) {
			bars = b
			break
		}
	}
	if len(bars) == 0 {
		return nil
	}

	firstBar := bars[0]
	return []models.Signal{{
		Idx:        firstBar.Idx,
		Symbol:     s.Row.Symbol,
		Date:       firstBar.Date,
		Open:       firstBar.Open,
		High:       firstBar.High,
		Low:        firstBar.Low,
		Close:      firstBar.Close,
		Volume:     firstBar.Volume,
		BuyLimit:   firstBar.Close,
		OrderType:  "market",
		Entry:      1,
		Direction:  "LONG",
		StrategyID: s.ID(),
	}}
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
