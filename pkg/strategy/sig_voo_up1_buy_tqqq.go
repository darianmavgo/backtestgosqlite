package strategy

import (
	"fmt"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// SigVooUp1BuyTqqq longs TQQQ at the close of any day on which VOO both
// closed up from the previous close AND traded more volume than the previous
// session. Exits: +8% take-profit, -10% stop-loss, or the max-hold backstop.
type SigVooUp1BuyTqqq struct {
	TradeSymbol string
	TP          float64
	SL          float64
	Hold        int
}

func NewSigVooUp1BuyTqqq() *SigVooUp1BuyTqqq {
	s := &SigVooUp1BuyTqqq{TradeSymbol: "TQQQ", TP: 0.08, SL: 0.10, Hold: 10}
	Register(s)
	RegisterAlias("sigvooup1buytqqq", s)
	return s
}

func (s *SigVooUp1BuyTqqq) ID() string { return "sig-voo-up1-buy-tqqq" }

func (s *SigVooUp1BuyTqqq) Name() string { return "VOO Up+Volume → TQQQ" }

func (s *SigVooUp1BuyTqqq) Description() string {
	return fmt.Sprintf(
		"Long %s when VOO closes up vs the prior day and VOO volume is higher than the prior day. Exits: +%.0f%% TP / -%.0f%% SL / %d-day max hold.",
		s.TradeSymbol, s.TP*100, s.SL*100, s.Hold)
}

func (s *SigVooUp1BuyTqqq) Validate() error { return ValidateConfig(s.DefaultConfig()) }

func (s *SigVooUp1BuyTqqq) RequiredSymbols() []string { return []string{"VOO", s.TradeSymbol} }

// MinHistoryBars: one prior VOO bar is needed; the rest is slack for a partial/holiday bar.
func (s *SigVooUp1BuyTqqq) MinHistoryBars() int { return 5 }

func (s *SigVooUp1BuyTqqq) DefaultConfig() StrategyConfig {
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
		StopLossPct:        1.0 - s.SL, // multiplier on entry, not an offset
		HoldingWindow:      s.Hold,
		PositionCap:        1,
		CashYieldAnnual:    0.045,
		SlippagePct:        0.0005,
		CommissionPerShare: 0.0001,
	}
}

func (s *SigVooUp1BuyTqqq) SetDatabases(marketDBPath, calcDBPath string) {}

// UpVolumeUpDates returns the dates on which a signal symbol closed above the previous
// close and its volume exceeded the previous session's. Zero-volume bars (missing
// data) never qualify on either side of the comparison.
func UpVolumeUpDates(bars []models.Bar) map[string]bool {
	out := make(map[string]bool)
	for i := 1; i < len(bars); i++ {
		p, c := bars[i-1], bars[i]
		if c.Close > p.Close && p.Volume > 0 && c.Volume > p.Volume {
			d := c.Date
			if len(d) >= 10 {
				d = d[:10]
			}
			out[d] = true
		}
	}
	return out
}

func (s *SigVooUp1BuyTqqq) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	voo := barsForSymbol(barsBySymbol, "VOO")
	trade := barsForSymbol(barsBySymbol, s.TradeSymbol)
	if len(voo) < 2 || len(trade) == 0 {
		return nil
	}
	dates := UpVolumeUpDates(voo)
	var signals []models.Signal
	for _, bar := range trade {
		d := bar.Date
		if len(d) >= 10 {
			d = d[:10]
		}
		if !dates[d] || bar.Close <= 0 {
			continue
		}
		signals = append(signals, models.Signal{
			Idx: bar.Idx, Symbol: s.TradeSymbol, Date: d,
			Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, Volume: bar.Volume,
			BuyLimit:         bar.Close,
			Entry:            1,
			OrderType:        "limit",
			Direction:        "LONG",
			Regime:           "All Regimes",
			TakeProfit:       bar.Close * (1.0 + s.TP),
			StopLoss:         bar.Close * (1.0 - s.SL),
			HoldDaysOverride: s.Hold,
			AssetClass:       "equity",
			StrategyID:       s.ID(),
		})
	}
	return signals
}

// ParameterSpace lets gridsearch sweep exits around this strategy's own entry
// rule (tree_bounce mode: signals come from GenerateSignals, not a decline streak).
func (s *SigVooUp1BuyTqqq) ParameterSpace() ParameterSpace {
	cfg := s.DefaultConfig()
	return ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      []string{s.TradeSymbol},
		SignalSymbol: "VOO",
		Direction:    "tree_bounce",
		SignalDays:   []int{1},
		HoldDays:     []int{1, 2, 3, 5, 8, 10, 15, 20},
		TakeProfits:  []float64{0.03, 0.05, 0.08, 0.12, 0.15},
		StopLosses:   []float64{0.0, 0.05, 0.08, 0.10, 0.15, 0.20},
		Regimes:      []string{"All Regimes"},
		Allocations:  []float64{cfg.AllocationPct},
		CashYield:    cfg.CashYieldAnnual,
		Baseline: BaselineParams{
			SignalDays: 1,
			HoldDays:   s.Hold,
			TakeProfit: s.TP,
			StopLoss:   s.SL,
			Allocation: cfg.AllocationPct,
			Regime:     "All Regimes",
		},
	}
}

func init() { NewSigVooUp1BuyTqqq() }
