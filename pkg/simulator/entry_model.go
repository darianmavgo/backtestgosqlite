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
// A strategy with NextDayOpenEntry buys at the next open instead (openEntry).
//
// Known simplification, shared with the legacy model: a position is not
// exited on its entry day, so a stop or target touched on the fill day itself
// is not seen. Strategies with the flag off pass through unchanged.
func ApplyLiveEntryModel(
	signals []models.Signal,
	barsBySymbol map[string][]models.Bar,
	cfgFor func(models.Signal) strategy.StrategyConfig,
) []models.Signal {
	return ApplyLiveEntryModelIntraday(signals, barsBySymbol, nil, cfgFor)
}

// ApplyLiveEntryModelIntraday is ApplyLiveEntryModel with intraday bars (see
// Intraday). A limit entry on a session the intraday bars cover is filled and
// judged on them; elsewhere it falls back to the daily bar.
func ApplyLiveEntryModelIntraday(
	signals []models.Signal,
	barsBySymbol map[string][]models.Bar,
	intraday Intraday,
	cfgFor func(models.Signal) strategy.StrategyConfig,
) []models.Signal {
	out := make([]models.Signal, 0, len(signals))
	for _, sig := range signals {
		cfg := cfgFor(sig)
		if cfg.EntryLimitPct > 0 && sig.Entry > 0 {
			if limited, ok := limitEntry(sig, cfg, barsBySymbol[sig.Symbol], intraday[sig.Symbol]); ok {
				out = append(out, limited)
			}
			continue
		}
		if cfg.NextDayOpenEntry {
			if open, ok := openEntry(sig, cfg, barsBySymbol[sig.Symbol]); ok {
				out = append(out, open)
			}
			continue
		}
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

// openEntry moves a signal on day D to a market buy at the open of the next
// session. The fill is that open, so take-profit (the row's percent, or an
// absolute price the signal carries) is measured from the fill and not from the
// signal bar's close. The stop is left as the signal set it, or, with none, the
// config's multiplier or the live crisis stop, both from the fill. A signal with
// no next session in the data, or a next bar with no open, is dropped.
func openEntry(sig models.Signal, cfg strategy.StrategyConfig, bars []models.Bar) (models.Signal, bool) {
	next, ok := nextBarAfter(bars, sig.Date)
	if !ok || next.Open <= 0 {
		return sig, false
	}
	fill := next.Open
	if sig.TakeProfit <= 0 {
		switch {
		case cfg.TakeProfitPct > 0:
			sig.TakeProfit = fill * (1 + cfg.TakeProfitPct)
		case cfg.TargetPct > 1:
			sig.TakeProfit = fill * cfg.TargetPct
		}
	}
	if sig.StopLoss <= 0 {
		if cfg.StopLossPct > 0 && cfg.StopLossPct < 1 {
			sig.StopLoss = fill * cfg.StopLossPct
		} else {
			sig.StopLoss = fill * CrisisStopFraction
		}
	}
	sig.Date = next.Date
	sig.Close = fill
	sig.BuyLimit = fill
	sig.OrderType = "market"
	return sig, true
}

// limitEntry turns an entry signal on session D into a buy limit at
// EntryLimitPct of the previous session's close, live on D itself. It fills if
// D's low reaches the limit (at the limit, or at the open when the open is
// already below it) and is dropped otherwise, and so is a signal on a
// symbol's first bar, which has no previous close. Take-profit and stop are left
// to the config percentages, applied to the booked entry.
func limitEntry(sig models.Signal, cfg strategy.StrategyConfig, bars []models.Bar, hourly []models.Bar) (models.Signal, bool) {
	i := sort.Search(len(bars), func(i int) bool { return bars[i].Date >= sig.Date })
	if i >= len(bars) || bars[i].Date != sig.Date || i == 0 || bars[i-1].Close <= 0 {
		return sig, false
	}
	bar := bars[i]
	limit := bars[i-1].Close * cfg.EntryLimitPct
	if bar.Low > limit {
		return sig, false // the day never traded down to the limit
	}
	fill := limit
	if bar.Open > 0 && bar.Open < limit {
		fill = bar.Open
	}
	if session, covered := intradaySession(hourly, bar); covered {
		after, price, ok := fillOnIntraday(session, bar, limit)
		if !ok {
			return sig, false // the day's low is in the daily bar but no hour confirms a touch
		}
		fill, sig.AfterFill = price, after
	}
	sig.Close = fill
	sig.BuyLimit = fill
	sig.OrderType = "limit"
	return sig, true
}

// nextBarAfter returns the first bar dated strictly after date.
func nextBarAfter(bars []models.Bar, date string) (models.Bar, bool) {
	i := sort.Search(len(bars), func(i int) bool { return bars[i].Date > date })
	if i >= len(bars) {
		return models.Bar{}, false
	}
	return bars[i], true
}
