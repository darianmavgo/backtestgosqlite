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

type CompareResult struct {
	ID     string
	Name   string
	Type   string
	Report models.PerformanceReport
	Trades []models.Trade
	Curve  []models.DailyEquityPoint
}

func main() {
	targetDb := flag.String("db", "data/wc_master_backtest.db", "Path to target SQLite DB")
	capital := flag.Float64("capital", 100000.0, "Starting portfolio capital for simulation")
	symbolFilter := flag.String("symbol", "", "Optional: Filter comparison to a specific symbol (e.g. DFEN, SOXL, TQQQ)")
	htmlOutput := flag.String("html", "reports/comparison_report.html", "Path to export interactive HTML dashboard report")
	flag.Parse()

	// Auto-discover any SQL pipeline strategies in sql/strategies/
	strategy.AutoRegisterSQLStrategies(".")

	db, err := storage.OpenSQLite(*targetDb)
	if err != nil {
		log.Fatalf("Failed to open DB %s: %v", *targetDb, err)
	}
	defer db.Close()

	fmt.Printf("📥 Loading historical data from %s...\n", *targetDb)
	barsBySymbol, sortedDates, err := storage.FetchAllBarsChronological(db)
	if err != nil {
		log.Fatalf("Failed to fetch historical bars: %v", err)
	}

	symUpper := strings.ToUpper(*symbolFilter)
	reportTitle := "Multi-Strategy Benchmark (Full Universe)"
	if symUpper != "" {
		reportTitle = fmt.Sprintf("Multi-Strategy Benchmark for %s", symUpper)
		fmt.Printf("🎯 Running multi-strategy benchmark for single symbol: %s\n", symUpper)
	} else {
		fmt.Printf("🚀 Running multi-strategy benchmark across %d symbols over %d trading dates (Capital: $%.2f)...\n",
			len(barsBySymbol), len(sortedDates), *capital)
	}

	allStrategies := strategy.List()
	var results []CompareResult

	var startDate, endDate string
	totalDays := len(sortedDates)
	if totalDays > 0 {
		startDate = sortedDates[0]
		endDate = sortedDates[totalDays-1]
	}

	equityCurvesMap := make(map[string][]float64)
	drawdownCurvesMap := make(map[string][]float64)
	var strategyDataList []analytics.StrategyReportData

	for _, strat := range allStrategies {
		config := strat.DefaultConfig()
		signals := strat.GenerateSignals(barsBySymbol)

		if symUpper != "" {
			var filtered []models.Signal
			for _, s := range signals {
				if strings.ToUpper(s.Symbol) == symUpper {
					filtered = append(filtered, s)
				}
			}
			signals = filtered
		}

		sim := simulator.NewPortfolioSimulator(config, *capital)
		report, trades, curve := sim.Run(signals, barsBySymbol, sortedDates)

		sType := "Go"
		if strings.HasSuffix(strat.ID(), "-sql") {
			sType = "SQL"
		}

		results = append(results, CompareResult{
			ID:     strat.ID(),
			Name:   strat.Name(),
			Type:   sType,
			Report: report,
			Trades: trades,
			Curve:  curve,
		})

		strategyDataList = append(strategyDataList, analytics.StrategyReportData{
			ID:     strat.ID(),
			Name:   strat.Name(),
			Type:   sType,
			Report: report,
			Trades: trades,
		})

		var eqSeries []float64
		var ddSeries []float64
		for _, pt := range curve {
			eqSeries = append(eqSeries, pt.TotalEquity)
			ddSeries = append(ddSeries, pt.DrawdownPct)
		}
		equityCurvesMap[strat.ID()] = eqSeries
		drawdownCurvesMap[strat.ID()] = ddSeries
	}

	// Render CLI table
	fmt.Printf("\n==================================================================================================================================================================\n")
	fmt.Printf("🏆 MULTI-STRATEGY COMPARATIVE TEAR SHEET (PORTFOLIO SIMULATION: GO & SQL STRATEGIES)\n")
	fmt.Printf("📅 BACKTEST TIME WINDOW: %s ➔ %s (%.1f Years | %d Trading Days | Starting Capital: $%.2f)\n",
		startDate, endDate, float64(totalDays)/252.0, totalDays, *capital)
	fmt.Printf("==================================================================================================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{
		"ID", "Type", "Strategy Name", "Trades", "Win Rate", "Net Profit", "Total Return", "Profit Factor", "🔴 MAX DRAWDOWN %", "🔴 MAX DRAWDOWN ($)", "DD Span", "Sharpe", "Sortino", "Avg Hold",
	})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, res := range results {
		rep := res.Report
		table.Append([]string{
			res.ID,
			res.Type,
			res.Name,
			fmt.Sprintf("%d", rep.TotalTrades),
			fmt.Sprintf("%.2f%%", rep.WinRate*100),
			fmt.Sprintf("$%.2f", rep.NetProfit),
			fmt.Sprintf("%.2f%%", rep.TotalReturnPct*100),
			fmt.Sprintf("%.2f", rep.ProfitFactor),
			fmt.Sprintf("%.2f%%", rep.MaxDrawdownPct*100),
			fmt.Sprintf("-$%.2f", rep.MaxDrawdownDollars),
			fmt.Sprintf("%dd", rep.MaxDrawdownDuration),
			fmt.Sprintf("%.2f", rep.SharpeRatio),
			fmt.Sprintf("%.2f", rep.SortinoRatio),
			fmt.Sprintf("%.1f days", rep.AvgHoldingDays),
		})
	}
	table.Render()

	// Export HTML Report
	if *htmlOutput != "" {
		htmlData := analytics.MultiStrategyHTMLData{
			Title:          reportTitle,
			Symbol:         symUpper,
			StartDate:      startDate,
			EndDate:        endDate,
			TotalDays:      totalDays,
			TotalYears:     float64(totalDays) / 252.0,
			InitialCap:     *capital,
			Strategies:     strategyDataList,
			AllDates:       sortedDates,
			EquityCurves:   equityCurvesMap,
			DrawdownCurves: drawdownCurvesMap,
		}

		err := analytics.GenerateComparisonHTML(*htmlOutput, htmlData)
		if err != nil {
			log.Printf("Warning: Failed to generate HTML report %s: %v", *htmlOutput, err)
		} else {
			fmt.Printf("\n✨ Interactive HTML Report generated: %s\n\n", *htmlOutput)
		}
	}
}
