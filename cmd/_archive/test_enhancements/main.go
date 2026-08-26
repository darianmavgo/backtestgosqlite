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

	var vooBars, uproBars, tqqqBars []BarData
	_ = db.Select(&vooBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, volume FROM backtest_start WHERE symbol = 'VOO' ORDER BY substr(Date, 1, 10) ASC;`)
	_ = db.Select(&uproBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, volume FROM backtest_start WHERE symbol = 'UPRO' ORDER BY substr(Date, 1, 10) ASC;`)
	_ = db.Select(&tqqqBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, volume FROM backtest_start WHERE symbol = 'TQQQ' ORDER BY substr(Date, 1, 10) ASC;`)

	// 3-day decline signals on VOO
	signalMap := make(map[string]bool)
	for i := 3; i < len(vooBars); i++ {
		if vooBars[i].Close < vooBars[i-1].Close && vooBars[i-1].Close < vooBars[i-2].Close && vooBars[i-2].Close < vooBars[i-3].Close {
			signalMap[vooBars[i].Date] = true
		}
	}

	// 1. Run Baseline (Fixed $50k, 0% cash yield)
	res1 := runSim(vooBars, uproBars, signalMap, 100000.0, false, 0.50, 0.0, 8, 0.05, "1. Fixed $50k Base (Current Rank #6)")

	// 2. Dynamic Compounding (50% of Total Current Portfolio Equity per trade)
	res2 := runSim(vooBars, uproBars, signalMap, 100000.0, true, 0.50, 0.0, 8, 0.05, "2. Dynamic Compounding (50% of Current Equity)")

	// 3. Dynamic Compounding + 4.5% Treasury Cash Yield on Idle Reserves (78.6% of time in cash!)
	res3 := runSim(vooBars, uproBars, signalMap, 100000.0, true, 0.50, 0.045, 8, 0.05, "3. Dynamic Compounding + 4.5% Cash Yield (Treasuries)")

	res4 := runSim(vooBars, uproBars, signalMap, 100000.0, true, 0.65, 0.045, 8, 0.05, "4. 65% Allocation + 4.5% Cash Yield")
	res5 := runSim(vooBars, uproBars, signalMap, 100000.0, true, 0.75, 0.045, 8, 0.05, "5. 75% Allocation + 4.5% Cash Yield")
	res6 := runSim(vooBars, uproBars, signalMap, 100000.0, true, 0.80, 0.045, 8, 0.05, "6. 80% Allocation + 4.5% Cash Yield")
	res7 := runSim(vooBars, uproBars, signalMap, 100000.0, true, 0.85, 0.045, 8, 0.05, "7. 85% Allocation + 4.5% Cash Yield")
	res8 := runSim(vooBars, uproBars, signalMap, 100000.0, true, 0.90, 0.045, 8, 0.05, "8. 90% Allocation + 4.5% Cash Yield")

	allRes := []SimResult{res1, res2, res3, res4, res5, res6, res7, res8}

	fmt.Printf("\n=====================================================================================================\n")
	fmt.Printf("🚀 STRATEGY ALLOCATION SCALING: FROM 50%% UP TO 90%% DYNAMIC ALLOCATION (+4.5%% CASH YIELD)\n")
	fmt.Printf("=====================================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Enhancement Strategy", "5-Year Profit ($)", "Ending Capital", "Total Return", "CAGR", "Max Drawdown", "DD < 11% Status"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, r := range allRes {
		status := "✅ SAFE (< 11%)"
		if r.MaxDD > 11.0 {
			status = "⚠️ Slightly Over"
		}
		table.Append([]string{
			r.Name,
			fmt.Sprintf("+$%.2f", r.NetProfit),
			fmt.Sprintf("$%.2f", r.EndingCapital),
			fmt.Sprintf("+%.2f%%", r.TotalReturn),
			fmt.Sprintf("%.2f%% / yr", r.CAGR),
			fmt.Sprintf("🔴 %.2f%%", r.MaxDD),
			status,
		})
	}
	table.Render()
}

type SimResult struct {
	Name          string
	NetProfit     float64
	EndingCapital float64
	TotalReturn   float64
	CAGR          float64
	MaxDD         float64
	Trades        int
	WinRate       float64
}

func runSim(vooBars, tradeBars []BarData, signals map[string]bool, startCap float64, dynamicCompound bool, allocPct, cashYieldAnn float64, holdDays int, tpPct float64, name string) SimResult {
	cash := startCap
	peakEquity := startCap
	maxDD := 0.0

	tradeIdx := 0
	inPosUntil := -1
	wins := 0
	totalTrades := 0

	dailyYieldRate := math.Pow(1.0+cashYieldAnn, 1.0/252.0) - 1.0

	for i := 0; i < len(tradeBars); i++ {
		// Daily cash interest
		if cash > 0 && cashYieldAnn > 0 {
			cash += cash * dailyYieldRate
		}

		b := tradeBars[i]
		if signals[b.Date] && i > inPosUntil {
			// Enter trade
			posCap := 50000.0
			if dynamicCompound {
				posCap = cash * allocPct
			}
			shares := int(posCap / b.Close)
			if shares > 0 {
				entryPrice := b.Close
				invested := float64(shares) * entryPrice
				cash -= invested

				exitIdx := i + holdDays
				if exitIdx >= len(tradeBars) {
					exitIdx = len(tradeBars) - 1
				}

				actualExitIdx := exitIdx
				actualExitPrice := tradeBars[actualExitIdx].Close
				targetPrice := entryPrice * (1.0 + tpPct)

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

				pnl := float64(shares) * (actualExitPrice - entryPrice)
				cash += invested + pnl
				if pnl > 0 {
					wins++
				}
				totalTrades++
				inPosUntil = actualExitIdx
			}
		}

		equity := cash
		if equity > peakEquity {
			peakEquity = equity
		}
		dd := (peakEquity - equity) / peakEquity
		if dd > maxDD {
			maxDD = dd
		}
		tradeIdx++
	}

	netProfit := cash - startCap
	totRet := (cash - startCap) / startCap * 100.0
	cagr := (math.Pow(cash/startCap, 1.0/5.0) - 1.0) * 100.0
	winRate := 0.0
	if totalTrades > 0 {
		winRate = float64(wins) / float64(totalTrades) * 100.0
	}

	return SimResult{
		Name:          name,
		NetProfit:     netProfit,
		EndingCapital: cash,
		TotalReturn:   totRet,
		CAGR:          cagr,
		MaxDD:         maxDD * 100.0,
		Trades:        totalTrades,
		WinRate:       winRate,
	}
}
