package strategy

import (
	"fmt"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// SigQqqUp1BuyTqqq buys TQQQ (3x long Nasdaq-100) at the close of any day on
// which QQQ closed up from the prior close AND on higher volume than the prior
// session, then holds it one trading day. Exit is the next close, or the +8%
// take-profit if TQQQ's high reaches it first. There is no stop-loss (the 1-day
// hold is the risk limit).
type SigQqqUp1BuyTqqq struct {
	TradeSymbol  string
	TP           float64
	Hold         int
	marketDBPath string
	calcDBPath   string
}

func NewSigQqqUp1BuyTqqq() *SigQqqUp1BuyTqqq {
	s := &SigQqqUp1BuyTqqq{TradeSymbol: "TQQQ", TP: 0.08, Hold: 1}
	Register(s)
	RegisterAlias("sigqqqup1buytqqq", s)
	return s
}

func (s *SigQqqUp1BuyTqqq) ID() string { return "sig-qqq-up1-buy-tqqq" }

func (s *SigQqqUp1BuyTqqq) Name() string { return "QQQ Up+Volume → TQQQ" }

func (s *SigQqqUp1BuyTqqq) Description() string {
	return fmt.Sprintf(
		"Long %s when QQQ closes up vs the prior day and QQQ volume is higher than the prior day. Exits: +%.0f%% TP or the next close (%d-day hold), no stop-loss.",
		s.TradeSymbol, s.TP*100, s.Hold)
}

func (s *SigQqqUp1BuyTqqq) Validate() error { return ValidateConfig(s.DefaultConfig()) }

func (s *SigQqqUp1BuyTqqq) RequiredSymbols() []string { return []string{"QQQ", s.TradeSymbol} }

func (s *SigQqqUp1BuyTqqq) MinHistoryBars() int { return 5 }

func (s *SigQqqUp1BuyTqqq) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "QQQ",
		Timeframe:          "1d",
		PositionSizing:     "fixed_pct",
		AllocationPct:      0.65,
		TargetPct:          1.0 + s.TP,
		TakeProfitPct:      s.TP,
		StopLossPct:        0.0, // 0 = no stop-loss (same convention as sig-voo-buy-tecl)
		HoldingWindow:      s.Hold,
		PositionCap:        1,
		CashYieldAnnual:    0.045,
		SlippagePct:        0.0005,
		CommissionPerShare: 0.0001,
	}
}

func (s *SigQqqUp1BuyTqqq) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *SigQqqUp1BuyTqqq) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// 1. Prefer the SQL pipeline (sql/strategies/sig_qqq_up1_buy_tqqq): see
	// sig_voo_up1_buy_tqqq.go's GenerateSignals for why.
	if s.calcDBPath != "" && s.marketDBPath != "" {
		dir := "sql/strategies/sig_qqq_up1_buy_tqqq"
		if sqlStrat, exists := Get("sig_qqq_up1_buy_tqqq-sql"); exists {
			if sp, ok := sqlStrat.(*SQLPipelineStrategy); ok {
				dir = sp.PipelineDir()
			}
		}
		pipe := NewSQLPipelineStrategy("sig-qqq-up1-buy-tqqq-pipeline", s.Name(), s.Description(), dir, s.DefaultConfig())
		pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
		return pipe.GenerateSignals(barsBySymbol)
	}

	// 2. Pure Go calculation fallback for in-memory backtesting and unit testing.
	qqq := barsForSymbol(barsBySymbol, "QQQ")
	trade := barsForSymbol(barsBySymbol, s.TradeSymbol)
	if len(qqq) < 2 || len(trade) == 0 {
		return nil
	}
	dates := UpVolumeUpDates(qqq)
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
			HoldDaysOverride: s.Hold,
			AssetClass:       "equity",
			StrategyID:       s.ID(),
		})
	}
	return signals
}

// ParameterSpace lets gridsearch sweep exits around this strategy's own entry
// rule (tree_bounce mode: signals come from GenerateSignals, not a decline streak).
func (s *SigQqqUp1BuyTqqq) ParameterSpace() ParameterSpace {
	cfg := s.DefaultConfig()
	return ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      []string{s.TradeSymbol},
		SignalSymbol: "QQQ",
		Direction:    "tree_bounce",
		SignalDays:   []int{1},
		HoldDays:     []int{1, 2, 3, 5},
		TakeProfits:  []float64{0.03, 0.05, 0.08, 0.12},
		StopLosses:   []float64{0.0, 0.05, 0.10, 0.15, 0.20},
		Regimes:      []string{"All Regimes"},
		Allocations:  []float64{cfg.AllocationPct},
		CashYield:    cfg.CashYieldAnnual,
		Baseline: BaselineParams{
			SignalDays: 1,
			HoldDays:   s.Hold,
			TakeProfit: s.TP,
			StopLoss:   0.0,
			Allocation: cfg.AllocationPct,
			Regime:     "All Regimes",
		},
	}
}

func init() { NewSigQqqUp1BuyTqqq() }
