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

type WithdrawalResult struct {
	StrategyName      string
	StartingCapital   float64
	MonthlyWithdrawal float64
	TotalWithdrawn    float64
	EndingCapital     float64
	NetCapitalGrowth  float64
	MaxDrawdownMTM    float64
	SurvivalStatus    string
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

	monthlyWithdrawal := 5000.0 // $5,000 / month ($60k / year)
	startingBases := []float64{400000.0, 500000.0, 750000.0, 1000000.0}

	fmt.Printf("\n=====================================================================================================\n")
	fmt.Printf("💵 LIVING EXPENSES WITHDRAWAL ANALYSIS ($5,000 / MONTH = $60,000 / YEAR OVER 5 YEARS)\n")
	fmt.Printf("=====================================================================================================\n\n")

	for _, startCap := range startingBases {
		fmt.Printf("📌 STARTING PRINCIPAL BASE: $%.0f (Total Withdrawn Over 5 Years: $300,000.00)\n", startCap)
		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"Strategy", "Total Withdrawn", "Ending Capital", "Net Capital Growth ($)", "Growth %", "Max MTM DD", "Status"})
		table.SetBorder(true)
		table.SetAutoWrapText(false)

		// 1. Passive VOO Buy & Hold (Extracting $5k/mo)
		resVOO := runVOOWithWithdrawal(vooBars, startCap, monthlyWithdrawal)

		// 2. Ultra-Conservative 30% Dynamic UPRO + 4.5% Treasury Yield (Extracting $5k/mo)
		resStrat30 := runStrategyWithWithdrawal(uproBars, signalMap, startCap, monthlyWithdrawal, 0.30, 0.045, 8, 0.05, "🛡️ 30% UPRO Dip + 4.5% T-Bills")

		// 3. Balanced 50% Dynamic UPRO + 4.5% Treasury Yield (Extracting $5k/mo)
		resStrat50 := runStrategyWithWithdrawal(uproBars, signalMap, startCap, monthlyWithdrawal, 0.50, 0.045, 8, 0.05, "⭐️ 50% UPRO Dip + 4.5% T-Bills")

		for _, r := range []WithdrawalResult{resVOO, resStrat30, resStrat50} {
			growthStr := fmt.Sprintf("%+0.2f%%", r.NetCapitalGrowth/startCap*100.0)
			table.Append([]string{
				r.StrategyName,
				fmt.Sprintf("$%.2f", r.TotalWithdrawn),
				fmt.Sprintf("💰 $%.2f", r.EndingCapital),
				fmt.Sprintf("%+$%.2f", r.NetCapitalGrowth),
				growthStr,
				fmt.Sprintf("🔴 %.2f%%", r.MaxDrawdownMTM),
				r.SurvivalStatus,
			})
		}
		table.Render()
		fmt.Println()
	}
}

func runVOOWithWithdrawal(bars []BarData, startCap, monthlyWithdrawal float64) WithdrawalResult {
	cash := 0.0
	shares := startCap / bars[0].Close
	totalWithdrawn := 0.0

	peakVal := startCap
	maxDD := 0.0
	lastMonth := ""

	for i := 0; i < len(bars); i++ {
		currMonth := bars[i].Date[:7]
		// Withdraw on 1st trading day of new month
		if currMonth != lastMonth && i > 0 {
			// Sell shares to extract $5,000 cash
			sharesToSell := monthlyWithdrawal / bars[i].Close
			if shares >= sharesToSell {
				shares -= sharesToSell
				totalWithdrawn += monthlyWithdrawal
			} else {
				// Run out of shares
				totalWithdrawn += shares * bars[i].Close
				shares = 0
			}
			lastMonth = currMonth
		}

		equity := shares * bars[i].Close + cash
		if equity > peakVal {
			peakVal = equity
		}
		dd := (peakVal - equity) / peakVal * 100.0
		if dd > maxDD {
			maxDD = dd
		}
	}

	finalEquity := shares*bars[len(bars)-1].Close + cash
	netGrowth := finalEquity - startCap
	status := "🟢 Principal Grew"
	if netGrowth < 0 {
		status = "🔴 Principal Depleted"
	}

	return WithdrawalResult{
		StrategyName:      "🏛️ VOO Buy & Hold (Passive)",
		StartingCapital:   startCap,
		MonthlyWithdrawal: monthlyWithdrawal,
		TotalWithdrawn:    totalWithdrawn,
		EndingCapital:     finalEquity,
		NetCapitalGrowth:  netGrowth,
		MaxDrawdownMTM:    maxDD,
		SurvivalStatus:    status,
	}
}

func runStrategyWithWithdrawal(tradeBars []BarData, signals map[string]bool, startCap, monthlyWithdrawal, allocPct, cashYieldAnn float64, holdDays int, tpPct float64, name string) WithdrawalResult {
	cash := startCap
	totalWithdrawn := 0.0

	peakEquity := startCap
	maxDD := 0.0

	dailyYieldRate := math.Pow(1.0+cashYieldAnn, 1.0/252.0) - 1.0

	inPosUntil := -1
	activeShares := 0
	activeEntryPrice := 0.0
	inPos := false
	lastMonth := ""

	for i := 0; i < len(tradeBars); i++ {
		date := tradeBars[i].Date
		uproClose := tradeBars[i].Close
		currMonth := date[:7]

		// Monthly Withdrawal from Cash Reserve on 1st of month
		if currMonth != lastMonth && i > 0 {
			if cash >= monthlyWithdrawal {
				cash -= monthlyWithdrawal
				totalWithdrawn += monthlyWithdrawal
			} else {
				// Take from active position if cash low
				needed := monthlyWithdrawal - cash
				totalWithdrawn += cash
				cash = 0
				if inPos && activeShares > 0 {
					sharesToLiquidate := int(needed / uproClose)
					if sharesToLiquidate > activeShares {
						sharesToLiquidate = activeShares
					}
					activeShares -= sharesToLiquidate
					totalWithdrawn += float64(sharesToLiquidate) * uproClose
				}
			}
			lastMonth = currMonth
		}

		// Earn daily cash yield on uninvested cash
		if cash > 0 && !inPos {
			cash += cash * dailyYieldRate
		}

		// Check Entry Signal
		if signals[date] && i > inPosUntil && !inPos && cash > 1000.0 {
			posCapital := cash * allocPct
			shares := int(posCapital / uproClose)
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
				targetPrice := activeEntryPrice * (1.0 + tpPct)

				for d := i + 1; d <= exitIdx; d++ {
					if tradeBars[d].High >= targetPrice {
						actualExitIdx = d
						break
					}
				}
				inPosUntil = actualExitIdx
			}
		}

		// Check Exit
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
		}

		// Equity
		posVal := 0.0
		if inPos {
			posVal = float64(activeShares) * uproClose
		}
		currentEquity := cash + posVal

		if currentEquity > peakEquity {
			peakEquity = currentEquity
		}
		dd := (peakEquity - currentEquity) / peakEquity * 100.0
		if dd > maxDD {
			maxDD = dd
		}
	}

	posVal := 0.0
	if inPos {
		posVal = float64(activeShares) * tradeBars[len(tradeBars)-1].Close
	}
	finalEquity := cash + posVal
	netGrowth := finalEquity - startCap
	status := "🟢 Principal Grew"
	if netGrowth < 0 {
		status = "🔴 Principal Depleted"
	}

	return WithdrawalResult{
		StrategyName:      name,
		StartingCapital:   startCap,
		MonthlyWithdrawal: monthlyWithdrawal,
		TotalWithdrawn:    totalWithdrawn,
		EndingCapital:     finalEquity,
		NetCapitalGrowth:  netGrowth,
		MaxDrawdownMTM:    maxDD,
		SurvivalStatus:    status,
	}
}
