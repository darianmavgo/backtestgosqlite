package strategy

import (
	"fmt"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// DividendBuyHoldStrategy buys one dividend ETF on its first bar in the
// backtest window and holds to the end, fully invested. Unlike VOOBuyHold it
// is simulated on dividend-adjusted prices (see TotalReturnProvider), so the
// result is capital gains plus reinvested dividends, and the run also reports
// how much of the return each contributed.
type DividendBuyHoldStrategy struct {
	Symbol string
	Label  string
}

func init() {
	for _, e := range []struct{ sym, label string }{
		{"SCHD", "Schwab U.S. Dividend Equity ETF"},
		{"VYM", "Vanguard High Dividend Yield ETF"},
		{"DVY", "iShares Select Dividend ETF"},
	} {
		Register(&DividendBuyHoldStrategy{Symbol: e.sym, Label: e.label})
	}
}

func (s *DividendBuyHoldStrategy) ID() string { return strings.ToLower(s.Symbol) + "-buy-hold" }

func (s *DividendBuyHoldStrategy) Name() string {
	return fmt.Sprintf("%s Buy & Hold (%s, total return)", s.Symbol, s.Label)
}

func (s *DividendBuyHoldStrategy) Description() string {
	return fmt.Sprintf("Buys %s on the first bar of the backtest window and holds it fully invested with no stops or targets. "+
		"Results include capital gains AND dividends (reinvested by default; pass backtest -no-reinvest-dividends to hold them as cash); the run also prints the split. "+
		"Benchmarked to VOO on the same adjusted basis.", s.Symbol)
}

func (s *DividendBuyHoldStrategy) UsesTotalReturn() bool { return true }

func (s *DividendBuyHoldStrategy) RequiredSymbols() []string { return []string{s.Symbol} }

func (s *DividendBuyHoldStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "VOO",
		TargetPct:          999.0,  // never exit via profit target
		StopLossPct:        0.0001, // never exit via stop loss
		HoldingWindow:      99999,  // never exit via time limit
		PositionCap:        1,
		AllocationPct:      1.0,
		SlippagePct:        0.0005,
		CommissionPerShare: 0.0001,
	}
}

func (s *DividendBuyHoldStrategy) Validate() error { return ValidateConfig(s.DefaultConfig()) }

func (s *DividendBuyHoldStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	for sym, bars := range barsBySymbol {
		if !strings.EqualFold(sym, s.Symbol) || len(bars) == 0 {
			continue
		}
		b := bars[0]
		return []models.Signal{{
			Idx: b.Idx, Symbol: s.Symbol, Date: b.Date,
			Open: b.Open, High: b.High, Low: b.Low, Close: b.Close, Volume: b.Volume,
			BuyLimit: b.Close, OrderType: "market", Entry: 1, StrategyID: s.ID(),
		}}
	}
	return nil
}

func (s *DividendBuyHoldStrategy) SetDatabases(marketDBPath, calcDBPath string) {}
