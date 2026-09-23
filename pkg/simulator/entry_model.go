package simulator

import (
	"sort"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

// CrisisStopFraction is the protective stop the live pipeline attaches when a
// strategy has none (entry * 0.80). It must equal schwaber's
// trader.CrisisStopFraction so a backtest of a stop-less strategy sees the
// same stop the live bracket order carries.
const CrisisStopFraction = 0.80

// ApplyLiveEntryModel rewrites the entry signals of strategies whose config
// sets NextDayLimitEntry so the simulators enter the way the live pipeline
// does, instead of at the signal bar's close (a price that has already passed
// by the time a signal computed from that close can be acted on).
//
// For a signal on day D with limit price L (BuyLimit, else Close):
//   - the order is live on the next bar D+1; if that bar's Low never reaches L
//     the order does not fill and the signal is dropped;
//   - otherwise it fills at L, or at the open when the open is already below L;
//   - the signal moves to date D+1 with that fill price;
//   - take-profit and stop are absolute prices anchored to L (not to the
//     fill), exactly as the staged bracket order is. A strategy with no stop
//     gets the live crisis stop.
//
// Known simplification, shared with the legacy model: a position is not
// exited on its entry day, so a stop or target touched on the fill day itself
// is not seen. Strategies with the flag off pass through unchanged.
func ApplyLiveEntryModel(
	signals []models.Signal,
	barsBySymbol map[string][]models.Bar,
	cfgFor func(models.Signal) strategy.StrategyConfig,
) []models.Signal {
	out := make([]models.Signal, 0, len(signals))
	for _, sig := range signals {
		cfg := cfgFor(sig)
		if !cfg.NextDayLimitEntry {
			out = append(out, sig)
			continue
		}
		limit := sig.BuyLimit
		if limit <= 0 {
			limit = sig.Close
		}
		if limit <= 0 {
			continue
		}
		next, ok := nextBarAfter(barsBySymbol[sig.Symbol], sig.Date)
		if !ok || next.Low > limit {
			continue // no next session in the data, or the limit never traded
		}
		fill := limit
		if next.Open > 0 && next.Open < limit {
			fill = next.Open
		}

		if sig.TakeProfit <= 0 {
			switch {
			case cfg.TakeProfitPct > 0:
				sig.TakeProfit = limit * (1 + cfg.TakeProfitPct)
			case cfg.TargetPct > 1:
				sig.TakeProfit = limit * cfg.TargetPct
			}
		}
		if sig.StopLoss <= 0 {
			if cfg.StopLossPct > 0 && cfg.StopLossPct < 1 {
				sig.StopLoss = limit * cfg.StopLossPct
			} else {
				sig.StopLoss = limit * CrisisStopFraction
			}
		}

		sig.Date = next.Date
		sig.Close = fill
		sig.BuyLimit = fill
		sig.OrderType = "limit"
		out = append(out, sig)
	}
	return out
}

// nextBarAfter returns the first bar dated strictly after date.
func nextBarAfter(bars []models.Bar, date string) (models.Bar, bool) {
	i := sort.Search(len(bars), func(i int) bool { return bars[i].Date > date })
	if i >= len(bars) {
		return models.Bar{}, false
	}
	return bars[i], true
}
