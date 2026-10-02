package hold_bail_strategy

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
	Row refdb.HoldBailStrategy
}

func (s *Strategy) ID() string { return s.Row.ID }

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
		ReentryCooldownDays: 1, // minimum 1-day breath after exit to prevent same-day re-whipsaw
		HoldingWindow:       99999, // indefinite hold until trailing stop
		PositionCap:         1,
		CashYieldAnnual:     s.Row.CashYield,
		SlippagePct:         s.Row.SlippagePct,
		CommissionPerShare:  0.0001,
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

	smaPeriod := s.Row.SMAReentryPeriod
	if smaPeriod < 1 {
		smaPeriod = 20
	}
	sma := strategy.CalcSMA(bars, smaPeriod)

	signals := make([]models.Signal, 0, len(bars))
	for i, b := range bars {
		d := b.Date
		if len(d) >= 10 {
			d = d[:10]
		}

		canEnter := (i == 0)
		if i >= smaPeriod && b.Close > sma[i] {
			canEnter = true
		}

		if canEnter {
			signals = append(signals, models.Signal{
				Idx:        b.Idx,
				Symbol:     s.Row.Symbol,
				Date:       d,
				Open:       b.Open,
				High:       b.High,
				Low:        b.Low,
				Close:      b.Close,
				Volume:     b.Volume,
				BuyLimit:   b.Close,
				OrderType:  "market",
				Entry:      1,
				Direction:  "LONG",
				Regime:     fmt.Sprintf("Close>SMA%d", smaPeriod),
				AssetClass: "equity",
				StrategyID: s.ID(),
			})
		}
	}
	return signals
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
	rows, err := refdb.HoldBailStrategies(db)
	if err != nil {
		return
	}
	for _, row := range rows {
		s := &Strategy{Row: row}
		if err := s.Validate(); err != nil {
			log.Printf("hold_bail_strategy: skip %s: %v", row.ID, err)
			continue
		}
		if existing, ok := strategy.Get(row.ID); ok {
			if _, is := existing.(*Strategy); !is {
				log.Printf("hold_bail_strategy: skip %s: id already used by %T", row.ID, existing)
				continue
			}
		}
		strategy.Register(s)
	}
}
