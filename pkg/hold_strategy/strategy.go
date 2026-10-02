package hold_strategy

import (
	"fmt"
	"log"
	"os"
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

func Register() {
	RegisterFrom(refdb.DefaultPath)
}

func RegisterFrom(path string) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() == 0 {
		return
	}
	db, err := refdb.Open(path)
	if err != nil || db == nil {
		return
	}
	defer db.Close()
	rows, err := refdb.HoldStrategies(db)
	if err != nil {
		return
	}
	for _, row := range rows {
		s := &Strategy{Row: row}
		if err := s.Validate(); err != nil {
			log.Printf("hold_strategy: skip %s: %v", row.ID, err)
			continue
		}
		if existing, ok := strategy.Get(row.ID); ok {
			if _, is := existing.(*Strategy); !is {
				log.Printf("hold_strategy: skip %s: id already used by %T", row.ID, existing)
				continue
			}
		}
		strategy.Register(s)
	}
}
