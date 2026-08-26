package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	"github.com/darianmavgo/backtestgosqlite/internal/strategy"
	"github.com/olekukonko/tablewriter"
)

func main() {
	db, err := storage.OpenSQLite("data/live_scan.db")
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	barsBySymbol, sortedDates, err := storage.FetchAllBarsChronological(db)
	if err != nil {
		log.Fatalf("Failed to fetch bars: %v", err)
	}

	if len(sortedDates) == 0 {
		log.Fatalf("No dates found")
	}

	lastDate := sortedDates[len(sortedDates)-1]
	prevDate := ""
	if len(sortedDates) >= 2 {
		prevDate = sortedDates[len(sortedDates)-2]
	}

	fmt.Printf("\n========================================================================\n")
	fmt.Printf("🔍 MARKET SCAN AS OF LATEST DATE: %s (Prev Date: %s)\n", lastDate, prevDate)
	fmt.Printf("========================================================================\n\n")

	// 1. Inspect recent status for each symbol
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Symbol", "Last Date", "Close", "Decline Streak", "RSI(2)", "RSI(5)", "RSI(14)", "Lower BB(20)", "SMA(50)", "MinLow(3-12)", "WC Cliff %"})
	table.SetBorder(true)

	for sym, bars := range barsBySymbol {
		if len(bars) < 55 {
			continue
		}
		n := len(bars)
		lastBar := bars[n-1]
		rsi2 := strategy.CalcRSI(bars, 2)
		rsi5 := strategy.CalcRSI(bars, 5)
		rsi14 := strategy.CalcRSI(bars, 14)
		bb := strategy.CalcBollinger(bars, 20, 2.0)
		sma50 := strategy.CalcSMA(bars, 50)

		// Current decline streak
		declineStreak := 0
		for j := n - 1; j >= 1; j-- {
			if bars[j].Close < bars[j-1].Close {
				declineStreak++
			} else {
				break
			}
		}

		// Min low Day -3 to -12
		minLow := bars[n-3].Low
		for j := 4; j <= 12 && n-j >= 0; j++ {
			if bars[n-j].Low < minLow {
				minLow = bars[n-j].Low
			}
		}
		cliffPct := (lastBar.Close - minLow) / minLow * 100.0

		table.Append([]string{
			sym,
			lastBar.Date,
			fmt.Sprintf("$%.2f", lastBar.Close),
			fmt.Sprintf("%d days", declineStreak),
			fmt.Sprintf("%.1f", rsi2[n-1]),
			fmt.Sprintf("%.1f", rsi5[n-1]),
			fmt.Sprintf("%.1f", rsi14[n-1]),
			fmt.Sprintf("$%.2f", bb.Lower[n-1]),
			fmt.Sprintf("$%.2f", sma50[n-1]),
			fmt.Sprintf("$%.2f", minLow),
			fmt.Sprintf("%.1f%%", cliffPct),
		})
	}
	table.Render()

	// 2. Check signals across all registered strategies on the latest 3 trading dates
	fmt.Printf("\n========================================================================\n")
	fmt.Printf("🎯 STRATEGY SIGNALS GENERATED ON RECENT BARS (Last 3 Dates)\n")
	fmt.Printf("========================================================================\n")

	recentDates := sortedDates[len(sortedDates)-3:]
	recentDatesMap := make(map[string]bool)
	for _, d := range recentDates {
		recentDatesMap[d] = true
	}

	sigTable := tablewriter.NewWriter(os.Stdout)
	sigTable.SetHeader([]string{"Date", "Strategy ID", "Strategy Name", "Symbol", "Close Price", "Default Target (+%)", "Default Stop (-%)", "Max Hold"})
	sigTable.SetBorder(true)

	allStrats := strategy.List()
	totalRecentSignals := 0

	for _, s := range allStrats {
		if strings.HasSuffix(s.ID(), "-sql") || s.ID() == "buy-and-hold" {
			continue
		}
		signals := s.GenerateSignals(barsBySymbol)
		cfg := s.DefaultConfig()

		for _, sig := range signals {
			if recentDatesMap[sig.Date] {
				totalRecentSignals++
				sigTable.Append([]string{
					sig.Date,
					s.ID(),
					s.Name(),
					sig.Symbol,
					fmt.Sprintf("$%.2f", sig.Close),
					fmt.Sprintf("+%.1f%% ($%.2f)", (cfg.TargetPct-1)*100, sig.Close*cfg.TargetPct),
					fmt.Sprintf("-%.1f%% ($%.2f)", (1-cfg.StopLossPct)*100, sig.Close*cfg.StopLossPct),
					fmt.Sprintf("%dd", cfg.HoldingWindow),
				})
			}
		}
	}

	if totalRecentSignals == 0 {
		fmt.Println("No active strategy signals generated on the most recent 3 dates.")
	} else {
		sigTable.Render()
	}
}
