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
	strategy.AutoRegisterSQLStrategies(".", *targetDb)

	for _, s := range strategy.List() {
		if sqlStrat, ok := s.(*strategy.SQLPipelineStrategy); ok {
			sqlStrat.SetDBPath(*targetDb)
		}
	}

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

	// Run all strategies concurrently across goroutines
	simResults := simulator.RunConcurrent(allStrategies, barsBySymbol, sortedDates, *capital, nil)

	for _, simRes := range simResults {
		if symUpper != "" {
			// Single symbol filter if requested
			var filteredTrades []models.Trade
			for _, t := range simRes.Trades {
				if strings.ToUpper(t.Symbol) == symUpper {
					filteredTrades = append(filteredTrades, t)
				}
			}
			simRes.Trades = filteredTrades
		}

		sType := "Go"
		if strings.HasSuffix(simRes.StrategyID, "-sql") {
			sType = "SQL"
		}

		results = append(results, CompareResult{
			ID:     simRes.StrategyID,
			Name:   simRes.Name,
			Type:   sType,
			Report: simRes.Report,
			Trades: simRes.Trades,
			Curve:  simRes.EquityCurve,
		})

		strategyDataList = append(strategyDataList, analytics.StrategyReportData{
			ID:     simRes.StrategyID,
			Name:   simRes.Name,
			Type:   sType,
			Report: simRes.Report,
			Trades: simRes.Trades,
		})

		var eqSeries []float64
		var ddSeries []float64
		for _, pt := range simRes.EquityCurve {
			eqSeries = append(eqSeries, pt.TotalEquity)
			ddSeries = append(ddSeries, pt.DrawdownPct)
		}
		equityCurvesMap[simRes.StrategyID] = eqSeries
		drawdownCurvesMap[simRes.StrategyID] = ddSeries
	}

	// Render CLI table
	fmt.Printf("\n==================================================================================================================================================================\n")
	fmt.Printf("🏆 MULTI-STRATEGY COMPARATIVE TEAR SHEET (CONCURRENT PORTFOLIO SIMULATION: GO & SQL STRATEGIES)\n")
	fmt.Printf("📅 BACKTEST TIME WINDOW: %s ➔ %s (%.1f Years | %d Trading Days | Starting Capital: $%.2f)\n",
		startDate, endDate, float64(totalDays)/252.0, totalDays, *capital)
	fmt.Printf("==================================================================================================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{
		"ID", "Type", "Strategy Name", "Trades", "Win Rate", "Net Profit", "Total Return", "Profit Factor", "🔴 MAX DRAWDOWN %", "🔴 MAX DRAWDOWN ($)", "Calmar", "Sharpe", "Sortino", "Avg Hold",
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
			fmt.Sprintf("%.2f", rep.CalmarRatio),
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
