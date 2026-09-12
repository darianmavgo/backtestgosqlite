package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/darianmavgo/backtestgosqlite/pkg/analytics"
	"github.com/darianmavgo/backtestgosqlite/pkg/runner"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	defaultMarketDb := "data/market_history.db"
	if _, err := os.Stat(defaultMarketDb); os.IsNotExist(err) {
		defaultMarketDb = "data/leveraged_backtest.db"
	}

	targetDb := flag.String("db", defaultMarketDb, "Path to source SQLite DB containing historical market bars")
	tableName := flag.String("table", "backtest_start", "Table name containing historical bars")
	strategyType := flag.String("strategy", "", "Strategy ID to run, comma-separated list, or 'all' (e.g. bb-capitulation,trend-bb,rsi2)")
	outDir := flag.String("out-dir", "reports", "Directory to write strategy-isolated SQLite database results and reports")
	listFlag := flag.Bool("list", false, "List all registered Go and SQL strategies")
	symbolFilter := flag.String("symbol", "", "Optional: Filter backtest to a specific symbol (e.g. DFEN, SOXL)")
	capital := flag.Float64("capital", 100000.0, "Starting portfolio capital for simulation")
	maxPositions := flag.Int("max-positions", 0, "Optional override: Maximum concurrent open positions allowed")
	stopLoss := flag.Float64("stoploss", 0.0, "Optional override: Stop-loss floor multiplier (e.g. 0.93 for -7%)")
	profitTarget := flag.Float64("target", 0.0, "Optional override: Take-profit multiplier (e.g. 1.18 for +18%)")
	holdWindow := flag.Int("hold", 0, "Optional override: Max holding days window")
	htmlOutput := flag.String("html", "reports/backtest_report.html", "Path to export interactive HTML dashboard report")
	autoDownload := flag.Bool("auto-download", true, "Automatically detect missing market data and run download")
	downloadYears := flag.Int("download-years", 5, "Number of years of history to fetch when downloading missing data")
	flag.Parse()

	// Auto-discover any SQL pipeline strategies in sql/strategies/
	strategy.AutoRegisterSQLStrategies(".", *targetDb)

	if *listFlag {
		runner.PrintStrategyList()
		return
	}

	// Resolve strategies from flags or positional arguments
	stratArg := strings.TrimSpace(*strategyType)
	posArgs := flag.Args()
	if stratArg == "" && len(posArgs) > 0 {
		stratArg = strings.Join(posArgs, ",")
	}
	if stratArg == "" {
		stratArg = "bb-capitulation"
	}

	var selectedStrategies []strategy.Strategy
	if strings.ToLower(stratArg) == "all" {
		selectedStrategies = strategy.List()
	} else {
		tokens := strings.FieldsFunc(stratArg, func(r rune) bool {
			return r == ',' || r == ' '
		})
		for _, token := range tokens {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			s, exists := strategy.Get(token)
			if !exists {
				log.Fatalf("Strategy '%s' not found in registry. Run with -list to view available strategies.", token)
			}
			selectedStrategies = append(selectedStrategies, s)
		}
	}

	if len(selectedStrategies) == 0 {
		log.Fatalf("No valid strategies selected. Run with -list to view available strategies.")
	}

	// Detect missing market data and download before running backtest
	if err := runner.DetectAndDownloadMissingData(*targetDb, *tableName, selectedStrategies, *symbolFilter, *autoDownload, *downloadYears); err != nil {
		log.Fatalf("Market data resolution error: %v", err)
	}

	// Open read-only historical bars from source DB
	db, err := storage.OpenSQLite(*targetDb)
	if err != nil {
		log.Fatalf("Failed to open source DB %s: %v", *targetDb, err)
	}
	defer db.Close()

	fmt.Printf("\n⚙️ Loading chronological bars from table '%s' for Portfolio Simulation (Starting Capital: $%.2f)...\n", *tableName, *capital)
	barsBySymbol, sortedDates, err := storage.FetchAllBarsChronological(db, *tableName)
	if err != nil {
		log.Fatalf("Error loading historical bars for simulation: %v", err)
	}

	var results []runner.RunResult

	if len(selectedStrategies) == 1 {
		strat := selectedStrategies[0]
		cfg := runner.BuildConfig(strat, *stopLoss, *profitTarget, *holdWindow, *maxPositions)

		fmt.Printf("\n========================================================================================\n")
		fmt.Printf("🎯 STRATEGY SELECTED: %s (ID: %s)\n", strat.Name(), strat.ID())
		fmt.Printf("   Description:  %s\n", strat.Description())
		fmt.Printf("   Target:       +%.1f%% | Stop-Loss: -%.1f%% | Max Hold: %d days | Max Positions: %d\n",
			(cfg.TargetPct-1)*100, (1-cfg.StopLossPct)*100, cfg.HoldingWindow, cfg.PositionCap)
		fmt.Printf("========================================================================================\n")

		res := runner.ExecuteStrategy(strat, cfg, barsBySymbol, sortedDates, *capital, *symbolFilter, *outDir, *targetDb)
		if res.Err != nil {
			log.Fatalf("Backtest failed: %v", res.Err)
		}
		results = []runner.RunResult{res}

		fmt.Printf("Generated %d entry signals.\n", res.SignalCount)
		fmt.Printf("💾 Persisted %d generated entry signals to SQLite 'signals' table.\n", res.SignalCount)
		fmt.Printf("💾 Persisted %d completed trades to SQLite 'trades' table.\n", len(res.Trades))
		fmt.Printf("💾 Persisted %d daily equity points to SQLite 'equity_curve' table.\n", len(res.EquityCurve))
		fmt.Printf("💾 Persisted quantitative performance summary to SQLite 'performance_summary' table.\n")

		runner.PrintPerformanceTearSheet(strat.Name(), res.Report)
		runner.PrintTradesTable(res.Trades, *symbolFilter)

		fmt.Printf("\n💾 Dedicated Strategy SQLite Database: %s\n", res.DbPath)
	} else {
		fmt.Printf("\n========================================================================================\n")
		fmt.Printf("🚀 CONCURRENT STRATEGY BACKTESTING (%d STRATEGIES)\n", len(selectedStrategies))
		fmt.Printf("   Market Data:  %d symbols across %d dates loaded from %s\n", len(barsBySymbol), len(sortedDates), *targetDb)
		fmt.Printf("   Output Dir:   %s/ (each strategy writes to an isolated, uniquely suffixed SQLite DB)\n", *outDir)
		fmt.Printf("========================================================================================\n\n")

		results = make([]runner.RunResult, len(selectedStrategies))
		var wg sync.WaitGroup

		for i, strat := range selectedStrategies {
			wg.Add(1)
			go func(idx int, s strategy.Strategy) {
				defer wg.Done()
				cfg := runner.BuildConfig(s, *stopLoss, *profitTarget, *holdWindow, *maxPositions)
				res := runner.ExecuteStrategy(s, cfg, barsBySymbol, sortedDates, *capital, *symbolFilter, *outDir, *targetDb)
				results[idx] = res
				if res.Err != nil {
					log.Printf("❌ [%s] Error: %v\n", s.ID(), res.Err)
				} else {
					fmt.Printf("✅ [%s] Completed: %d signals, %d trades, Return: %+.2f%%, Sharpe: %.2f ➔ %s\n",
						s.ID(), res.SignalCount, len(res.Trades), res.Report.TotalReturnPct*100, res.Report.SharpeRatio, res.DbPath)
				}
			}(i, strat)
		}

		wg.Wait()

		runner.PrintComparisonTable(results)
	}

	// Export HTML Report
	if *htmlOutput != "" {
		symUpper := strings.ToUpper(*symbolFilter)
		reportTitle := "Multi-Strategy Quantitative Comparison Report"
		if len(selectedStrategies) == 1 {
			reportTitle = fmt.Sprintf("%s Performance Report", selectedStrategies[0].Name())
			if symUpper != "" {
				reportTitle = fmt.Sprintf("%s Performance Report for %s", selectedStrategies[0].Name(), symUpper)
			}
		}

		var stratReports []analytics.StrategyReportData
		eqCurves := make(map[string][]float64)
		ddCurves := make(map[string][]float64)

		var startDate, endDate string
		var totalDays int
		var totalYears float64

		for _, r := range results {
			if r.Err != nil {
				continue
			}
			sType := "Go"
			if strings.HasSuffix(r.Strat.ID(), "-sql") {
				sType = "SQL"
			}
			stratReports = append(stratReports, analytics.StrategyReportData{
				ID:     r.Strat.ID(),
				Name:   r.Strat.Name(),
				Type:   sType,
				Report: r.Report,
				Trades: r.Trades,
			})
			var eqSeries, ddSeries []float64
			for _, pt := range r.EquityCurve {
				eqSeries = append(eqSeries, pt.TotalEquity)
				ddSeries = append(ddSeries, pt.DrawdownPct)
			}
			eqCurves[r.Strat.ID()] = eqSeries
			ddCurves[r.Strat.ID()] = ddSeries

			if startDate == "" {
				startDate = r.Report.StartDate
				endDate = r.Report.EndDate
				totalDays = r.Report.TotalTradingDays
				totalYears = r.Report.TotalCalendarYears
			}
		}

		htmlData := analytics.MultiStrategyHTMLData{
			Title:          reportTitle,
			Symbol:         symUpper,
			StartDate:      startDate,
			EndDate:        endDate,
			TotalDays:      totalDays,
			TotalYears:     totalYears,
			InitialCap:     *capital,
			Strategies:     stratReports,
			AllDates:       sortedDates,
			EquityCurves:   eqCurves,
			DrawdownCurves: ddCurves,
		}

		err := analytics.GenerateComparisonHTML(*htmlOutput, htmlData)
		if err != nil {
			log.Printf("Warning: Failed to generate HTML report %s: %v", *htmlOutput, err)
		} else {
			fmt.Printf("\n✨ Interactive HTML Report generated: %s\n\n", *htmlOutput)
		}
	}
}
