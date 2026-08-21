package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
	"github.com/darianmavgo/backtestgosqlite/internal/analytics"
	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/darianmavgo/backtestgosqlite/internal/simulator"
	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	"github.com/darianmavgo/backtestgosqlite/internal/strategy"
)

func printPerformanceTearSheet(strategyName string, report models.PerformanceReport) {
	fmt.Printf("\n========================================================================================\n")
	fmt.Printf("📊 QUANTITATIVE PORTFOLIO TEAR SHEET: %s\n", strings.ToUpper(strategyName))
	fmt.Printf("📅 BACKTEST TIME WINDOW: %s ➔ %s (%.1f Years | %d Trading Days)\n",
		report.StartDate, report.EndDate, report.TotalCalendarYears, report.TotalTradingDays)
	fmt.Printf("========================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Metric", "Value", "Benchmark / Context"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	// Time Window
	table.Append([]string{
		"Backtest Time Window",
		fmt.Sprintf("%s to %s", report.StartDate, report.EndDate),
		fmt.Sprintf("%d trading days (%.1f years)", report.TotalTradingDays, report.TotalCalendarYears),
	})

	// Capital & Returns
	table.Append([]string{"Initial Capital", fmt.Sprintf("$%.2f", report.InitialCapital), "Starting portfolio cash"})
	table.Append([]string{"Ending Total Equity", fmt.Sprintf("$%.2f", report.FinalEquity), "Cash + open positions"})
	table.Append([]string{"Net Realized Profit", fmt.Sprintf("$%.2f", report.NetProfit), fmt.Sprintf("%.2f%% total return", report.TotalReturnPct*100)})
	table.Append([]string{"CAGR (Annualized Return)", fmt.Sprintf("%.2f%%", report.CAGR*100), "Compound Annual Growth Rate"})
	table.Append([]string{"Sharpe Ratio (Annualized)", fmt.Sprintf("%.2f", report.SharpeRatio), "Risk-adjusted return vs. 0% Rf"})
	table.Append([]string{"Sortino Ratio (Annualized)", fmt.Sprintf("%.2f", report.SortinoRatio), "Downside volatility adjusted"})

	// Highlighted Max Drawdown Details
	table.Append([]string{
		"🔴 MAX DRAWDOWN (MDD %)",
		fmt.Sprintf("%.2f%%", report.MaxDrawdownPct*100),
		fmt.Sprintf("Worst account decline from peak equity"),
	})
	table.Append([]string{
		"🔴 MAX DRAWDOWN ($ LOSS)",
		fmt.Sprintf("-$%.2f", report.MaxDrawdownDollars),
		fmt.Sprintf("Peak: $%.2f ➔ Trough: $%.2f", report.MaxDrawdownPeakEquity, report.MaxDrawdownTroughEquity),
	})
	table.Append([]string{
		"🔴 MAX DRAWDOWN DATES",
		fmt.Sprintf("%s ➔ %s", report.MaxDrawdownPeakDate, report.MaxDrawdownTroughDate),
		fmt.Sprintf("Longest drawdown duration: %d days", report.MaxDrawdownDuration),
	})

	// Trade-Level Performance
	table.Append([]string{"Total Completed Trades", fmt.Sprintf("%d", report.TotalTrades), fmt.Sprintf("%d Wins / %d Losses", report.WinningTrades, report.LosingTrades)})
	table.Append([]string{"Trade Win Rate", fmt.Sprintf("%.2f%%", report.WinRate*100), "Pct of closed trades in profit"})
	table.Append([]string{"Profit Factor", fmt.Sprintf("%.2f", report.ProfitFactor), "Gross Profits / Gross Losses"})
	table.Append([]string{"Win / Loss Payoff Ratio", fmt.Sprintf("%.2f", report.PayoffRatio), "Avg Win $ / Avg Loss $"})
	table.Append([]string{"Average Win", fmt.Sprintf("$%.2f", report.AvgWinAmount), "Per winning trade"})
	table.Append([]string{"Average Loss", fmt.Sprintf("$%.2f", report.AvgLossAmount), "Per losing trade"})
	table.Append([]string{"Average Holding Period", fmt.Sprintf("%.1f days", report.AvgHoldingDays), "Holding horizon"})
	table.Append([]string{"Total Commissions & Fees", fmt.Sprintf("$%.2f", report.TotalCommissionPaid), "Exchange / broker costs deducted"})

	table.Render()
}

func listStrategies() {
	fmt.Printf("\n========================================================================================================================\n")
	fmt.Printf("📋 REGISTERED TRADING STRATEGIES (GO & SQL PIPELINES)\n")
	fmt.Printf("========================================================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "Type", "Strategy Name", "Default Target", "Default Stop", "Hold", "Description"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, s := range strategy.List() {
		cfg := s.DefaultConfig()
		sType := "Go"
		if strings.HasSuffix(s.ID(), "-sql") {
			sType = "SQL Pipeline"
		}
		table.Append([]string{
			s.ID(),
			sType,
			s.Name(),
			fmt.Sprintf("+%.1f%%", (cfg.TargetPct-1)*100),
			fmt.Sprintf("-%.1f%%", (1-cfg.StopLossPct)*100),
			fmt.Sprintf("%dd", cfg.HoldingWindow),
			s.Description(),
		})
	}
	table.Render()
	fmt.Printf("\nRun any strategy with: ./bin/backtest -strategy <ID>\n\n")
}

func main() {
	targetDb := flag.String("db", "data/wc_master_backtest.db", "Path to target SQLite DB containing backtest_start table")
	strategyType := flag.String("strategy", "bb-capitulation", "Strategy ID to run (e.g. bb-capitulation, trend-bb, rsi2, wc, whitings_creek-sql)")
	listFlag := flag.Bool("list", false, "List all registered Go and SQL strategies")
	symbolFilter := flag.String("symbol", "", "Optional: Filter backtest to a specific symbol (e.g. DFEN, SOXL)")
	capital := flag.Float64("capital", 100000.0, "Starting portfolio capital for simulation")
	maxPositions := flag.Int("max-positions", 0, "Optional override: Maximum concurrent open positions allowed")
	stopLoss := flag.Float64("stoploss", 0.0, "Optional override: Stop-loss floor multiplier (e.g. 0.93 for -7%)")
	profitTarget := flag.Float64("target", 0.0, "Optional override: Take-profit multiplier (e.g. 1.18 for +18%)")
	holdWindow := flag.Int("hold", 0, "Optional override: Max holding days window")
	htmlOutput := flag.String("html", "reports/backtest_report.html", "Path to export interactive HTML dashboard report")
	flag.Parse()

	// Auto-discover any SQL pipeline strategies in sql/strategies/
	strategy.AutoRegisterSQLStrategies(".")

	if *listFlag {
		listStrategies()
		return
	}

	strat, exists := strategy.Get(*strategyType)
	if !exists {
		log.Fatalf("Strategy '%s' not found in registry. Run with -list to view available strategies.", *strategyType)
	}

	cfg := strat.DefaultConfig()
	if *stopLoss > 0 {
		cfg.StopLossPct = *stopLoss
	}
	if *profitTarget > 0 {
		cfg.TargetPct = *profitTarget
	}
	if *holdWindow > 0 {
		cfg.HoldingWindow = *holdWindow
	}
	if *maxPositions > 0 {
		cfg.PositionCap = *maxPositions
	}

	fmt.Printf("\n========================================================================================\n")
	fmt.Printf("🎯 STRATEGY SELECTED: %s (ID: %s)\n", strat.Name(), strat.ID())
	fmt.Printf("   Description:  %s\n", strat.Description())
	fmt.Printf("   Target:       +%.1f%% | Stop-Loss: -%.1f%% | Max Hold: %d days | Max Positions: %d\n",
		(cfg.TargetPct-1)*100, (1-cfg.StopLossPct)*100, cfg.HoldingWindow, cfg.PositionCap)
	fmt.Printf("========================================================================================\n")

	db, err := storage.OpenSQLite(*targetDb)
	if err != nil {
		log.Fatalf("Failed to open target DB %s: %v", *targetDb, err)
	}
	defer db.Close()

	fmt.Printf("\n⚙️ Loading chronological bars for Portfolio Simulation (Starting Capital: $%.2f)...\n", *capital)
	barsBySymbol, sortedDates, err := storage.FetchAllBarsChronological(db)
	if err != nil {
		log.Fatalf("Error loading historical bars for simulation: %v", err)
	}

	// Generate signals from the selected strategy
	signals := strat.GenerateSignals(barsBySymbol)

	symUpper := strings.ToUpper(*symbolFilter)
	if symUpper != "" {
		var filteredSignals []models.Signal
		for _, s := range signals {
			if strings.ToUpper(s.Symbol) == symUpper {
				filteredSignals = append(filteredSignals, s)
			}
		}
		signals = filteredSignals
		fmt.Printf("Filtered to %d entry signals for symbol %s\n", len(signals), symUpper)
	} else {
		fmt.Printf("Generated %d total entry signals across %d symbols over %d trading dates.\n",
			len(signals), len(barsBySymbol), len(sortedDates))
	}

	sim := simulator.NewPortfolioSimulator(cfg, *capital)
	report, trades, equityCurve := sim.Run(signals, barsBySymbol, sortedDates)

	printPerformanceTearSheet(strat.Name(), report)

	if len(trades) > 0 {
		fmt.Printf("\nCompleted Trades for Simulation (Total: %d):\n", len(trades))
		tTable := tablewriter.NewWriter(os.Stdout)
		tTable.SetHeader([]string{"ID", "Symbol", "Entry Date", "Entry $", "Exit Date", "Exit $", "Hold Days", "Exit Reason", "Net PnL", "Return %"})
		tTable.SetBorder(true)

		startIdx := 0
		if *symbolFilter == "" && len(trades) > 20 {
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

	// Export HTML Report
	if *htmlOutput != "" {
		sType := "Go"
		if strings.HasSuffix(strat.ID(), "-sql") {
			sType = "SQL"
		}

		var eqSeries []float64
		var ddSeries []float64
		for _, pt := range equityCurve {
			eqSeries = append(eqSeries, pt.TotalEquity)
			ddSeries = append(ddSeries, pt.DrawdownPct)
		}

		reportTitle := fmt.Sprintf("%s Performance Report", strat.Name())
		if symUpper != "" {
			reportTitle = fmt.Sprintf("%s Performance Report for %s", strat.Name(), symUpper)
		}

		htmlData := analytics.MultiStrategyHTMLData{
			Title:      reportTitle,
			Symbol:     symUpper,
			StartDate:  report.StartDate,
			EndDate:    report.EndDate,
			TotalDays:  report.TotalTradingDays,
			TotalYears: report.TotalCalendarYears,
			InitialCap: *capital,
			Strategies: []analytics.StrategyReportData{
				{
					ID:     strat.ID(),
					Name:   strat.Name(),
					Type:   sType,
					Report: report,
					Trades: trades,
				},
			},
			AllDates: sortedDates,
			EquityCurves: map[string][]float64{
				strat.ID(): eqSeries,
			},
			DrawdownCurves: map[string][]float64{
				strat.ID(): ddSeries,
			},
		}

		err := analytics.GenerateComparisonHTML(*htmlOutput, htmlData)
		if err != nil {
			log.Printf("Warning: Failed to generate HTML report %s: %v", *htmlOutput, err)
		} else {
			fmt.Printf("\n✨ Interactive HTML Report generated: %s\n\n", *htmlOutput)
		}
	}
}
