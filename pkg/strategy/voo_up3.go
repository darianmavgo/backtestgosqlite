package strategy

import (
	"fmt"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// VOOUp3Strategy longs a trade ETF the day VOO closes up for GainDays
// consecutive sessions.
type VOOUp3Strategy struct {
	TradeSymbol  string
	GainDays     int
	TP           float64
	SL           float64
	Hold         int
	marketDBPath string
	calcDBPath   string
}

func NewVOOUp3Strategy() *VOOUp3Strategy {
	s := &VOOUp3Strategy{
		TradeSymbol: "TQQQ",
		GainDays:    3,
		TP:          0.05,
		SL:          0.06,
		Hold:        8,
	}
	Register(s)
	RegisterAlias("voo-up3", s)
	RegisterAlias("voo_up3", s)
	return s
}

func (s *VOOUp3Strategy) ID() string { return "voo-up3" }

func (s *VOOUp3Strategy) Name() string {
	return fmt.Sprintf("VOO %d-Up → %s", s.GainDays, s.TradeSymbol)
}

func (s *VOOUp3Strategy) Description() string {
	return fmt.Sprintf(
		"Long %s when VOO closes up %d consecutive days. Exits: +%.0f%% TP / -%.0f%% SL / %d-day hold.",
		s.TradeSymbol, s.GainDays, s.TP*100, s.SL*100, s.Hold)
}

func (s *VOOUp3Strategy) Validate() error { return ValidateConfig(s.DefaultConfig()) }

func (s *VOOUp3Strategy) RequiredSymbols() []string {
	return []string{"VOO", s.TradeSymbol}
}

func (s *VOOUp3Strategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "VOO",
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

func (s *VOOUp3Strategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *VOOUp3Strategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	voo := barsForSymbol(barsBySymbol, "VOO")
	trade := barsForSymbol(barsBySymbol, s.TradeSymbol)
	if len(voo) < s.GainDays+1 || len(trade) < 2 {
		return nil
	}
	days := s.GainDays
	if days <= 0 {
		days = 3
	}
	return VOOUpStreakSignals(s.TradeSymbol, voo, trade, days, s.TP, s.SL, s.Hold)
}

func (s *VOOUp3Strategy) ParameterSpace() ParameterSpace {
	cfg := s.DefaultConfig()
	return ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      []string{s.TradeSymbol},
		SignalSymbol: "VOO",
		Direction:    "rally",
		SignalDays:   []int{2, 3, 4, 5},
		HoldDays:     []int{2, 5, 8, 12, 15},
		TakeProfits:  []float64{0.03, 0.05, 0.08, 0.12},
		StopLosses:   []float64{0.02, 0.04, 0.06, 0.08},
		Regimes:      []string{"All Regimes"},
		Allocations:  []float64{cfg.AllocationPct},
		CashYield:    cfg.CashYieldAnnual,
		Baseline: BaselineParams{
			SignalDays: s.GainDays,
			HoldDays:   s.Hold,
			TakeProfit: s.TP,
			StopLoss:   s.SL,
			Allocation: cfg.AllocationPct,
			Regime:     "All Regimes",
		},
	}
}

// VOOUpStreakDates returns dates on which VOO has closed up `streak` days in a row.
func VOOUpStreakDates(voo []models.Bar, streak int) []string {
	if streak < 1 || len(voo) < streak+1 {
		return nil
	}
	var out []string
	up := 0
	for i := 1; i < len(voo); i++ {
		if voo[i].Close > voo[i-1].Close {
			up++
		} else {
			up = 0
		}
		if up >= streak {
			d := voo[i].Date
			if len(d) >= 10 {
				d = d[:10]
			}
			out = append(out, d)
		}
	}
	return out
}

// VOOUpStreakSignals longs tradeSymbol on VOO up-streak dates, using that
// symbol's close as the limit and the given TP/SL/hold.
func VOOUpStreakSignals(tradeSymbol string, voo, trade []models.Bar, streak int, tpPct, slPct float64, holdDays int) []models.Signal {
	upDates := make(map[string]bool, 64)
	for _, d := range VOOUpStreakDates(voo, streak) {
		upDates[d] = true
	}
	if len(upDates) == 0 {
		return nil
	}
	var signals []models.Signal
	for _, bar := range trade {
		d := bar.Date
		if len(d) >= 10 {
			d = d[:10]
		}
		if !upDates[d] || bar.Close <= 0 {
			continue
		}
		var takeProfit, stopLoss float64
		if tpPct > 0 {
			takeProfit = bar.Close * (1.0 + tpPct)
		}
		if slPct > 0 {
			stopLoss = bar.Close * (1.0 - slPct)
		}
		signals = append(signals, models.Signal{
			Idx:              bar.Idx,
			Symbol:           tradeSymbol,
			Date:             d,
			Open:             bar.Open,
			High:             bar.High,
			Low:              bar.Low,
			Close:            bar.Close,
			Volume:           bar.Volume,
			BuyLimit:         bar.Close,
			Entry:            1,
			OrderType:        "limit",
			Direction:        "LONG",
			Regime:           "All Regimes",
			TakeProfit:       takeProfit,
			StopLoss:         stopLoss,
			HoldDaysOverride: holdDays,
			AssetClass:       "equity",
			StrategyID:       "voo-up3",
			Priority:         0,
		})
	}
	return signals
}

func barsForSymbol(barsBySymbol map[string][]models.Bar, want string) []models.Bar {
	for sym, b := range barsBySymbol {
		if strings.EqualFold(sym, want) {
			return b
		}
	}
	return nil
}

func init() {
	NewVOOUp3Strategy()
}
