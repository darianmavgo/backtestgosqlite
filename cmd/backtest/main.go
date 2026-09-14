package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/analytics"
	"github.com/darianmavgo/backtestgosqlite/pkg/cliutils"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/runner"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	defaultMarketDb := cliutils.GetDefaultMarketDB()

	targetDb := flag.String("db", defaultMarketDb, "Path to source SQLite DB containing historical market bars")
	tableName := flag.String("table", "backtest_start", "Table name containing historical bars")
	strategyType := flag.String("strategy", "", "Strategy ID to run, comma-separated list, 'all', or 'strat1+strat2' for shared account")
	sharedAccountFlag := flag.Bool("shared-account", false, "Run strategies in a single shared cash account with priority preemption")
	primaryFlag := flag.String("primary", "", "Primary strategy ID for shared-account execution (has capital priority)")
	secondaryFlag := flag.String("secondary", "", "Secondary strategy ID(s) for shared-account execution (comma-separated)")
	outDir := flag.String("out-dir", "reports", "Directory to write strategy SQLite database results and reports")
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

	// Ensure HTML reports land in reports/ directory
	if *htmlOutput != "" && !filepath.IsAbs(*htmlOutput) && !strings.HasPrefix(*htmlOutput, "reports/") && !strings.HasPrefix(*htmlOutput, "reports"+string(filepath.Separator)) {
		*htmlOutput = filepath.Join("reports", *htmlOutput)
	}

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

	// Detect if user requested Shared Account Mode
	isSharedAccount := *sharedAccountFlag || *primaryFlag != "" || *secondaryFlag != "" || strings.Contains(stratArg, "+")

	if isSharedAccount {
		var primaryStrat strategy.Strategy
		var secondaryStrats []strategy.Strategy

		if *primaryFlag != "" {
			s, exists := strategy.Get(*primaryFlag)
			if !exists {
				log.Fatalf("Primary strategy '%s' not found in registry.", *primaryFlag)
			}
			primaryStrat = s

			if *secondaryFlag != "" {
				secTokens := strings.FieldsFunc(*secondaryFlag, func(r rune) bool { return r == ',' || r == ' ' })
				for _, tok := range secTokens {
					sec, exists := strategy.Get(strings.TrimSpace(tok))
					if !exists {
						log.Fatalf("Secondary strategy '%s' not found in registry.", tok)
					}
					secondaryStrats = append(secondaryStrats, sec)
				}
			}
		} else if strings.Contains(stratArg, "+") {
			parts := strings.Split(stratArg, "+")
			pID := strings.TrimSpace(parts[0])
			pStrat, exists := strategy.Get(pID)
			if !exists {
				log.Fatalf("Primary strategy '%s' not found in registry.", pID)
			}
			primaryStrat = pStrat

			for _, p := range parts[1:] {
				sID := strings.TrimSpace(p)
				sStrat, exists := strategy.Get(sID)
				if !exists {
					log.Fatalf("Secondary strategy '%s' not found in registry.", sID)
				}
				secondaryStrats = append(secondaryStrats, sStrat)
			}
		} else {
			// Spliced from -strategy comma-separated list
			tokens := strings.FieldsFunc(stratArg, func(r rune) bool { return r == ',' || r == ' ' })
			if len(tokens) < 2 {
				log.Fatalf("Shared account mode requires at least 2 strategies (primary + secondary).")
			}
			pStrat, exists := strategy.Get(strings.TrimSpace(tokens[0]))
			if !exists {
				log.Fatalf("Primary strategy '%s' not found in registry.", tokens[0])
			}
			primaryStrat = pStrat

			for _, tok := range tokens[1:] {
				sStrat, exists := strategy.Get(strings.TrimSpace(tok))
				if !exists {
					log.Fatalf("Secondary strategy '%s' not found in registry.", tok)
				}
				secondaryStrats = append(secondaryStrats, sStrat)
			}
		}

		allStrats := append([]strategy.Strategy{primaryStrat}, secondaryStrats...)

		// Detect missing market data and download
		if err := runner.DetectAndDownloadMissingData(*targetDb, *tableName, allStrats, *symbolFilter, *autoDownload, *downloadYears); err != nil {
			log.Fatalf("Market data resolution error: %v", err)
		}

		db, err := storage.OpenSQLite(*targetDb)
		if err != nil {
			log.Fatalf("Failed to open source DB %s: %v", *targetDb, err)
		}
		defer db.Close()

		fmt.Printf("\n⚙️ Loading chronological bars from table '%s' for Shared-Account Simulation (Starting Capital: $%.2f)...\n", *tableName, *capital)
		barsBySymbol, sortedDates, err := storage.FetchAllBarsChronological(db, *tableName)
		if err != nil {
			log.Fatalf("Error loading historical bars for simulation: %v", err)
		}

		sharedRes := runner.ExecuteSharedAccount(
			primaryStrat, secondaryStrats, barsBySymbol, sortedDates, *capital, *symbolFilter, *outDir, *targetDb,
		)
		if sharedRes.Err != nil {
			log.Fatalf("Shared account backtest failed: %v", sharedRes.Err)
		}

		runner.PrintSharedAccountTearSheet(sharedRes)

		// Export HTML Report for Shared Account
		if *htmlOutput != "" {
			symUpper := strings.ToUpper(*symbolFilter)
			reportTitle := fmt.Sprintf("Shared Account Multi-Strategy Report: %s + %s", primaryStrat.Name(), secondaryStrats[0].Name())

			var stratReports []analytics.StrategyReportData
			eqCurves := make(map[string][]float64)
			ddCurves := make(map[string][]float64)

			// 1. Shared Account Combined
			stratReports = append(stratReports, analytics.StrategyReportData{
				ID:     "SHARED_ACCOUNT",
				Name:   "Consolidated Shared Account",
				Type:   "Portfolio",
				Report: sharedRes.CombinedReport,
				Trades: sharedRes.Trades,
			})
			var eqSeries, ddSeries []float64
			for _, pt := range sharedRes.EquityCurve {
				eqSeries = append(eqSeries, pt.TotalEquity)
				ddSeries = append(ddSeries, pt.DrawdownPct)
			}
			eqCurves["SHARED_ACCOUNT"] = eqSeries
			ddCurves["SHARED_ACCOUNT"] = ddSeries

			// 2. Primary Strategy Attribution
			pRep := sharedRes.PerStrategyReports[primaryStrat.ID()]
			var pTrades []models.Trade
			for _, t := range sharedRes.Trades {
				if t.StrategyID == primaryStrat.ID() {
					pTrades = append(pTrades, t)
				}
			}
			stratReports = append(stratReports, analytics.StrategyReportData{
				ID:     primaryStrat.ID(),
				Name:   primaryStrat.Name() + " (Primary)",
				Type:   "Primary",
				Report: pRep,
				Trades: pTrades,
			})

			// 3. Secondary Strategies Attribution
			for _, sec := range secondaryStrats {
				sRep := sharedRes.PerStrategyReports[sec.ID()]
				var sTrades []models.Trade
				for _, t := range sharedRes.Trades {
					if t.StrategyID == sec.ID() {
						sTrades = append(sTrades, t)
					}
				}
				stratReports = append(stratReports, analytics.StrategyReportData{
					ID:     sec.ID(),
					Name:   sec.Name() + " (Secondary)",
					Type:   "Secondary",
					Report: sRep,
					Trades: sTrades,
				})
			}

			htmlData := analytics.MultiStrategyHTMLData{
				Title:          reportTitle,
				GeneratedAt:    time.Now().Format("2006-01-02 15:04:05 MST"),
				Symbol:         symUpper,
				StartDate:      sharedRes.CombinedReport.StartDate,
				EndDate:        sharedRes.CombinedReport.EndDate,
				TotalDays:      sharedRes.CombinedReport.TotalTradingDays,
				TotalYears:     sharedRes.CombinedReport.TotalCalendarYears,
				InitialCap:     *capital,
				Strategies:     stratReports,
				AllDates:       sortedDates,
				EquityCurves:   eqCurves,
				DrawdownCurves: ddCurves,
			}

			if err := analytics.GenerateComparisonHTML(*htmlOutput, htmlData); err != nil {
				log.Printf("Warning: Failed to generate HTML report %s: %v", *htmlOutput, err)
			} else {
				fmt.Printf("\n✨ Interactive HTML Report generated: %s\n\n", *htmlOutput)
			}
		}
		return
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
		tpPct := cfg.TakeProfitPct * 100
		if tpPct == 0 && cfg.TargetPct > 1.0 {
			tpPct = (cfg.TargetPct - 1.0) * 100
		}
		slPct := cfg.StopLossPct * 100
		if slPct > 50 {
			slPct = (1.0 - cfg.StopLossPct) * 100
		}
		fmt.Printf("   Target:       +%.1f%% | Stop-Loss: -%.1f%% | Max Hold: %d days | Max Positions: %d\n",
			tpPct, slPct, cfg.HoldingWindow, cfg.PositionCap)
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

	// Export HTML Report for standalone/concurrent mode
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
			GeneratedAt:    time.Now().Format("2006-01-02 15:04:05 MST"),
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
