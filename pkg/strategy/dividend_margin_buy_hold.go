package strategy

import (
	"fmt"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// DividendMarginBuyHoldStrategy buys a high dividend ETF using maximum available buying power
// on Reg-T 2:1 margin (200% allocation of starting equity = $100k base cash borrows $100k margin to buy $200k worth).
// Dividends flow directly into the cash account, while margin debit interest flows out of cash daily.
type DividendMarginBuyHoldStrategy struct {
	Symbol string
	Label  string
}

func init() {
	for _, e := range []struct{ sym, label string }{
		{"VYM", "Vanguard High Dividend Yield ETF"},
		{"SCHD", "Schwab U.S. Dividend Equity ETF"},
		{"DVY", "iShares Select Dividend ETF"},
	} {
		Register(&DividendMarginBuyHoldStrategy{Symbol: e.sym, Label: e.label})
	}
}

func (s *DividendMarginBuyHoldStrategy) ID() string {
	return strings.ToLower(s.Symbol) + "-margin-buy-hold"
}

func (s *DividendMarginBuyHoldStrategy) Name() string {
	return fmt.Sprintf("%s 2x Margin Max Buying Power Buy & Hold (%s)", s.Symbol, s.Label)
}

func (s *DividendMarginBuyHoldStrategy) Description() string {
	return fmt.Sprintf("Buys %s on the first bar using 100%% of available buying power (2:1 Reg-T margin leverage). "+
		"Starts with base capital ($100k base equity yielding $200k buying power). "+
		"Dividends arrive into the cash balance, and margin borrow interest leaves cash daily at 7.0%% APR debit rate.", s.Symbol)
}

func (s *DividendMarginBuyHoldStrategy) UsesTotalReturn() bool { return true }

func (s *DividendMarginBuyHoldStrategy) RequiredSymbols() []string { return []string{s.Symbol} }

func (s *DividendMarginBuyHoldStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                   s.ID(),
		Name:                 s.Name(),
		Description:          s.Description(),
		Benchmark:            "VOO",
		TargetPct:            999.0,  // never exit via profit target
		StopLossPct:          0.0001, // never exit via stop loss
		HoldingWindow:        99999,  // hold throughout backtest window
		PositionCap:          1,
		AllocationPct:        2.0,    // 200% allocation = 100% of 2:1 Reg-T buying power
		UseMargin:            true,   // enable Reg-T margin simulation
		MarginLeverage:       2.0,    // 2:1 buying power multiplier
		MarginInterestAnnual: 0.07,   // 7.0% APR margin borrow interest
		SlippagePct:          0.0005,
		CommissionPerShare:   0.0001,
	}
}

func (s *DividendMarginBuyHoldStrategy) Validate() error { return ValidateConfig(s.DefaultConfig()) }

func (s *DividendMarginBuyHoldStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
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

func (s *DividendMarginBuyHoldStrategy) SetDatabases(marketDBPath, calcDBPath string) {}
