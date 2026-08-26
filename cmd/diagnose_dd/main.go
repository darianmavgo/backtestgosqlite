package main

import (
	"fmt"
	"log"

	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	_ "github.com/mattn/go-sqlite3"
)

type BarData struct {
	Date   string  `db:"Date"`
	Open   float64 `db:"open"`
	High   float64 `db:"high"`
	Low    float64 `db:"low"`
	Close  float64 `db:"close"`
	Volume int64   `db:"volume"`
}

func main() {
	db, err := storage.OpenSQLite("data/sp500_etfs_study.db")
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	var vooBars, uproBars []BarData
	_ = db.Select(&vooBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, volume FROM backtest_start WHERE symbol = 'VOO' ORDER BY substr(Date, 1, 10) ASC;`)
	_ = db.Select(&uproBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, volume FROM backtest_start WHERE symbol = 'UPRO' ORDER BY substr(Date, 1, 10) ASC;`)

	signalMap := make(map[string]bool)
	for i := 3; i < len(vooBars); i++ {
		if vooBars[i].Close < vooBars[i-1].Close && vooBars[i-1].Close < vooBars[i-2].Close && vooBars[i-2].Close < vooBars[i-3].Close {
			signalMap[vooBars[i].Date] = true
		}
	}

	// Compare Closed-Trade Drawdown vs Daily Mark-to-Market Drawdown across allocations
	allocs := []float64{0.50, 0.65, 0.75, 0.80, 0.85, 0.90, 0.95, 1.00}

	fmt.Println("Allocation | Closed-Trade Max DD | Daily Mark-to-Market Max DD | Peak Date | Trough Date")
	fmt.Println("-----------------------------------------------------------------------------------------")

	for _, a := range allocs {
		closedDD, mtmDD, peakDate, troughDate := runDetailedSim(uproBars, signalMap, 100000.0, a, 0.045, 8, 0.05)
		fmt.Printf("   %3.0f%%    |        %5.2f%%       |           %5.2f%%            | %s | %s\n",
			a*100, closedDD, mtmDD, peakDate, troughDate)
	}
}

func runDetailedSim(tradeBars []BarData, signals map[string]bool, startCap, allocPct, cashYieldAnn float64, holdDays int, tpPct float64) (float64, float64, string, string) {
	cash := startCap
	closedPeak := startCap
	mtmPeak := startCap
	maxClosedDD := 0.0
	maxMtmDD := 0.0

	mtmPeakDate := ""
	mtmTroughDate := ""
	currentPeakDate := tradeBars[0].Date

	inPosUntil := -1
	activeShares := 0
	activeEntryPrice := 0.0
	inPos := false

	dailyYieldRate := 0.045 / 252.0

	for i := 0; i < len(tradeBars); i++ {
		date := tradeBars[i].Date
		uproClose := tradeBars[i].Close

		if cash > 0 && !inPos {
			cash += cash * dailyYieldRate
		}

		// Entry
		if signals[date] && i > inPosUntil && !inPos {
			posCap := cash * allocPct
			shares := int(posCap / uproClose)
			if shares > 0 {
				activeShares = shares
				activeEntryPrice = uproClose
				cash -= float64(activeShares) * activeEntryPrice
				inPos = true

				exitIdx := i + holdDays
				if exitIdx >= len(tradeBars) {
					exitIdx = len(tradeBars) - 1
				}

				actualExitIdx := exitIdx
				actualExitPrice := tradeBars[actualExitIdx].Close
				targetPrice := activeEntryPrice * (1.0 + tpPct)

				for d := i + 1; d <= exitIdx; d++ {
					if tradeBars[d].High >= targetPrice {
						actualExitIdx = d
						actualExitPrice = targetPrice
						if tradeBars[d].Open > targetPrice {
							actualExitPrice = tradeBars[d].Open
						}
						break
					}
				}
				_ = actualExitPrice

				inPosUntil = actualExitIdx
			}
		}

		// Exit
		if inPos && i == inPosUntil {
			actualExitPrice := tradeBars[i].Close
			targetPrice := activeEntryPrice * (1.0 + tpPct)
			if tradeBars[i].High >= targetPrice {
				actualExitPrice = targetPrice
			}
			realized := float64(activeShares) * actualExitPrice
			cash += realized
			inPos = false
			activeShares = 0

			// Update closed drawdown
			if cash > closedPeak {
				closedPeak = cash
			}
			cDD := (closedPeak - cash) / closedPeak * 100.0
			if cDD > maxClosedDD {
				maxClosedDD = cDD
			}
		}

		// Daily Mark to Market Equity
		posVal := 0.0
		if inPos {
			posVal = float64(activeShares) * uproClose
		}
		mtmEquity := cash + posVal

		if mtmEquity > mtmPeak {
			mtmPeak = mtmEquity
			currentPeakDate = date
		}
		mDD := (mtmPeak - mtmEquity) / mtmPeak * 100.0
		if mDD > maxMtmDD {
			maxMtmDD = mDD
			mtmPeakDate = currentPeakDate
			mtmTroughDate = date
		}
	}

	return maxClosedDD, maxMtmDD, mtmPeakDate, mtmTroughDate
}
