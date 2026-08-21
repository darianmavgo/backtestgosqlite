package simulator

import (
	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/darianmavgo/backtestgosqlite/internal/strategy"
)

// EvaluateTradeOutcome performs a path-dependent dual-barrier simulation on an individual trade,
// evaluating day-by-day whether stop-loss, profit-target, or time-up exit occurs first.
func EvaluateTradeOutcome(
	config strategy.StrategyConfig,
	symbol string,
	entryIdx int,
	entryDate string,
	entryPrice float64,
	futureBars []models.Bar,
) models.Trade {
	targetPrice := entryPrice * config.TargetPct
	stopLossPrice := entryPrice * config.StopLossPct

	trade := models.Trade{
		Symbol:        symbol,
		EntryIdx:      entryIdx,
		EntryDate:     entryDate,
		EntryPrice:    entryPrice,
		TargetPrice:   targetPrice,
		StopLossPrice: stopLossPrice,
		ExitReason:    models.ExitReasonTimeUp,
	}

	if len(futureBars) == 0 {
		trade.ExitDate = entryDate
		trade.ExitPrice = entryPrice
		trade.ExitReason = models.ExitReasonEndBacktest
		return trade
	}

	minLowSeen := entryPrice
	maxHighSeen := entryPrice

	maxHold := config.HoldingWindow
	if maxHold <= 0 {
		maxHold = 10
	}
	if len(futureBars) < maxHold {
		maxHold = len(futureBars)
	}

	for day := 0; day < maxHold; day++ {
		bar := futureBars[day]

		if bar.Low < minLowSeen {
			minLowSeen = bar.Low
		}
		if bar.High > maxHighSeen {
			maxHighSeen = bar.High
		}

		// Calculate running MAE and MFE
		currentMAE := (minLowSeen - entryPrice) / entryPrice
		currentMFE := (maxHighSeen - entryPrice) / entryPrice
		if currentMAE < trade.MaxAdverseExcursion {
			trade.MaxAdverseExcursion = currentMAE
		}
		if currentMFE > trade.MaxFavorableExcursion {
			trade.MaxFavorableExcursion = currentMFE
		}

		// 1. Check Stop-Loss Breach First (Conservative Risk-First Assumption)
		if bar.Low <= stopLossPrice {
			trade.ExitDate = bar.Date
			// Model slippage or gap down below stop
			if bar.Open < stopLossPrice {
				trade.ExitPrice = bar.Open * (1.0 - config.SlippagePct)
			} else {
				trade.ExitPrice = stopLossPrice * (1.0 - config.SlippagePct)
			}
			trade.ExitReason = models.ExitReasonStopLoss
			trade.HoldDays = day + 1
			trade.ReturnPct = (trade.ExitPrice - entryPrice) / entryPrice
			return trade
		}

		// 2. Check Profit Target Hit
		if bar.High >= targetPrice {
			trade.ExitDate = bar.Date
			// Model gap up above target
			if bar.Open > targetPrice {
				trade.ExitPrice = bar.Open * (1.0 - config.SlippagePct)
			} else {
				trade.ExitPrice = targetPrice * (1.0 - config.SlippagePct)
			}
			trade.ExitReason = models.ExitReasonProfitTarget
			trade.HoldDays = day + 1
			trade.ReturnPct = (trade.ExitPrice - entryPrice) / entryPrice
			return trade
		}
	}

	// 3. Time-Up Exit: Held for max holding days without hitting stop or target
	lastBar := futureBars[maxHold-1]
	trade.ExitDate = lastBar.Date
	trade.ExitPrice = lastBar.Close * (1.0 - config.SlippagePct)
	trade.ExitReason = models.ExitReasonTimeUp
	trade.HoldDays = maxHold
	trade.ReturnPct = (trade.ExitPrice - entryPrice) / entryPrice

	return trade
}
