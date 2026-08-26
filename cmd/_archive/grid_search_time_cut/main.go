package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"

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

type TimeCutResult struct {
	MaxHoldDays   int
	CutRule       string // "Unconditional Time Cut" vs "Cut Only If Down"
	TakeProfitPct float64
	DisasterStop  float64
	EndingCap     float64
	NetProfit     float64
	TotalReturn   float64
	CAGR          float64
	MTMMaxDD      float64
	CalmarRatio   float64
	WinRate       float64
	TotalTrades   int
	DisasterHits  int
	TimeCuts      int
	ProfitHits    int
}

func main() {
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	initialCapital := flag.Float64("capital", 100000.0, "Starting cash ($)")
	allocRatio := flag.Float64("alloc", 0.65, "Dynamic allocation ratio (0.65 = 65%)")
	cashYieldAnn := flag.Float64("yield", 0.045, "Annual cash yield on idle reserves (4.5% APY)")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
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

	holdDaysList := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16, 20}
	disasterStop := 0.20 // -20% Hard Disaster Guard
	tpPct := 0.05        // +5% Take Profit Target

	var results []TimeCutResult

	for _, h := range holdDaysList {
		// Rule 1: Cut position on Day H if profit target not reached
		res := runTimeCutSim(teclBars, signalMap, *initialCapital, *allocRatio, *cashYieldAnn, h, tpPct, disasterStop)
		results = append(results, res)
	}

	fmt.Printf("\n=======================================================================================================================\n")
	fmt.Printf("⏱️ GRID SEARCH: OPTIMAL DAY TO CUT POSITION (WITH -20%% DISASTER GUARD & +5%% TP)\n")
	fmt.Printf("   Asset: TECL (3x Tech Bull) | Sizing: 65%% Dynamic Allocation + 4.5%% T-Bills | $100k Base\n")
	fmt.Printf("=======================================================================================================================\n\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Cut Day (Time Barrier)", "5-Year Profit ($)", "Ending Capital", "CAGR (%/yr)", "Max MTM DD", "🔥 Calmar", "Win Rate", "TP Hits (+5%)", "Time Cuts", "-20% Stops Hit"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, r := range results {
		table.Append([]string{
			fmt.Sprintf("Cut on Day %d", r.MaxHoldDays),
			fmt.Sprintf("+$%.2f", r.NetProfit),
			fmt.Sprintf("💰 $%.2f", r.EndingCap),
			fmt.Sprintf("%.2f%% / yr", r.CAGR),
			fmt.Sprintf("🔴 %.2f%%", r.MTMMaxDD),
			fmt.Sprintf("⭐ %.2f", r.CalmarRatio),
			fmt.Sprintf("🔥 %.1f%%", r.WinRate),
			fmt.Sprintf("🎯 %d", r.ProfitHits),
			fmt.Sprintf("⏱️ %d", r.TimeCuts),
			fmt.Sprintf("🛑 %d", r.DisasterHits),
		})
	}
	table.Render()

	// Sort by Net Profit DESC to find best
	sort.Slice(results, func(i, j int) bool {
		return results[i].NetProfit > results[j].NetProfit
	})

	fmt.Printf("\n🏆 #1 PROFIT CHAMPION: Cut on Day %d ➔ +$%.2f Net Profit ($%.2f Final) | %.2f%% CAGR | %.2f%% Max DD | Calmar: %.2f\n",
		results[0].MaxHoldDays, results[0].NetProfit, results[0].EndingCap, results[0].CAGR, results[0].MTMMaxDD, results[0].CalmarRatio)

	// Sort by Calmar Ratio DESC
	sort.Slice(results, func(i, j int) bool {
		return results[i].CalmarRatio > results[j].CalmarRatio
	})

	fmt.Printf("⭐ #1 RISK-ADJUSTED CALMAR CHAMPION: Cut on Day %d ➔ Calmar: %.2f | %.2f%% Max DD | %.2f%% CAGR | Win Rate: %.1f%%\n\n",
		results[0].MaxHoldDays, results[0].CalmarRatio, results[0].MTMMaxDD, results[0].CAGR, results[0].WinRate)
}

func runTimeCutSim(tradeBars []BarData, signals map[string]bool, startCap, allocPct, cashYieldAnn float64, holdDays int, tpPct, slPct float64) TimeCutResult {
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
	profitHits := 0
	timeCuts := 0
	disasterHits := 0
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
				exitType := "TIME_CUT"

				targetPrice := activeEntryPrice * (1.0 + tpPct)
				stopPrice := activeEntryPrice * (1.0 - slPct)

				for d := i + 1; d <= exitIdx; d++ {
					// 1. Check -20% Disaster Guard Stop
					if tradeBars[d].Low <= stopPrice {
						actualExitIdx = d
						actualExitPrice = stopPrice
						if tradeBars[d].Open < stopPrice {
							actualExitPrice = tradeBars[d].Open
						}
						exitType = "DISASTER_STOP"
						break
					}

					// 2. Check +5% Take Profit Target
					if tradeBars[d].High >= targetPrice {
						actualExitIdx = d
						actualExitPrice = targetPrice
						if tradeBars[d].Open > targetPrice {
							actualExitPrice = tradeBars[d].Open
						}
						exitType = "PROFIT_TARGET"
						break
					}
				}

				pnl := float64(activeShares) * (actualExitPrice - activeEntryPrice)
				if pnl > 0 {
					wins++
				} else {
					losses++
				}

				if exitType == "PROFIT_TARGET" {
					profitHits++
				} else if exitType == "DISASTER_STOP" {
					disasterHits++
				} else {
					timeCuts++
				}

				totalTrades++
				inPosUntil = actualExitIdx
			}
		}

		if inPos && i == inPosUntil {
			actualExitPrice := tradeBars[i].Close
			targetPrice := activeEntryPrice * (1.0 + tpPct)
			stopPrice := activeEntryPrice * (1.0 - slPct)

			if tradeBars[i].Low <= stopPrice {
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
	calmar := 0.0
	if maxMtmDD > 0 {
		calmar = cagr / maxMtmDD
	}

	return TimeCutResult{
		MaxHoldDays:   holdDays,
		TakeProfitPct: tpPct,
		DisasterStop:  slPct,
		EndingCap:     finalEquity,
		NetProfit:     pnl,
		TotalReturn:   pnl / startCap * 100.0,
		CAGR:          cagr,
		MTMMaxDD:      maxMtmDD,
		CalmarRatio:   calmar,
		WinRate:       winRate,
		TotalTrades:   totalTrades,
		DisasterHits:  disasterHits,
		TimeCuts:      timeCuts,
		ProfitHits:    profitHits,
	}
}
