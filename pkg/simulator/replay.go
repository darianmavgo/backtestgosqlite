package simulator

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

// Round-trip outcomes from ReplayRoundTrip. CLOSED is a simulator exit
// (target, stop, or time). OPEN means the entry filled and the bars ended
// before a strategy exit. UNFILLED means the next session never traded the limit.
const (
	RoundTripClosed   = "CLOSED"
	RoundTripOpen     = "OPEN"
	RoundTripUnfilled = "UNFILLED"
	RoundTripNoBars   = "NO_BARS"
)

// RoundTrip is what the portfolio simulator does with one stored signal.
// Trade.ReturnPct is a fraction (0.05 is five percent). On OPEN, Trade carries
// the entry and no strategy exit. On UNFILLED, Trade is empty.
type RoundTrip struct {
	Outcome string
	Trade   models.Trade
}

// ReplayRoundTrip runs one signal through the same entry model and exit rules
// as PortfolioSimulator, sized to a single share so the return does not depend
// on account equity. Bars may use full timestamps; dates are compared on the
// first ten characters. A position still open when the bars run out is OPEN,
// not an end-of-data exit.
func ReplayRoundTrip(sig models.Signal, bars []models.Bar, cfg strategy.StrategyConfig) RoundTrip {
	if len(bars) == 0 || sig.Symbol == "" {
		return RoundTrip{Outcome: RoundTripNoBars}
	}
	norm := make([]models.Bar, len(bars))
	dates := make([]string, len(bars))
	for i, b := range bars {
		if len(b.Date) >= 10 {
			b.Date = b.Date[:10]
		}
		norm[i] = b
		dates[i] = b.Date
	}
	if len(sig.Date) >= 10 {
		sig.Date = sig.Date[:10]
	}
	cfg.PositionSizing = "fixed_shares"
	cfg.FixedShares = 1
	if cfg.PositionCap < 1 {
		cfg.PositionCap = 1
	}
	sim := NewPortfolioSimulator(cfg, 1_000_000)
	_, trades, _ := sim.Run([]models.Signal{sig}, map[string][]models.Bar{sig.Symbol: norm}, dates)
	if len(trades) == 0 {
		if len(sim.Positions) == 0 {
			return RoundTrip{Outcome: RoundTripUnfilled}
		}
		pos := sim.Positions[sig.Symbol]
		rt := RoundTrip{Outcome: RoundTripOpen}
		if pos != nil {
			rt.Trade.Symbol = pos.Symbol
			rt.Trade.EntryDate = pos.EntryDate
			rt.Trade.EntryPrice = pos.EntryPrice
		}
		return rt
	}
	tr := trades[0]
	if tr.ExitReason == models.ExitReasonEndBacktest {
		return RoundTrip{
			Outcome: RoundTripOpen,
			Trade:   models.Trade{Symbol: tr.Symbol, EntryDate: tr.EntryDate, EntryPrice: tr.EntryPrice},
		}
	}
	return RoundTrip{Outcome: RoundTripClosed, Trade: tr}
}
