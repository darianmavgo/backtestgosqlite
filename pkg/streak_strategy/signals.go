package streak_strategy

import (
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// StreakSignals buys tradeSym long when signalBars prints consecutiveDays
// down closes (direction "drop") or up closes (direction "rally").
// Take-profit and stop are fractional offsets. Regime filters the watch bar.
// This is the grid-search entry rule; the SQL pipeline is checked against it.
func StreakSignals(signalBars, tradeBars []models.Bar, consecutiveDays int, direction, regime string, tpPct, slPct float64, holdDays int, tradeSym, strategyID string) []models.Signal {
	tradeByDate := make(map[string]models.Bar, len(tradeBars))
	for _, b := range tradeBars {
		tradeByDate[b.Date] = b
	}
	isLong := direction != "rally"
	if strategyID == "" {
		strategyID = tradeSym + "-opt"
	}
	var signals []models.Signal

	for i := consecutiveDays; i < len(signalBars); i++ {
		watch := signalBars[i]
		detected := true
		if isLong {
			for s := 0; s < consecutiveDays; s++ {
				if signalBars[i-s].Close >= signalBars[i-s-1].Close {
					detected = false
					break
				}
			}
		} else {
			for s := 0; s < consecutiveDays; s++ {
				if signalBars[i-s].Close <= signalBars[i-s-1].Close {
					detected = false
					break
				}
			}
		}
		if !detected {
			continue
		}
		switch {
		case strings.HasSuffix(regime, "<SMA200"):
			if watch.SMA200 > 0 && watch.Close >= watch.SMA200 {
				continue
			}
		case strings.HasSuffix(regime, "<SMA50"):
			if watch.SMA50 > 0 && watch.Close >= watch.SMA50 {
				continue
			}
		case strings.HasSuffix(regime, ">=SMA200"):
			if watch.SMA200 > 0 && watch.Close < watch.SMA200 {
				continue
			}
		case strings.HasSuffix(regime, ">=SMA50"):
			if watch.SMA50 > 0 && watch.Close < watch.SMA50 {
				continue
			}
		}
		tradeBar, ok := tradeByDate[watch.Date]
		if !ok || tradeBar.Close <= 0 {
			continue
		}
		entryPrice := tradeBar.Close
		sig := models.Signal{
			Symbol:           tradeSym,
			Date:             watch.Date,
			Open:             tradeBar.Open,
			High:             tradeBar.High,
			Low:              tradeBar.Low,
			Close:            entryPrice,
			Volume:           tradeBar.Volume,
			Entry:            1,
			Direction:        "LONG",
			OrderType:        "limit",
			BuyLimit:         entryPrice,
			Regime:           regime,
			HoldDaysOverride: holdDays,
			AssetClass:       "equity",
			StrategyID:       strategyID,
			Priority:         0,
		}
		if tpPct > 0 {
			sig.TakeProfit = entryPrice * (1.0 + tpPct)
		}
		if slPct > 0 {
			sig.StopLoss = entryPrice * (1.0 - slPct)
		}
		signals = append(signals, sig)
	}
	return signals
}
