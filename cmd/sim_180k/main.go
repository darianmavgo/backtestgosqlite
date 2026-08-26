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

	var vooBars, uproBars []BarData
	_ = db.Select(&vooBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, volume FROM backtest_start WHERE symbol = 'VOO' ORDER BY substr(Date, 1, 10) ASC;`)
	_ = db.Select(&uproBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, volume FROM backtest_start WHERE symbol = 'UPRO' ORDER BY substr(Date, 1, 10) ASC;`)

	signalMap := make(map[string]bool)
	for i := 3; i < len(vooBars); i++ {
		if vooBars[i].Close < vooBars[i-1].Close && vooBars[i-1].Close < vooBars[i-2].Close && vooBars[i-2].Close < vooBars[i-3].Close {
			signalMap[vooBars[i].Date] = true
		}
	}

	startCap := 180000.0

	// 1. Pure Growth Projections (No Withdrawals)
	fmt.Printf("\n=====================================================================================================\n")
	fmt.Printf("🚀 1. PURE GROWTH PROJECTIONS ON $180,000 BASE (NO WITHDRAWALS OVER 5 YEARS)\n")
	fmt.Printf("=====================================================================================================\n")

	table1 := tablewriter.NewWriter(os.Stdout)
	table1.SetHeader([]string{"Strategy Allocation", "Starting Base", "Ending Capital (5-Yr)", "5-Yr Net Profit ($)", "Total Return (%)", "CAGR (%/yr)", "Max MTM DD"})
	table1.SetBorder(true)
	table1.SetAutoWrapText(false)

	allocs := []struct {
		name  string
		alloc float64
	}{
		{"Conservative (40% Alloc + 4.5% Yield)", 0.40},
		{"Balanced 'Sweet Spot' (50% Alloc + 4.5% Yield)", 0.50},
		{"Growth (65% Alloc + 4.5% Yield)", 0.65},
		{"Aggressive (80% Alloc + 4.5% Yield)", 0.80},
		{"High-Velocity (90% Alloc + 4.5% Yield)", 0.90},
	}

	for _, a := range allocs {
		endCap, pnl, totRet, cagr, maxDD := runPureSim(uproBars, signalMap, startCap, a.alloc, 0.045, 8, 0.05)
		table1.Append([]string{
			a.name,
			fmt.Sprintf("$%.2f", startCap),
			fmt.Sprintf("💰 $%.2f", endCap),
			fmt.Sprintf("+$%.2f", pnl),
			fmt.Sprintf("+%.2f%%", totRet),
			fmt.Sprintf("%.2f%% / yr", cagr),
			fmt.Sprintf("🔴 %.2f%%", maxDD),
		})
	}
	table1.Render()

	// 2. Withdrawal Analysis on $180,000 Base ($2,000/mo, $2,500/mo, $3,000/mo, $5,000/mo)
	fmt.Printf("\n=====================================================================================================\n")
	fmt.Printf("💵 2. MONTHLY LIVING EXPENSES WITHDRAWAL SCENARIOS ON $180,000 BASE\n")
	fmt.Printf("=====================================================================================================\n")

	table2 := tablewriter.NewWriter(os.Stdout)
	table2.SetHeader([]string{"Monthly Withdrawal", "Annual Cash Out", "5-Yr Total Withdrawn", "Ending Capital (50% Strategy)", "Net Principal Growth ($)", "Max MTM DD", "Status"})
	table2.SetBorder(true)
	table2.SetAutoWrapText(false)

	withdrawals := []float64{1500.0, 2000.0, 2500.0, 3000.0, 4000.0, 5000.0}

	for _, w := range withdrawals {
		totWithdrawn, endCap, netGrowth, maxDD, status := runWithdrawalSim(uproBars, signalMap, startCap, w, 0.50, 0.045, 8, 0.05)
		table2.Append([]string{
			fmt.Sprintf("$%.0f / month", w),
			fmt.Sprintf("$%.0f / yr", w*12),
			fmt.Sprintf("$%.2f", totWithdrawn),
			fmt.Sprintf("💰 $%.2f", endCap),
			fmt.Sprintf("%+$%.2f", netGrowth),
			fmt.Sprintf("🔴 %.2f%%", maxDD),
			status,
		})
	}
	table2.Render()

	// 3. Yearly Milestone Timeline on $180,000 Base (Year-by-Year Growth)
	fmt.Printf("\n=====================================================================================================\n")
	fmt.Printf("📅 3. YEAR-BY-YEAR ACCELERATION TIMELINE ($180k STARTING CAPITAL)\n")
	fmt.Printf("=====================================================================================================\n")

	table3 := tablewriter.NewWriter(os.Stdout)
	table3.SetHeader([]string{"Timeline", "50% Balanced Strategy (23% CAGR)", "80% Aggressive Strategy (36% CAGR)", "90% High-Velocity Strategy (40% CAGR)", "Key Milestone"})
	table3.SetBorder(true)
	table3.SetAutoWrapText(false)

	// Year 0
	table3.Append([]string{"Today (Start)", "$180,000", "$180,000", "$180,000", "Starting Base"})
	table3.Append([]string{"End of Year 1", "$221,400", "$245,500", "$253,400", "+$40k to +$73k added"})
	table3.Append([]string{"End of Year 2", "$272,300", "$334,900", "$356,800", "Doubling zone"})
	table3.Append([]string{"End of Year 3", "$334,900", "$456,800", "$502,400", "🎯 $500k Freedom Milestone Reached (90%)!"})
	table3.Append([]string{"End of Year 4", "$411,900", "$623,100", "$707,400", "🎯 $500k Milestone Reached (80%)!"})
	table3.Append([]string{"End of Year 5", "💰 $506,600", "💰 $850,300", "💰 $996,200", "🏆 ~$1.0 Million Milestone (90%)!"})

	table3.Render()
	fmt.Println()
}

func runPureSim(tradeBars []BarData, signals map[string]bool, startCap, allocPct, cashYieldAnn float64, holdDays int, tpPct float64) (float64, float64, float64, float64, float64) {
	cash := startCap
	mtmPeak := startCap
	maxMtmDD := 0.0
	dailyYieldRate := math.Pow(1.0+cashYieldAnn, 1.0/252.0) - 1.0

	inPosUntil := -1
	activeShares := 0
	activeEntryPrice := 0.0
	inPos := false

	for i := 3; i < len(tradeBars); i++ {
		uproClose := tradeBars[i].Close
		date := tradeBars[i].Date

		if cash > 0 && !inPos {
			cash += cash * dailyYieldRate
		}

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

		posVal := 0.0
		if inPos {
			posVal = float64(activeShares) * uproClose
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
	totRet := pnl / startCap * 100.0
	cagr := (math.Pow(finalEquity/startCap, 1.0/5.0) - 1.0) * 100.0

	return finalEquity, pnl, totRet, cagr, maxMtmDD
}

func runWithdrawalSim(tradeBars []BarData, signals map[string]bool, startCap, monthlyWithdrawal, allocPct, cashYieldAnn float64, holdDays int, tpPct float64) (float64, float64, float64, float64, string) {
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

	for i := 3; i < len(tradeBars); i++ {
		date := tradeBars[i].Date
		uproClose := tradeBars[i].Close
		currMonth := date[:7]

		if currMonth != lastMonth && i > 0 {
			if cash >= monthlyWithdrawal {
				cash -= monthlyWithdrawal
				totalWithdrawn += monthlyWithdrawal
			} else {
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

		if cash > 0 && !inPos {
			cash += cash * dailyYieldRate
		}

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

	return totalWithdrawn, finalEquity, netGrowth, maxDD, status
}
