package simulator

import "github.com/darianmavgo/backtestgosqlite/pkg/models"

// sameDayExit reports whether a long position opened on bar's session is closed
// by that same session: the stop when the low reaches it, else the target when
// the high reaches it. The stop is judged first because the order of the day's
// touches is unknown. With afterFill (intraday bars from the fill on) the bars
// are judged in order instead of the daily range. The fill is at the level, less slippage. A short is never
// exited here (its levels run the other way). It also records the session's range
// on the position, so the trade's excursion covers the day it opened.
func sameDayExit(pos *models.Position, bar models.Bar, slippage float64, afterFill []models.Bar) (price float64, reason models.ExitReason, ok bool) {
	if pos.Direction == "SHORT" {
		return 0, "", false
	}
	if afterFill != nil {
		// Intraday bars: the touches are taken in time order, and inside one bar
		// the stop comes before the target.
		for _, b := range afterFill {
			if pos.StopLossPrice > 0 && b.Low <= pos.StopLossPrice {
				return pos.StopLossPrice * (1.0 - slippage), models.ExitReasonStopLoss, true
			}
			if pos.TargetPrice > 0 && b.High >= pos.TargetPrice {
				return pos.TargetPrice * (1.0 - slippage), models.ExitReasonProfitTarget, true
			}
		}
		return 0, "", false
	}
	if bar.Low < pos.MinLowSince {
		pos.MinLowSince = bar.Low
	}
	if bar.High > pos.MaxHighSince {
		pos.MaxHighSince = bar.High
	}
	if pos.StopLossPrice > 0 && bar.Low <= pos.StopLossPrice {
		return pos.StopLossPrice * (1.0 - slippage), models.ExitReasonStopLoss, true
	}
	if pos.TargetPrice > 0 && bar.High >= pos.TargetPrice {
		return pos.TargetPrice * (1.0 - slippage), models.ExitReasonProfitTarget, true
	}
	return 0, "", false
}
