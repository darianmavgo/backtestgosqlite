package main

import (
	"fmt"
	"log"
	"math"
	"os"

	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
)

type BarData struct {
	Date     string  `db:"Date"`
	Open     float64 `db:"open"`
	High     float64 `db:"high"`
	Low      float64 `db:"low"`
	Close    float64 `db:"close"`
	AdjClose float64 `db:"Adj Close"`
	Volume   int64   `db:"volume"`
}

type StopLossResult struct {
	StopLossDesc string
	StopLossPct  float64
	WinRate      float64
	Wins         int
	Losses       int
	TotalTrades  int
	StoppedOut   int
	ProfitFactor float64
	NetProfit    float64
	EndingCap    float64
	CAGR         float64
	MTMMaxDD     float64
	CalmarRatio  float64
	WinRateDelta string
}

func main() {
	db, err := storage.OpenSQLite("data/sp500_etfs_study.db")
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	var vooBars, teclBars []BarData
	_ = db.Select(&vooBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = 'VOO' ORDER BY substr(Date, 1, 10) ASC;`)
	_ = db.Select(&teclBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = 'TECL' ORDER BY substr(Date, 1, 10) ASC;`)

	signalMap := make(map[string]bool)
	for i := 3; i < len(vooBars); i++ {
		if vooBars[i].Close < vooBars[i-1].Close &&
			vooBars[i-1].Close < vooBars[i-2].Close &&
			vooBars[i-2].Close < vooBars[i-3].Close {
			signalMap[vooBars[i].Date] = true
		}
	}

	stopLossLevels := []float64{
		0.0,   // None (Baseline)
		-0.03, // -3%
		-0.04, // -4%
		-0.05, // -5%
		-0.06, // -6%
		-0.07, // -7%
		-0.08, // -8%
		-0.09, // -9%
		-0.10, // -10%
		-0.12, // -12%
		-0.15, // -15%
		-0.18, // -18%
		-0.20, // -20%
		-0.25, // -25%
	}

	var results []StopLossResult
	baselineWinRate := 0.0

	for i, sl := range stopLossLevels {
		res := runStopLossSim(teclBars, signalMap, 100000.0, 0.65, 0.045, 8, 0.05, math.Abs(sl))
		if i == 0 {
			baselineWinRate = res.WinRate
			res.WinRateDelta = "Baseline"
		} else {
			delta := res.WinRate - baselineWinRate
			if delta >= 0 {
				res.WinRateDelta = fmt.Sprintf("🟢 +%.1f%%", delta)
			} else {
				res.WinRateDelta = fmt.Sprintf("🔴 %.1f%%", delta)
			}
		}
		results = append(results, res)
	}

	fmt.Printf("\n=======================================================================================================================\n")
	fmt.Printf("🔬 TECL BULL STRATEGY STOP LOSS SENSITIVITY STUDY (VOO 3-DAY DROP / 8-DAY HOLD / +5%% TP / 65%% ALLOC)\n")
	fmt.Printf("=======================================================================================================================\n\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Stop Loss Level", "Win Rate (%)", "Win Rate vs Baseline", "Stopped Out", "Wins / Losses", "Profit Factor", "5-Year Profit ($)", "Ending Capital", "CAGR", "Max MTM DD", "🔥 Calmar"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, r := range results {
		slLabel := "None (Hold to Exit)"
		if r.StopLossPct > 0 {
			slLabel = fmt.Sprintf("-%.0f%% Fixed Stop", r.StopLossPct*100)
		}

		table.Append([]string{
			slLabel,
			fmt.Sprintf("%.1f%%", r.WinRate),
			r.WinRateDelta,
			fmt.Sprintf("%d trades", r.StoppedOut),
			fmt.Sprintf("%dW / %dL", r.Wins, r.Losses),
			fmt.Sprintf("%.2f", r.ProfitFactor),
			fmt.Sprintf("+$%.2f", r.NetProfit),
			fmt.Sprintf("💰 $%.2f", r.EndingCap),
			fmt.Sprintf("%.2f%%/yr", r.CAGR),
			fmt.Sprintf("🔴 %.2f%%", r.MTMMaxDD),
			fmt.Sprintf("⭐ %.2f", r.CalmarRatio),
		})
	}
	table.Render()
	fmt.Println()
}

func runStopLossSim(tradeBars []BarData, signals map[string]bool, startCap, allocPct, cashYieldAnn float64, holdDays int, tpPct, slPct float64) StopLossResult {
	cash := startCap
	mtmPeak := startCap
	maxMtmDD := 0.0
	dailyYieldRate := math.Pow(1.0+cashYieldAnn, 1.0/252.0) - 1.0

	inPosUntil := -1
	activeShares := 0
	activeEntryPrice := 0.0
	inPos := false

	wins := 0
	losses := 0
	stoppedOutCount := 0
	grossGains := 0.0
	grossLosses := 0.0
	totalTrades := 0

	for i := 3; i < len(tradeBars); i++ {
		date := tradeBars[i].Date
		closePrice := tradeBars[i].Close

		if cash > 0 && !inPos {
			cash += cash * dailyYieldRate
		}

		if signals[date] && i > inPosUntil && !inPos {
			posCap := cash * allocPct
			shares := int(posCap / closePrice)
			if shares > 0 {
				activeShares = shares
				activeEntryPrice = closePrice
				cash -= float64(activeShares) * activeEntryPrice
				inPos = true

				exitIdx := i + holdDays
				if exitIdx >= len(tradeBars) {
					exitIdx = len(tradeBars) - 1
				}

				actualExitIdx := exitIdx
				actualExitPrice := tradeBars[actualExitIdx].Close

				targetPrice := activeEntryPrice * (1.0 + tpPct)
				stopPrice := 0.0
				if slPct > 0 {
					stopPrice = activeEntryPrice * (1.0 - slPct)
				}

				hitStop := false
				for d := i + 1; d <= exitIdx; d++ {
					// Stop loss check
					if slPct > 0 && tradeBars[d].Low <= stopPrice {
						actualExitIdx = d
						actualExitPrice = stopPrice
						if tradeBars[d].Open < stopPrice {
							actualExitPrice = tradeBars[d].Open
						}
						hitStop = true
						stoppedOutCount++
						break
					}

					// Take profit check
					if tradeBars[d].High >= targetPrice {
						actualExitIdx = d
						actualExitPrice = targetPrice
						if tradeBars[d].Open > targetPrice {
							actualExitPrice = tradeBars[d].Open
						}
						break
					}
				}

				pnl := float64(activeShares) * (actualExitPrice - activeEntryPrice)
				if pnl > 0 {
					wins++
					grossGains += pnl
				} else {
					losses++
					grossLosses += math.Abs(pnl)
				}
				totalTrades++
				inPosUntil = actualExitIdx
				_ = hitStop
			}
		}

		if inPos && i == inPosUntil {
			actualExitPrice := tradeBars[i].Close
			targetPrice := activeEntryPrice * (1.0 + tpPct)
			stopPrice := activeEntryPrice * (1.0 - slPct)

			if slPct > 0 && tradeBars[i].Low <= stopPrice {
				actualExitPrice = stopPrice
			} else if tradeBars[i].High >= targetPrice {
				actualExitPrice = targetPrice
			}

			realized := float64(activeShares) * actualExitPrice
			cash += realized
			inPos = false
			activeShares = 0
		}

		posVal := 0.0
		if inPos {
			posVal = float64(activeShares) * closePrice
		}
		mtmEquity := cash + posVal
		if mtmEquity > mtmPeak {
			mtmPeak = mtmEquity
		}
		dd := (mtmPeak - mtmEquity) / mtmPeak * 100.0
		if dd > maxMtmDD {
			maxMtmDD = dd
		}
	}

	posVal := 0.0
	if inPos {
		posVal = float64(activeShares) * tradeBars[len(tradeBars)-1].Close
	}
	finalEquity := cash + posVal
	pnl := finalEquity - startCap
	cagr := (math.Pow(math.Max(0.01, finalEquity/startCap), 1.0/5.0) - 1.0) * 100.0

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

	return StopLossResult{
		StopLossPct:  slPct,
		WinRate:      winRate,
		Wins:         wins,
		Losses:       losses,
		TotalTrades:  totalTrades,
		StoppedOut:   stoppedOutCount,
		ProfitFactor: pf,
		NetProfit:    pnl,
		EndingCap:    finalEquity,
		CAGR:         cagr,
		MTMMaxDD:     maxMtmDD,
		CalmarRatio:  calmar,
	}
}
