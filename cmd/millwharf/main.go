package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/darianmavgo/backtestgosqlite/internal/simulator"
	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	"github.com/darianmavgo/backtestgosqlite/internal/strategy"
)

func main() {
	dbPath := flag.String("db", "data/leveraged_backtest.db", "Path to SQLite database containing backtest_start table")
	startDate := flag.String("start", "", "Optional start date filter for analysis (YYYY-MM-DD)")
	minStreak := flag.Int("min-streak", 5, "Minimum consecutive decline days to display")
	topN := flag.Int("top", 25, "Number of top longest declines to display")
	runBacktest := flag.Bool("backtest", true, "Run full portfolio backtest on Millwharf strategy")
	capital := flag.Float64("capital", 100000.0, "Starting portfolio capital for backtest simulation")
	flag.Parse()

	// Fall back to live_scan.db if target DB does not exist
	targetDB := *dbPath
	if _, err := os.Stat(targetDB); os.IsNotExist(err) {
		targetDB = "data/live_scan.db"
	}

	db, err := storage.OpenSQLite(targetDB)
	if err != nil {
		log.Fatalf("Failed to open SQLite database %s: %v", targetDB, err)
	}
	defer db.Close()

	barsBySymbol, sortedDates, err := storage.FetchAllBarsChronological(db)
	if err != nil {
		log.Fatalf("Failed to fetch historical bars: %v", err)
	}

	if len(sortedDates) == 0 {
		log.Fatalf("No historical date data found in %s", targetDB)
	}

	firstDate := sortedDates[0]
	latestDate := sortedDates[len(sortedDates)-1]

	fmt.Printf("\n========================================================================================\n")
	fmt.Printf("📉 MILLWHARF STRATEGY: WEEKLY CONSISTENT DECLINE REVERSAL\n")
	fmt.Printf("🔎 DATASET HORIZON: %s ➔ %s (%d trading dates, %d ETF symbols in universe)\n",
		firstDate, latestDate, len(sortedDates), len(barsBySymbol))
	fmt.Printf("   Source DB:       %s\n", targetDB)
	fmt.Printf("========================================================================================\n\n")

	// 1. Search for longest consistent declines across universe
	streaks := strategy.FindLongestDeclines(barsBySymbol, *startDate, *minStreak)

	filterDesc := "Full 4-Year Horizon"
	if *startDate != "" {
		filterDesc = fmt.Sprintf("Since %s", *startDate)
	}
	fmt.Printf("🏆 LONGEST CONSISTENT DECLINES (Each Close < Previous Close, %s):\n\n", filterDesc)

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Rank", "Symbol", "Decline Streak", "Peak Date", "Trough Date", "Peak Close", "Trough Close", "Total Drop %"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	limit := len(streaks)
	if *topN > 0 && *topN < limit {
		limit = *topN
	}

	for i := 0; i < limit; i++ {
		s := streaks[i]
		table.Append([]string{
			fmt.Sprintf("#%d", i+1),
			s.Symbol,
			fmt.Sprintf("%d consecutive days", s.StreakDays),
			s.StartDate,
			s.EndDate,
			fmt.Sprintf("$%.2f", s.StartClose),
			fmt.Sprintf("$%.2f", s.EndClose),
			fmt.Sprintf("%.2f%%", s.TotalDropPct),
		})
	}
	table.Render()

	// 2. Millwharf Strategy Signals
	strat, _ := strategy.Get("millwharf")
	signals := strat.GenerateSignals(barsBySymbol)

	var filteredSignals []models.Signal
	for _, sig := range signals {
		if *startDate == "" || sig.Date >= *startDate {
			filteredSignals = append(filteredSignals, sig)
		}
	}

	fmt.Printf("\n🎯 WEEKLY SELECTED POSITIONS (1 per week, Longest Decline >= 5 days):\n")
	fmt.Printf("   Total Weekly Positions Initiated: %d\n\n", len(filteredSignals))

	if len(filteredSignals) > 0 {
		sigTable := tablewriter.NewWriter(os.Stdout)
		sigTable.SetHeader([]string{"Entry Date", "Symbol", "Entry Price", "Streak", "Drop %", "6-Day High", "Take Profit", "Target Gain %"})
		sigTable.SetBorder(true)

		displayCount := len(filteredSignals)
		if displayCount > 20 {
			displayCount = 20
		}

		for _, sig := range filteredSignals[:displayCount] {
			streakLen := int(sig.Metadata["decline_streak"])
			dropPct := sig.Metadata["total_drop_pct"]
			high6d := sig.Metadata["high_6d"]
			tpGainPct := sig.Metadata["target_pct"] * 100.0

			sigTable.Append([]string{
				sig.Date,
				sig.Symbol,
				fmt.Sprintf("$%.2f", sig.Close),
				fmt.Sprintf("%d days", streakLen),
				fmt.Sprintf("%.2f%%", dropPct),
				fmt.Sprintf("$%.2f", high6d),
				fmt.Sprintf("$%.2f", sig.TakeProfit),
				fmt.Sprintf("+%.2f%%", tpGainPct),
			})
		}
		sigTable.Render()
		if len(filteredSignals) > 20 {
			fmt.Printf("... and %d more weekly trade entries (showing first 20).\n", len(filteredSignals)-20)
		}
	}

	// 3. Portfolio Backtest Simulation
	if *runBacktest {
		cfg := strat.DefaultConfig()
		fmt.Printf("\n🚀 RUNNING PORTFOLIO BACKTEST SIMULATION:\n")
		fmt.Printf("   Capital:         $%.2f\n", *capital)
		fmt.Printf("   Position Cap:    %d concurrent positions (20%% equity per position)\n", cfg.PositionCap)
		fmt.Printf("   Take Profit:     Min(6-Day High, Entry Price * 1.20)\n")
		fmt.Printf("   Stop Loss:       None (0%%)\n")
		fmt.Printf("   Holding Window:  4 days (Exit at Market Open on Day 4)\n")

		sim := simulator.NewPortfolioSimulator(cfg, *capital)
		rep, trades, _ := sim.Run(signals, barsBySymbol, sortedDates)

		fmt.Printf("\n========================================================================================\n")
		fmt.Printf("📊 QUANTITATIVE PORTFOLIO TEAR SHEET: MILLWHARF STRATEGY\n")
		fmt.Printf("📅 BACKTEST TIME WINDOW: %s ➔ %s (%.1f Years | %d Trading Days)\n",
			rep.StartDate, rep.EndDate, rep.TotalCalendarYears, rep.TotalTradingDays)
		fmt.Printf("========================================================================================\n")

		resTable := tablewriter.NewWriter(os.Stdout)
		resTable.SetHeader([]string{"Metric", "Value", "Context"})
		resTable.SetBorder(true)

		resTable.Append([]string{"Initial Capital", fmt.Sprintf("$%.2f", rep.InitialCapital), "Starting cash"})
		resTable.Append([]string{"Ending Total Equity", fmt.Sprintf("$%.2f", rep.FinalEquity), "Cash + open positions"})
		resTable.Append([]string{"Net Realized Profit", fmt.Sprintf("$%.2f", rep.NetProfit), fmt.Sprintf("%.2f%% total return", rep.TotalReturnPct*100)})
		resTable.Append([]string{"CAGR (Annualized Return)", fmt.Sprintf("%.2f%%", rep.CAGR*100), "Compound Annual Growth Rate"})
		resTable.Append([]string{"Sharpe Ratio (Annualized)", fmt.Sprintf("%.2f", rep.SharpeRatio), "Risk-adjusted return"})
		resTable.Append([]string{"Sortino Ratio", fmt.Sprintf("%.2f", rep.SortinoRatio), "Downside risk-adjusted"})
		resTable.Append([]string{"Calmar Ratio", fmt.Sprintf("%.2f", rep.CalmarRatio), "CAGR / Max Drawdown"})
		resTable.Append([]string{"🔴 MAX DRAWDOWN (MDD %)", fmt.Sprintf("%.2f%%", rep.MaxDrawdownPct*100), "Worst peak-to-trough drop"})
		resTable.Append([]string{"🔴 MAX DRAWDOWN ($)", fmt.Sprintf("-$%.2f", rep.MaxDrawdownDollars), fmt.Sprintf("Peak: $%.2f ➔ Trough: $%.2f", rep.MaxDrawdownPeakEquity, rep.MaxDrawdownTroughEquity)})
		resTable.Append([]string{"🔴 MAX DRAWDOWN DATES", fmt.Sprintf("%s ➔ %s", rep.MaxDrawdownPeakDate, rep.MaxDrawdownTroughDate), fmt.Sprintf("Duration: %d days", rep.MaxDrawdownDuration)})
		resTable.Append([]string{"Total Completed Trades", fmt.Sprintf("%d", rep.TotalTrades), fmt.Sprintf("%d Wins / %d Losses", rep.WinningTrades, rep.LosingTrades)})
		resTable.Append([]string{"Trade Win Rate", fmt.Sprintf("%.2f%%", rep.WinRate*100), "Pct of winning trades"})
		resTable.Append([]string{"Profit Factor", fmt.Sprintf("%.2f", rep.ProfitFactor), "Gross profits / Gross losses"})
		resTable.Append([]string{"Win / Loss Payoff Ratio", fmt.Sprintf("%.2f", rep.PayoffRatio), "Avg win / Avg loss"})
		resTable.Append([]string{"Average Win", fmt.Sprintf("$%.2f", rep.AvgWinAmount), "Per winning trade"})
		resTable.Append([]string{"Average Loss", fmt.Sprintf("$%.2f", rep.AvgLossAmount), "Per losing trade"})
		resTable.Append([]string{"Average Holding Period", fmt.Sprintf("%.1f days", rep.AvgHoldingDays), "Holding horizon"})
		resTable.Append([]string{"Total Commissions Paid", fmt.Sprintf("$%.2f", rep.TotalCommissionPaid), "Broker costs deducted"})

		resTable.Render()

		if len(trades) > 0 {
			fmt.Printf("\nCompleted Trades Sample (Total: %d trades):\n", len(trades))
			tTable := tablewriter.NewWriter(os.Stdout)
			tTable.SetHeader([]string{"ID", "Symbol", "Entry Date", "Entry $", "Exit Date", "Exit $", "Hold Days", "Exit Reason", "Net PnL", "Return %"})
			tTable.SetBorder(true)

			startIdx := 0
			if len(trades) > 20 {
				startIdx = len(trades) - 20
			}

			for _, t := range trades[startIdx:] {
				tTable.Append([]string{
					fmt.Sprintf("%d", t.ID),
					t.Symbol,
					t.EntryDate,
					fmt.Sprintf("$%.2f", t.EntryPrice),
					t.ExitDate,
					fmt.Sprintf("$%.2f", t.ExitPrice),
					fmt.Sprintf("%d", t.HoldDays),
					string(t.ExitReason),
					fmt.Sprintf("$%.2f", t.NetPnL),
					fmt.Sprintf("%.2f%%", t.ReturnPct*100),
				})
			}
			tTable.Render()
		}
	}
}
