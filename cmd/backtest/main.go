package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
	"github.com/darianmavgo/backtestgosqlite/pkg/analytics"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

type runResult struct {
	strat       strategy.Strategy
	report      models.PerformanceReport
	trades      []models.Trade
	equityCurve []models.DailyEquityPoint
	dbPath      string
	signalCount int
	err         error
}

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
	table.Append([]string{"Calmar Ratio", fmt.Sprintf("%.2f", report.CalmarRatio), "CAGR / Max Drawdown"})
	table.Append([]string{"Omega Ratio", fmt.Sprintf("%.2f", report.OmegaRatio), "Gain-to-loss probability ratio"})
	table.Append([]string{"Ulcer Index", fmt.Sprintf("%.2f", report.UlcerIndex), "Depth & duration of drawdowns"})

	if report.Beta != 0 || report.Alpha != 0 {
		table.Append([]string{"Alpha (vs. Benchmark)", fmt.Sprintf("%.2f%%", report.Alpha*100), "Excess return over benchmark"})
		table.Append([]string{"Beta (vs. Benchmark)", fmt.Sprintf("%.2f", report.Beta), "Systematic market volatility"})
	}

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
	table.Append([]string{"Average MAE (Drawdown)", fmt.Sprintf("%.2f%%", report.AvgMAE*100), "Max Adverse Excursion during trade"})
	table.Append([]string{"Average MFE (Runup)", fmt.Sprintf("%.2f%%", report.AvgMFE*100), "Max Favorable Excursion during trade"})
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

func printTradesTable(trades []models.Trade, symbolFilter string) {
	if len(trades) == 0 {
		return
	}
	fmt.Printf("\nCompleted Trades for Simulation (Total: %d):\n", len(trades))
	tTable := tablewriter.NewWriter(os.Stdout)
	tTable.SetHeader([]string{"ID", "Symbol", "Entry Date", "Entry $", "Exit Date", "Exit $", "Hold Days", "Exit Reason", "Net PnL", "Return %"})
	tTable.SetBorder(true)

	startIdx := 0
	if symbolFilter == "" && len(trades) > 20 {
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

func printComparisonTable(results []runResult) {
	fmt.Printf("\n========================================================================================================================\n")
	fmt.Printf("📊 MULTI-STRATEGY CONCURRENT BENCHMARK COMPARISON\n")
	fmt.Printf("========================================================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Strategy ID", "Name", "Total Return", "CAGR", "Sharpe", "Max Drawdown", "Win Rate", "Trades", "SQLite Results File"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, r := range results {
		if r.err != nil {
			table.Append([]string{r.strat.ID(), r.strat.Name(), "ERROR", "ERROR", "ERROR", "ERROR", "ERROR", "0", "N/A"})
			continue
		}
		table.Append([]string{
			r.strat.ID(),
			r.strat.Name(),
			fmt.Sprintf("%.2f%%", r.report.TotalReturnPct*100),
			fmt.Sprintf("%.2f%%", r.report.CAGR*100),
			fmt.Sprintf("%.2f", r.report.SharpeRatio),
			fmt.Sprintf("%.2f%%", r.report.MaxDrawdownPct*100),
			fmt.Sprintf("%.2f%%", r.report.WinRate*100),
			fmt.Sprintf("%d", r.report.TotalTrades),
			r.dbPath,
		})
	}
	table.Render()
}

func buildConfig(s strategy.Strategy, stopLoss, profitTarget float64, holdWindow, maxPositions int) strategy.StrategyConfig {
	cfg := s.DefaultConfig()
	if stopLoss > 0 {
		cfg.StopLossPct = stopLoss
	}
	if profitTarget > 0 {
		cfg.TargetPct = profitTarget
	}
	if holdWindow > 0 {
		cfg.HoldingWindow = holdWindow
	}
	if maxPositions > 0 {
		cfg.PositionCap = maxPositions
	}
	return cfg
}

func executeStrategy(
	strat strategy.Strategy,
	cfg strategy.StrategyConfig,
	barsBySymbol map[string][]models.Bar,
	sortedDates []string,
	capital float64,
	symbolFilter string,
	outDir string,
	marketDBPath string,
) runResult {
	// 1. Create unique SQLite database matching strategy name FIRST
	outDBPath, outDB, err := storage.CreateUniqueDB(outDir, strat.ID())
	if err != nil {
		return runResult{strat: strat, err: fmt.Errorf("failed to create unique SQLite DB for %s: %w", strat.ID(), err)}
	}
	outDB.Close() // Close it so SQLPipelineStrategy can natively execute against it if needed

	// 2. Set DB routing for ALL strategies
	strat.SetDatabases(marketDBPath, outDBPath)

	// 3. Generate signals (calculations will now write natively into outDBPath if applicable)
	signals := strat.GenerateSignals(barsBySymbol)
	symUpper := strings.ToUpper(symbolFilter)
	if symUpper != "" {
		var filtered []models.Signal
		for _, s := range signals {
			if strings.ToUpper(s.Symbol) == symUpper {
				filtered = append(filtered, s)
			}
		}
		signals = filtered
	}

	// 2. Simulate portfolio
	sim := simulator.NewPortfolioSimulator(cfg, capital)
	report, trades, equityCurve := sim.Run(signals, barsBySymbol, sortedDates)

	// 5. Persist signals, trades, equity curve, and performance summary
	// Re-open outDB to write simulator output
	outDB, err = storage.OpenSQLite(outDBPath)
	if err != nil {
		log.Printf("Warning: Failed to re-open %s for simulator results: %v", outDBPath, err)
	} else {
		defer outDB.Close()

	// 4. Persist signals, trades, equity curve, and performance summary
	if err := storage.SaveSignals(outDB, strat.ID(), signals); err != nil {
		log.Printf("Warning: Failed to save signals to %s: %v", outDBPath, err)
	}
	if err := storage.SaveTrades(outDB, strat.ID(), trades); err != nil {
		log.Printf("Warning: Failed to save trades to %s: %v", outDBPath, err)
	}
	if err := storage.SaveEquityCurve(outDB, strat.ID(), equityCurve); err != nil {
		log.Printf("Warning: Failed to save equity curve to %s: %v", outDBPath, err)
	}
	if err := storage.SavePerformanceReport(outDB, strat.ID(), report); err != nil {
			log.Printf("Warning: Failed to save performance summary to %s: %v", outDBPath, err)
		}
	}

	return runResult{
		strat:       strat,
		report:      report,
		trades:      trades,
		equityCurve: equityCurve,
		dbPath:      outDBPath,
		signalCount: len(signals),
	}
}

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
		listStrategies()
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
	if err := detectAndDownloadMissingData(*targetDb, *tableName, selectedStrategies, *symbolFilter, *autoDownload, *downloadYears); err != nil {
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

	var results []runResult

	if len(selectedStrategies) == 1 {
		strat := selectedStrategies[0]
		cfg := buildConfig(strat, *stopLoss, *profitTarget, *holdWindow, *maxPositions)

		fmt.Printf("\n========================================================================================\n")
		fmt.Printf("🎯 STRATEGY SELECTED: %s (ID: %s)\n", strat.Name(), strat.ID())
		fmt.Printf("   Description:  %s\n", strat.Description())
		fmt.Printf("   Target:       +%.1f%% | Stop-Loss: -%.1f%% | Max Hold: %d days | Max Positions: %d\n",
			(cfg.TargetPct-1)*100, (1-cfg.StopLossPct)*100, cfg.HoldingWindow, cfg.PositionCap)
		fmt.Printf("========================================================================================\n")

		res := executeStrategy(strat, cfg, barsBySymbol, sortedDates, *capital, *symbolFilter, *outDir, *targetDb)
		if res.err != nil {
			log.Fatalf("Backtest failed: %v", res.err)
		}
		results = []runResult{res}

		fmt.Printf("Generated %d entry signals.\n", res.signalCount)
		fmt.Printf("💾 Persisted %d generated entry signals to SQLite 'signals' table.\n", res.signalCount)
		fmt.Printf("💾 Persisted %d completed trades to SQLite 'trades' table.\n", len(res.trades))
		fmt.Printf("💾 Persisted %d daily equity points to SQLite 'equity_curve' table.\n", len(res.equityCurve))
		fmt.Printf("💾 Persisted quantitative performance summary to SQLite 'performance_summary' table.\n")

		printPerformanceTearSheet(strat.Name(), res.report)
		printTradesTable(res.trades, *symbolFilter)

		fmt.Printf("\n💾 Dedicated Strategy SQLite Database: %s\n", res.dbPath)
	} else {
		fmt.Printf("\n========================================================================================\n")
		fmt.Printf("🚀 CONCURRENT STRATEGY BACKTESTING (%d STRATEGIES)\n", len(selectedStrategies))
		fmt.Printf("   Market Data:  %d symbols across %d dates loaded from %s\n", len(barsBySymbol), len(sortedDates), *targetDb)
		fmt.Printf("   Output Dir:   %s/ (each strategy writes to an isolated, uniquely suffixed SQLite DB)\n", *outDir)
		fmt.Printf("========================================================================================\n\n")

		results = make([]runResult, len(selectedStrategies))
		var wg sync.WaitGroup

		for i, strat := range selectedStrategies {
			wg.Add(1)
			go func(idx int, s strategy.Strategy) {
				defer wg.Done()
				cfg := buildConfig(s, *stopLoss, *profitTarget, *holdWindow, *maxPositions)
				res := executeStrategy(s, cfg, barsBySymbol, sortedDates, *capital, *symbolFilter, *outDir, *targetDb)
				results[idx] = res
				if res.err != nil {
					log.Printf("❌ [%s] Error: %v\n", s.ID(), res.err)
				} else {
					fmt.Printf("✅ [%s] Completed: %d signals, %d trades, Return: %+.2f%%, Sharpe: %.2f ➔ %s\n",
						s.ID(), res.signalCount, len(res.trades), res.report.TotalReturnPct*100, res.report.SharpeRatio, res.dbPath)
				}
			}(i, strat)
		}

		wg.Wait()

		printComparisonTable(results)
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
			if r.err != nil {
				continue
			}
			sType := "Go"
			if strings.HasSuffix(r.strat.ID(), "-sql") {
				sType = "SQL"
			}
			stratReports = append(stratReports, analytics.StrategyReportData{
				ID:     r.strat.ID(),
				Name:   r.strat.Name(),
				Type:   sType,
				Report: r.report,
				Trades: r.trades,
			})
			var eqSeries, ddSeries []float64
			for _, pt := range r.equityCurve {
				eqSeries = append(eqSeries, pt.TotalEquity)
				ddSeries = append(ddSeries, pt.DrawdownPct)
			}
			eqCurves[r.strat.ID()] = eqSeries
			ddCurves[r.strat.ID()] = ddSeries

			if startDate == "" {
				startDate = r.report.StartDate
				endDate = r.report.EndDate
				totalDays = r.report.TotalTradingDays
				totalYears = r.report.TotalCalendarYears
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

func detectAndDownloadMissingData(
	targetDb string,
	tableName string,
	strategies []strategy.Strategy,
	symbolFilter string,
	autoDownload bool,
	downloadYears int,
) error {
	requiredSet := make(map[string]struct{})

	// 1. If explicit symbol filter requested
	if sym := strings.ToUpper(strings.TrimSpace(symbolFilter)); sym != "" {
		requiredSet[sym] = struct{}{}
	}

	// 2. Symbols required by chosen strategies
	for _, s := range strategies {
		if reqProvider, ok := s.(strategy.RequiredSymbolsProvider); ok {
			for _, sym := range reqProvider.RequiredSymbols() {
				if sym = strings.ToUpper(strings.TrimSpace(sym)); sym != "" {
					requiredSet[sym] = struct{}{}
				}
			}
		}
		cfg := s.DefaultConfig()
		if bm := strings.ToUpper(strings.TrimSpace(cfg.Benchmark)); bm != "" {
			requiredSet[bm] = struct{}{}
		}
	}

	// 3. Open DB to check table and coverage
	db, err := storage.OpenSQLite(targetDb)
	if err != nil {
		return fmt.Errorf("failed to open database %s: %w", targetDb, err)
	}

	if err := storage.EnsureBarTable(db, tableName); err != nil {
		db.Close()
		return fmt.Errorf("failed to ensure bar table %s: %w", tableName, err)
	}

	var totalBars int
	_ = db.Get(&totalBars, fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName))

	// If no specific symbols required and table is completely empty, populate baseline universe
	if len(requiredSet) == 0 && totalBars == 0 {
		for _, sym := range []string{"SPY", "QQQ", "VOO", "TECL", "SOXL", "TQQQ"} {
			requiredSet[sym] = struct{}{}
		}
	}

	var missingSymbols []string
	for sym := range requiredSet {
		cov, err := storage.GetSymbolDateCoverage(db, tableName, sym)
		if err != nil || cov.BarCount == 0 {
			missingSymbols = append(missingSymbols, sym)
		}
	}
	sort.Strings(missingSymbols)

	db.Close() // Close before spawning download process

	if len(missingSymbols) == 0 {
		return nil
	}

	if !autoDownload {
		return fmt.Errorf("missing market data for symbol(s) %v in %s (%s). Run with -auto-download or run: ./bin/download -symbols %s",
			missingSymbols, targetDb, tableName, strings.Join(missingSymbols, ","))
	}

	fmt.Printf("\n🔍 Detected missing market data for %d symbol(s) in %s (%s): %v\n",
		len(missingSymbols), targetDb, tableName, missingSymbols)
	fmt.Printf("📥 Running download to fetch missing historical bars (%d years)...\n\n", downloadYears)

	if err := runDownload(targetDb, tableName, missingSymbols, downloadYears); err != nil {
		return fmt.Errorf("download command failed for symbols %v: %w", missingSymbols, err)
	}

	fmt.Printf("\n✅ Successfully updated market data for %v in %s (%s).\n", missingSymbols, targetDb, tableName)
	return nil
}

func runDownload(targetDb, targetTable string, symbols []string, years int) error {
	if len(symbols) == 0 {
		return nil
	}

	symArg := strings.Join(symbols, ",")
	yearsArg := strconv.Itoa(years)

	// Look for download binary
	candidates := []string{
		filepath.Join("bin", "download"),
		"download",
	}

	if execPath, err := os.Executable(); err == nil {
		candidates = append([]string{filepath.Join(filepath.Dir(execPath), "download")}, candidates...)
	}

	var downloadBin string
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			downloadBin = c
			break
		}
		if lp, err := exec.LookPath(c); err == nil {
			downloadBin = lp
			break
		}
	}

	var cmd *exec.Cmd
	if downloadBin != "" {
		cmd = exec.Command(downloadBin, "-db", targetDb, "-target-table", targetTable, "-symbols", symArg, "-years", yearsArg)
	} else {
		cmd = exec.Command("go", "run", "./cmd/download", "-db", targetDb, "-target-table", targetTable, "-symbols", symArg, "-years", yearsArg)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}
