package strategy

import (
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// VOOBuyHoldStrategy buys VOO on the first available trading day and holds it
// across the entire backtest window — the passive benchmark every active
// strategy in this repo is measured against.
//
// Renamed from the original "buy-and-hold": with the ETF universe now at 1,800+
// symbols (see cmd/etf_decision_trees), an unscoped buy-and-hold that iterated
// every symbol in whatever bars map it was handed stopped meaning "buy VOO" and
// started meaning "buy whichever ~10 symbols happen to sort first by (date,
// symbol) and fit under PositionCap" — e.g. EWA/EWC/EWD/.../SPY in one recent
// full-universe run, never VOO. That's not a vague name, it's a silently wrong
// benchmark. Fixed by scoping to VOO explicitly instead of just renaming the label.
type VOOBuyHoldStrategy struct{}

func init() {
	s := &VOOBuyHoldStrategy{}
	Register(s)
	RegisterAlias("buy-and-hold", s) // backward-compat for the old vague ID
}

func (s *VOOBuyHoldStrategy) ID() string {
	return "voo-buy-hold"
}

func (s *VOOBuyHoldStrategy) Name() string {
	return "VOO Buy & Hold (Passive S&P 500 Benchmark)"
}

func (s *VOOBuyHoldStrategy) Description() string {
	return "Buys VOO (Vanguard S&P 500 ETF) on the first available historical bar and holds it through the " +
		"entire backtest horizon, fully invested, with no stops or profit targets. The passive baseline every " +
		"active strategy in this repo should be measured against for Alpha/Beta."
}

// RequiredSymbols pins this strategy to VOO — without this, GenerateSignals
// would receive every symbol the caller happened to load and (with a
// multi-thousand-symbol universe) buy an arbitrary, non-VOO basket instead.
func (s *VOOBuyHoldStrategy) RequiredSymbols() []string {
	return []string{"VOO"}
}

func (s *VOOBuyHoldStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "VOO",
		TargetPct:          999.0,  // Never exit via profit target
		StopLossPct:        0.0001, // Never exit via stop loss
		HoldingWindow:      99999,  // Never exit via time limit
		PositionCap:        1,      // Single asset (VOO) — fully invested
		AllocationPct:      1.0,    // Fully invested, no idle cash
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *VOOBuyHoldStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *VOOBuyHoldStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var bars []models.Bar
	for sym, b := range barsBySymbol {
		if strings.EqualFold(sym, "VOO") {
			bars = b
			break
		}
	}
	if len(bars) == 0 {
		return nil
	}

	firstBar := bars[0]
	return []models.Signal{{
		Idx:       firstBar.Idx,
		Symbol:    "VOO",
		Date:      firstBar.Date,
		Open:      firstBar.Open,
		High:      firstBar.High,
		Low:       firstBar.Low,
		Close:     firstBar.Close,
		Volume:    firstBar.Volume,
		BuyLimit:  firstBar.Close,
		OrderType: "market",
		Entry:     1,
	}}
}

func (s *VOOBuyHoldStrategy) SetDatabases(marketDBPath, calcDBPath string) {}
