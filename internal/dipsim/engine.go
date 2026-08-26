package dipsim

import (
	"fmt"
	"math"
	"sync"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/darianmavgo/backtestgosqlite/internal/signals"
	"github.com/jmoiron/sqlx"
)

// Run executes a single dip-buy simulation with the given config.
// signalDates is a pre-computed set of dates on which an entry signal fires.
// tradeBars are the OHLCV bars for the asset being traded.
func Run(cfg DipSimConfig, signalDates map[string]bool, tradeBars []models.BarData) DipSimResult {
	cash := cfg.InitialCapital
	mtmPeak := cfg.InitialCapital
	closedPeak := cfg.InitialCapital
	maxMtmDD := 0.0
	maxClosedDD := 0.0

	dailyYieldRate := 0.0
	if cfg.CashYieldAnnual > 0 {
		dailyYieldRate = math.Pow(1.0+cfg.CashYieldAnnual, 1.0/252.0) - 1.0
	}

	inPosUntil := -1
	activeShares := 0
	activeEntryPrice := 0.0
	inPos := false

	wins := 0
	losses := 0
	grossGains := 0.0
	grossLosses := 0.0
	totalTrades := 0
	totalHoldDays := 0

	var trades []ExecutedTrade
	var dailyPoints []models.DailySnapshot

	minIdx := cfg.SignalDays
	if minIdx < 3 {
		minIdx = 3
	}

	for i := minIdx; i < len(tradeBars); i++ {
		date := tradeBars[i].Date
		closePrice := tradeBars[i].Close

		// Accrue daily Treasury yield on idle cash
		if cash > 0 && !inPos {
			cash += cash * dailyYieldRate
		}

		// Entry: signal fires and we're not already in a position
		if signalDates[date] && i > inPosUntil && !inPos {
			posCap := cash * cfg.AllocationPct
			shares := int(posCap / closePrice)
			if shares > 0 {
				activeShares = shares
				activeEntryPrice = closePrice
				cash -= float64(activeShares) * activeEntryPrice
				inPos = true

				exitIdx := i + cfg.MaxHoldDays
				if exitIdx >= len(tradeBars) {
					exitIdx = len(tradeBars) - 1
				}

				actualExitIdx := exitIdx
				actualExitPrice := tradeBars[actualExitIdx].Close
				actualExitReason := fmt.Sprintf("TIME_UP_%dDAYS", cfg.MaxHoldDays)

				targetPrice := 999999.0
				if cfg.TakeProfitPct > 0 {
					targetPrice = activeEntryPrice * (1.0 + cfg.TakeProfitPct)
				}
				stopPrice := 0.0
				if cfg.StopLossPct > 0 {
					stopPrice = activeEntryPrice * (1.0 - cfg.StopLossPct)
				}

				for d := i + 1; d <= exitIdx; d++ {
					// Stop loss check
					if cfg.StopLossPct > 0 && tradeBars[d].Low <= stopPrice {
						actualExitIdx = d
						actualExitPrice = stopPrice
						if tradeBars[d].Open < stopPrice {
							actualExitPrice = tradeBars[d].Open
						}
						actualExitReason = fmt.Sprintf("STOP_LOSS_-%.0f%%", cfg.StopLossPct*100)
						break
					}

					// Take profit check
					if cfg.TakeProfitPct > 0 && tradeBars[d].High >= targetPrice {
						actualExitIdx = d
						actualExitPrice = targetPrice
						if tradeBars[d].Open > targetPrice {
							actualExitPrice = tradeBars[d].Open
						}
						actualExitReason = fmt.Sprintf("PROFIT_TARGET_+%.0f%%", cfg.TakeProfitPct*100)
						break
					}
				}

				holdDur := actualExitIdx - i
				if holdDur < 1 {
					holdDur = 1
				}
				totalHoldDays += holdDur

				pnl := float64(activeShares) * (actualExitPrice - activeEntryPrice)
				ret := (actualExitPrice - activeEntryPrice) / activeEntryPrice
				isWin := pnl > 0

				if isWin {
					wins++
					grossGains += pnl
				} else {
					losses++
					grossLosses += math.Abs(pnl)
				}
				totalTrades++

				direction := "LONG"
				if cfg.SignalDirection == "rally" {
					direction = "SHORT"
				}

				trades = append(trades, ExecutedTrade{
					TradeNum:   len(trades) + 1,
					Direction:  direction,
					Asset:      cfg.TradeSymbol,
					SignalDate: date,
					EntryDate:  date,
					ExitDate:   tradeBars[actualExitIdx].Date,
					EntryPrice: activeEntryPrice,
					ExitPrice:  actualExitPrice,
					HoldDays:   holdDur,
					ExitReason: actualExitReason,
					ReturnPct:  ret,
					NetPnL:     pnl,
					IsWin:      isWin,
				})
				inPosUntil = actualExitIdx
			}
		}

		// Exit
		if inPos && i == inPosUntil {
			currTrade := trades[len(trades)-1]
			realized := float64(activeShares) * currTrade.ExitPrice
			cash += realized
			inPos = false
			activeShares = 0

			if cash > closedPeak {
				closedPeak = cash
			}
			cDD := (closedPeak - cash) / closedPeak * 100.0
			if cDD > maxClosedDD {
				maxClosedDD = cDD
			}
		}

		// Daily mark-to-market
		posVal := 0.0
		if inPos {
			posVal = float64(activeShares) * closePrice
		}
		mtmEquity := cash + posVal
		if mtmEquity > mtmPeak {
			mtmPeak = mtmEquity
		}
		mDD := (mtmPeak - mtmEquity) / mtmPeak * 100.0
		if mDD > maxMtmDD {
			maxMtmDD = mDD
		}

		activeTag := ""
		if inPos {
			activeTag = cfg.TradeSymbol
		}
		dailyPoints = append(dailyPoints, models.DailySnapshot{
			Date:      date,
			Equity:    mtmEquity,
			Drawdown:  mDD,
			ActivePos: activeTag,
		})
	}

	// Final equity
	posVal := 0.0
	if inPos {
		posVal = float64(activeShares) * tradeBars[len(tradeBars)-1].Close
	}
	finalEquity := cash + posVal
	pnl := finalEquity - cfg.InitialCapital
	totRet := pnl / cfg.InitialCapital * 100.0
	cagr := (math.Pow(math.Max(0.01, finalEquity/cfg.InitialCapital), 1.0/5.0) - 1.0) * 100.0

	winRate := 0.0
	if totalTrades > 0 {
		winRate = float64(wins) / float64(totalTrades) * 100.0
	}
	pf := 0.0
	if grossLosses > 0 {
		pf = grossGains / grossLosses
	} else if grossGains > 0 {
		pf = 99.0
	}
	calmar := 0.0
	if maxMtmDD > 0 {
		calmar = cagr / maxMtmDD
	}
	avgHold := 0.0
	if totalTrades > 0 {
		avgHold = float64(totalHoldDays) / float64(totalTrades)
	}

	return DipSimResult{
		Config:       cfg,
		EndingCap:    finalEquity,
		NetProfit:    pnl,
		TotalReturn:  totRet,
		CAGR:         cagr,
		MTMMaxDD:     maxMtmDD,
		ClosedMaxDD:  maxClosedDD,
		CalmarRatio:  calmar,
		WinRate:      winRate,
		TotalTrades:  totalTrades,
		Wins:         wins,
		Losses:       losses,
		ProfitFactor: pf,
		AvgHoldDays:  avgHold,
		Trades:       trades,
		DailySeries:  dailyPoints,
	}
}

// RunFromSQL is a convenience that loads signal dates from a SQL file, then runs the simulation.
func RunFromSQL(db *sqlx.DB, cfg DipSimConfig, tradeBars []models.BarData, sigBars []models.SignalBar) DipSimResult {
	var signalDates map[string]bool

	if cfg.SignalSQLFile != "" {
		params := map[string]string{
			"signal_symbol": cfg.SignalSymbol,
		}
		result, err := signals.LoadFromSQL(db, cfg.SignalSQLFile, params)
		if err == nil {
			signalDates = result.Dates
		}
	}

	// Fallback to Go-based detection if SQL didn't produce results
	if len(signalDates) == 0 {
		barData := make([]models.BarData, len(tradeBars))
		copy(barData, tradeBars)
		// We need VOO bars for signal detection, not trade bars — this is handled at the call site
	}

	// Apply regime filter if needed
	if cfg.RegimeFilter != "" && cfg.RegimeFilter != "All Regimes" && len(sigBars) > 0 {
		signalDates = signals.FilterByRegime(signalDates, sigBars, cfg.RegimeFilter)
	}

	return Run(cfg, signalDates, tradeBars)
}

// RunCombo executes a dual long/short strategy where the long engine fires on dips
// and the short engine fires on rallies (in bear markets).
func RunCombo(combo ComboConfig, longSignals, shortSignals map[string]bool, vooBars, longBars, shortBars []models.BarData) ComboResult {
	startCap := combo.InitialCapital
	if startCap <= 0 {
		startCap = combo.LongConfig.InitialCapital
	}
	cashYield := combo.CashYieldAnnual
	if cashYield <= 0 {
		cashYield = combo.LongConfig.CashYieldAnnual
	}

	cash := startCap
	dailyYieldRate := 0.0
	if cashYield > 0 {
		dailyYieldRate = math.Pow(1.0+cashYield, 1.0/252.0) - 1.0
	}

	// Build date-indexed maps
	longMap := make(map[string]models.BarData, len(longBars))
	for _, b := range longBars {
		longMap[b.Date] = b
	}
	shortMap := make(map[string]models.BarData, len(shortBars))
	for _, b := range shortBars {
		shortMap[b.Date] = b
	}

	// Align common dates
	var commonDates []string
	for _, b := range vooBars {
		if _, ok1 := longMap[b.Date]; ok1 {
			if _, ok2 := shortMap[b.Date]; ok2 {
				commonDates = append(commonDates, b.Date)
			}
		}
	}

	vooStart := vooBars[0].Close
	vooShares := startCap / vooStart

	inPosUntil := -1
	activeShares := 0
	activeEntryPrice := 0.0
	activeAsset := ""
	inPos := false

	comboPeak := startCap
	vooPeak := startCap
	comboMaxDD := 0.0

	longTrades := 0
	shortTrades := 0
	longWins := 0
	shortWins := 0
	grossGains := 0.0
	grossLosses := 0.0

	var trades []ExecutedTrade
	var dailyPoints []models.DailySnapshot
	var vooPoints []models.DailySnapshot

	for i := 3; i < len(commonDates); i++ {
		date := commonDates[i]
		vooClose := vooBars[i].Close

		if cash > 0 && !inPos {
			cash += cash * dailyYieldRate
		}

		shouldBuyLong := longSignals[date]
		shouldBuyShort := shortSignals[date]

		if (shouldBuyLong || shouldBuyShort) && i > inPosUntil && !inPos {
			var tradeAsset string
			var tradeBarsMap map[string]models.BarData
			var holdDays int
			var tpPct, slPct float64
			var direction string

			if shouldBuyLong {
				tradeAsset = combo.LongConfig.TradeSymbol
				tradeBarsMap = longMap
				holdDays = combo.LongConfig.MaxHoldDays
				tpPct = combo.LongConfig.TakeProfitPct
				slPct = combo.LongConfig.StopLossPct
				direction = "LONG"
			} else {
				tradeAsset = combo.ShortConfig.TradeSymbol
				tradeBarsMap = shortMap
				holdDays = combo.ShortConfig.MaxHoldDays
				tpPct = combo.ShortConfig.TakeProfitPct
				slPct = combo.ShortConfig.StopLossPct
				direction = "SHORT"
			}

			allocPct := combo.LongConfig.AllocationPct
			if shouldBuyShort {
				allocPct = combo.ShortConfig.AllocationPct
			}

			entryPrice := tradeBarsMap[date].Close
			posCap := cash * allocPct
			shares := int(posCap / entryPrice)

			if shares > 0 {
				activeShares = shares
				activeEntryPrice = entryPrice
				activeAsset = tradeAsset
				cash -= float64(activeShares) * activeEntryPrice
				inPos = true

				exitIdx := i + holdDays
				if exitIdx >= len(commonDates) {
					exitIdx = len(commonDates) - 1
				}

				actualExitIdx := exitIdx
				actualExitPrice := tradeBarsMap[commonDates[actualExitIdx]].Close
				actualExitReason := fmt.Sprintf("TIME_UP_%dDAYS", holdDays)

				targetPrice := 999999.0
				if tpPct > 0 {
					targetPrice = activeEntryPrice * (1.0 + tpPct)
				}
				stopPrice := 0.0
				if slPct > 0 {
					stopPrice = activeEntryPrice * (1.0 - slPct)
				}

				for d := i + 1; d <= exitIdx; d++ {
					dDate := commonDates[d]
					dBar := tradeBarsMap[dDate]

					if slPct > 0 && dBar.Low <= stopPrice {
						actualExitIdx = d
						actualExitPrice = stopPrice
						if dBar.Open < stopPrice {
							actualExitPrice = dBar.Open
						}
						actualExitReason = fmt.Sprintf("STOP_LOSS_-%.0f%%", slPct*100)
						break
					}

					if tpPct > 0 && dBar.High >= targetPrice {
						actualExitIdx = d
						actualExitPrice = targetPrice
						if dBar.Open > targetPrice {
							actualExitPrice = dBar.Open
						}
						actualExitReason = fmt.Sprintf("PROFIT_TARGET_+%.0f%%", tpPct*100)
						break
					}
				}

				pnl := float64(activeShares) * (actualExitPrice - activeEntryPrice)
				ret := (actualExitPrice - activeEntryPrice) / activeEntryPrice
				isWin := pnl > 0

				if direction == "LONG" {
					longTrades++
					if isWin {
						longWins++
					}
				} else {
					shortTrades++
					if isWin {
						shortWins++
					}
				}

				if isWin {
					grossGains += pnl
				} else {
					grossLosses += math.Abs(pnl)
				}

				trades = append(trades, ExecutedTrade{
					TradeNum:   len(trades) + 1,
					Direction:  direction,
					Asset:      tradeAsset,
					SignalDate: date,
					EntryDate:  date,
					ExitDate:   commonDates[actualExitIdx],
					EntryPrice: activeEntryPrice,
					ExitPrice:  actualExitPrice,
					HoldDays:   actualExitIdx - i,
					ExitReason: actualExitReason,
					ReturnPct:  ret,
					NetPnL:     pnl,
					IsWin:      isWin,
				})
				inPosUntil = actualExitIdx
			}
		}

		// Exit
		if inPos && i == inPosUntil {
			currTrade := trades[len(trades)-1]
			realized := float64(activeShares) * currTrade.ExitPrice
			cash += realized
			inPos = false
			activeShares = 0
			activeAsset = ""
		}

		// Daily MTM
		posVal := 0.0
		if inPos {
			if activeAsset == combo.LongConfig.TradeSymbol {
				posVal = float64(activeShares) * longMap[date].Close
			} else {
				posVal = float64(activeShares) * shortMap[date].Close
			}
		}
		stratEquity := cash + posVal
		vooEquity := vooShares * vooClose

		if stratEquity > comboPeak {
			comboPeak = stratEquity
		}
		stratDD := (comboPeak - stratEquity) / comboPeak * 100.0
		if stratDD > comboMaxDD {
			comboMaxDD = stratDD
		}

		if vooEquity > vooPeak {
			vooPeak = vooEquity
		}
		vooDD := (vooPeak - vooEquity) / vooPeak * 100.0

		tag := ""
		if inPos {
			tag = activeAsset
		}
		dailyPoints = append(dailyPoints, models.DailySnapshot{
			Date:      date,
			Equity:    stratEquity,
			Drawdown:  stratDD,
			ActivePos: tag,
		})
		vooPoints = append(vooPoints, models.DailySnapshot{
			Date:     date,
			Equity:   vooEquity,
			Drawdown: vooDD,
		})
	}

	finalEquity := dailyPoints[len(dailyPoints)-1].Equity
	vooFinal := vooPoints[len(vooPoints)-1].Equity
	_ = vooFinal

	pnl := finalEquity - startCap
	totRet := pnl / startCap * 100.0
	cagr := (math.Pow(math.Max(0.01, finalEquity/startCap), 1.0/5.0) - 1.0) * 100.0

	totalWins := longWins + shortWins
	totalTrades := longTrades + shortTrades
	winRate := 0.0
	if totalTrades > 0 {
		winRate = float64(totalWins) / float64(totalTrades) * 100.0
	}
	pf := 0.0
	if grossLosses > 0 {
		pf = grossGains / grossLosses
	}
	calmar := 0.0
	if comboMaxDD > 0 {
		calmar = cagr / comboMaxDD
	}

	return ComboResult{
		LongConfig:   combo.LongConfig,
		ShortConfig:  combo.ShortConfig,
		EndingCap:    finalEquity,
		NetProfit:    pnl,
		TotalReturn:  totRet,
		CAGR:         cagr,
		MTMMaxDD:     comboMaxDD,
		CalmarRatio:  calmar,
		WinRate:      winRate,
		TotalTrades:  totalTrades,
		LongTrades:   longTrades,
		ShortTrades:  shortTrades,
		LongWins:     longWins,
		ShortWins:    shortWins,
		ProfitFactor: pf,
		Trades:       trades,
		DailySeries:  dailyPoints,
		VOOSeries:    vooPoints,
	}
}

// GridSearch runs a parallel parameter sweep and returns all valid results.
// minTrades filters out configurations with fewer than the specified number of trades.
func GridSearch(baseCfg DipSimConfig, ranges GridRanges, signalBarsMap map[string][]models.BarData, tradeBarsMap map[string][]models.BarData, sigBars []models.SignalBar, minTrades int) []DipSimResult {
	type task struct {
		cfg         DipSimConfig
		signalDates map[string]bool
		tradeBars   []models.BarData
	}

	taskChan := make(chan task, 5000)
	var results []DipSimResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	numWorkers := 16
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range taskChan {
				res := Run(t.cfg, t.signalDates, t.tradeBars)
				if res.TotalTrades >= minTrades {
					mu.Lock()
					results = append(results, res)
					mu.Unlock()
				}
			}
		}()
	}

	// Generate all permutations
	for _, sym := range ranges.Symbols {
		tBars, ok := tradeBarsMap[sym]
		if !ok {
			continue
		}

		for _, sigDays := range ranges.SignalDays {
			// Compute signal dates from the signal bars
			sigSymBars, ok := signalBarsMap[baseCfg.SignalSymbol]
			if !ok {
				continue
			}

			var rawSignals map[string]bool
			if baseCfg.SignalDirection == "rally" {
				rawSignals = signals.DetectConsecutiveRallies(sigSymBars, sigDays)
			} else {
				rawSignals = signals.DetectConsecutiveDrops(sigSymBars, sigDays)
			}

			for _, regime := range ranges.RegimeFilters {
				filteredSignals := signals.FilterByRegime(rawSignals, sigBars, regime)

				for _, hold := range ranges.HoldDays {
					for _, tp := range ranges.TakeProfitPcts {
						for _, sl := range ranges.StopLossPcts {
							allocPcts := ranges.AllocationPcts
							if len(allocPcts) == 0 {
								allocPcts = []float64{baseCfg.AllocationPct}
							}
							for _, alloc := range allocPcts {
								cfg := baseCfg
								cfg.TradeSymbol = sym
								cfg.SignalDays = sigDays
								cfg.MaxHoldDays = hold
								cfg.TakeProfitPct = tp
								cfg.StopLossPct = sl
								cfg.RegimeFilter = regime
								cfg.AllocationPct = alloc
								cfg.Label = fmt.Sprintf("%s/%dd/%dd/+%.0f%%/-%.0f%%/%s", sym, sigDays, hold, tp*100, sl*100, regime)

								taskChan <- task{cfg: cfg, signalDates: filteredSignals, tradeBars: tBars}
							}
						}
					}
				}
			}
		}
	}

	close(taskChan)
	wg.Wait()

	return results
}
