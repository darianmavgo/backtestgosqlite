package runner

import (
	"bytes"
	"fmt"
	"github.com/darianmavgo/backtestgosqlite/pkg/options"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/olekukonko/tablewriter"
	_ "modernc.org/sqlite"
)

// ReinvestDividends controls total-return strategies (see strategy.TotalReturnProvider).
// true (default): simulate on dividend-adjusted prices, i.e. dividends reinvested.
// false: simulate on raw prices and pay dividends into cash, uninvested.
// Set once from the backtest -no-reinvest-dividends flag before running.
var ReinvestDividends = true

type RunResult struct {
	Strat       strategy.Strategy
	Report      models.PerformanceReport
	Trades      []models.Trade
	EquityCurve []models.DailyEquityPoint
	DbPath      string
	SignalCount int
	Err         error

	// Set for strategies simulated on total-return (dividend-adjusted) prices.
	// Buy-and-hold decomposition, before fees/slippage, entry signal → last bar.
	TotalReturn         bool
	Notes               []string // extra strategy-specific result lines (printed by PrintNotes)
	DividendsReinvested bool
	DividendCash        float64 // total cash dividends received (only when not reinvested)
	PriceReturnPct      float64 // capital gains only (raw closes)
	DividendReturnPct   float64 // reinvested dividends (total − price)
	TotalReturnPct      float64
	BreakdownSymbol     string
	BreakdownStart      string
	BreakdownEnd        string
}

func PrintPerformanceTearSheet(strategyName string, report models.PerformanceReport) {
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
		fmt.Sprintf("Peak equity to trough equity dates"),
	})
	table.Append([]string{
		"🔴 MAX DRAWDOWN DURATION",
		fmt.Sprintf("%d days", report.MaxDrawdownDuration),
		fmt.Sprintf("Longest underwater period without new ATH"),
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

func PrintStrategyList() {
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
			FormatTargetDisplay(cfg),
			FormatStopDisplay(cfg),
			fmt.Sprintf("%dd", cfg.HoldingWindow),
			s.Description(),
		})
	}
	table.Render()
	fmt.Printf("\nRun any strategy with: ./bin/backtest -strategy <ID>\n\n")
}

// FormatTargetDisplay and FormatStopDisplay render a strategy's baseline
// target/stop for a -list table, honoring the same TargetPct (multiplier,
// legacy) / TakeProfitPct (offset, preferred) and StopLossPct (multiplier)
// conventions as pkg/simulator/portfolio.go. A plain (cfg.TargetPct-1)*100 or
// (1-cfg.StopLossPct)*100 renders garbage ("+99800.0%", "-100.0%") for any
// strategy that sets only TakeProfitPct or has StopLossPct == 0
// (intentionally "no stop") — which covers most SQL-pipeline strategies.
func FormatTargetDisplay(cfg strategy.StrategyConfig) string {
	if cfg.TargetPct > 1.0 {
		return fmt.Sprintf("+%.1f%%", (cfg.TargetPct-1)*100)
	}
	if cfg.TakeProfitPct > 0 {
		return fmt.Sprintf("+%.1f%%", cfg.TakeProfitPct*100)
	}
	return "n/a (per-signal)"
}

func FormatStopDisplay(cfg strategy.StrategyConfig) string {
	if cfg.StopLossPct <= 0 {
		return "none"
	}
	if cfg.StopLossPct < 1.0 {
		return fmt.Sprintf("-%.1f%%", (1-cfg.StopLossPct)*100)
	}
	return fmt.Sprintf("-%.1f%%", cfg.StopLossPct*100)
}

func PrintTradesTable(trades []models.Trade, symbolFilter string) {
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

func PrintComparisonTable(results []RunResult) {
	// Sort by CAGR descending
	sort.Slice(results, func(i, j int) bool {
		if results[i].Err != nil {
			return false
		}
		if results[j].Err != nil {
			return true
		}
		return results[i].Report.CAGR > results[j].Report.CAGR
	})
	fmt.Printf("\n========================================================================================================================\n")
	fmt.Printf("📊 MULTI-STRATEGY CONCURRENT BENCHMARK COMPARISON\n")
	fmt.Printf("========================================================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Strategy ID", "Name", "Total Return", "CAGR", "Sharpe", "Max DD %", "🔴 DD Duration", "Win Rate", "Trades", "SQLite Results File"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, r := range results {
		if r.Err != nil {
			table.Append([]string{r.Strat.ID(), r.Strat.Name(), "ERROR", "ERROR", "ERROR", "ERROR", "ERROR", "ERROR", "0", "N/A"})
			continue
		}
		table.Append([]string{
			r.Strat.ID(),
			r.Strat.Name(),
			fmt.Sprintf("%.2f%%", r.Report.TotalReturnPct*100),
			fmt.Sprintf("%.2f%%", r.Report.CAGR*100),
			fmt.Sprintf("%.2f", r.Report.SharpeRatio),
			fmt.Sprintf("%.2f%%", r.Report.MaxDrawdownPct*100),
			fmt.Sprintf("%d days", r.Report.MaxDrawdownDuration),
			fmt.Sprintf("%.2f%%", r.Report.WinRate*100),
			fmt.Sprintf("%d", r.Report.TotalTrades),
			r.DbPath,
		})
	}
	table.Render()
}

func BuildConfig(s strategy.Strategy, stopLoss, profitTarget float64, holdWindow, maxPositions int) strategy.StrategyConfig {
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

// scopeDatesToStrategy restricts a global, DB-wide sorted date list down to the
// dates covered by a strategy's own required symbols (+ benchmark), so CAGR and
// drawdown-duration metrics reflect that strategy's actual trading window instead
// of being diluted by unrelated long-history symbols that happen to share the DB.
func scopeDatesToStrategy(
	strat strategy.Strategy,
	cfg strategy.StrategyConfig,
	barsBySymbol map[string][]models.Bar,
	sortedDates []string,
) []string {
	relevantSymbols := make(map[string]struct{})
	if reqProvider, ok := strat.(strategy.RequiredSymbolsProvider); ok {
		for _, sym := range reqProvider.RequiredSymbols() {
			if sym = strings.ToUpper(strings.TrimSpace(sym)); sym != "" {
				relevantSymbols[sym] = struct{}{}
			}
		}
	}
	if bm := strings.ToUpper(strings.TrimSpace(cfg.Benchmark)); bm != "" {
		relevantSymbols[bm] = struct{}{}
	}
	if len(relevantSymbols) == 0 {
		return sortedDates // no symbol info available; fall back to the full range
	}

	relevantDates := make(map[string]struct{})
	for sym, bars := range barsBySymbol {
		if _, ok := relevantSymbols[strings.ToUpper(sym)]; !ok {
			continue
		}
		for _, b := range bars {
			relevantDates[b.Date] = struct{}{}
		}
	}
	if len(relevantDates) == 0 {
		return sortedDates
	}

	scoped := make([]string, 0, len(relevantDates))
	for _, d := range sortedDates {
		if _, ok := relevantDates[d]; ok {
			scoped = append(scoped, d)
		}
	}
	return scoped
}

func ExecuteStrategy(
	strat strategy.Strategy,
	cfg strategy.StrategyConfig,
	barsBySymbol map[string][]models.Bar,
	sortedDates []string,
	capital float64,
	symbolFilter string,
	outDir string,
	marketDBPath string,
) RunResult {
	// 1. Create unique SQLite database matching strategy name FIRST
	outDBPath, outDB, err := storage.CreateUniqueDB(outDir, strat.ID())
	if err != nil {
		return RunResult{Strat: strat, Err: fmt.Errorf("failed to create unique SQLite DB for %s: %w", strat.ID(), err)}
	}
	outDB.Close() // Close it so SQLPipelineStrategy can natively execute against it if needed

	// 2. Set DB routing for ALL strategies
	strat.SetDatabases(marketDBPath, outDBPath)

	// 2a. Covered-call overlays have their own simulator (pkg/options).
	if ov, ok := strat.(strategy.OptionOverlayProvider); ok {
		return executeOverlay(strat, ov.OverlaySpec(), cfg, barsBySymbol, capital, outDBPath, marketDBPath)
	}

	// 2b. Dividend-inclusive strategies run on adjusted-price copies of their bars.
	var tr *totalReturnInfo
	if p, ok := strat.(strategy.TotalReturnProvider); ok && p.UsesTotalReturn() {
		barsBySymbol, tr = toTotalReturnBars(strat, cfg, barsBySymbol, ReinvestDividends)
	}

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

	// 2. Simulate portfolio. Scope the simulation clock to dates relevant to this
	// strategy's own symbols rather than the full DB-wide date list — otherwise a
	// strategy trading a young ticker (e.g. 2021+) gets its CAGR diluted by decades
	// of idle time from unrelated long-history symbols sharing the same database.
	scopedDates := scopeDatesToStrategy(strat, cfg, barsBySymbol, sortedDates)
	sim := simulator.NewPortfolioSimulator(cfg, capital)
	if tr != nil {
		sim.Dividends = tr.divs
	}
	report, trades, equityCurve := sim.Run(signals, barsBySymbol, scopedDates)

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

	rr := RunResult{
		Strat:       strat,
		Report:      report,
		Trades:      trades,
		EquityCurve: equityCurve,
		DbPath:      outDBPath,
		SignalCount: len(signals),
	}
	if tr != nil && len(signals) > 0 {
		if b, ok := tr.breakdown(signals[0].Symbol, signals[0].Date); ok {
			rr.DividendsReinvested, rr.DividendCash = tr.reinvest, sim.DividendCash
			rr.TotalReturn, rr.PriceReturnPct, rr.DividendReturnPct, rr.TotalReturnPct = true, b.price, b.total-b.price, b.total
			rr.BreakdownSymbol, rr.BreakdownStart, rr.BreakdownEnd = signals[0].Symbol, b.start, b.end
			if db, err := storage.OpenSQLite(outDBPath); err == nil {
				if err := storage.SaveReturnBreakdown(db, strat.ID(), rr.BreakdownSymbol, b.start, b.end, tr.reinvest,
					rr.PriceReturnPct, rr.DividendReturnPct, rr.TotalReturnPct); err != nil {
					log.Printf("Warning: Failed to save return breakdown to %s: %v", outDBPath, err)
				}
				db.Close()
			}
		}
	}
	return rr
}

// PrintNotes prints strategy-specific result lines (e.g. covered-call income).
func PrintNotes(res RunResult) {
	if len(res.Notes) == 0 {
		return
	}
	fmt.Println()
	for _, n := range res.Notes {
		fmt.Println(n)
	}
}

// PrintReturnBreakdown prints the capital-gain vs dividend split for total-return strategies.
func PrintReturnBreakdown(res RunResult) {
	if !res.TotalReturn {
		return
	}
	mode := "dividends reinvested"
	if !res.DividendsReinvested {
		mode = "dividends NOT reinvested"
	}
	fmt.Printf("\n💰 %s buy & hold return split (%s ➔ %s, before fees/slippage, %s):\n", res.BreakdownSymbol, res.BreakdownStart, res.BreakdownEnd, mode)
	fmt.Printf("   Capital gains: %+.2f%%   Dividends: %+.2f%%   Total: %+.2f%%\n", res.PriceReturnPct*100, res.DividendReturnPct*100, res.TotalReturnPct*100)
	if res.DividendsReinvested {
		fmt.Printf("   (Simulated on dividend-adjusted prices; tear-sheet returns already include dividends, net of costs.)\n")
	} else {
		fmt.Printf("   (Simulated on raw prices; $%.0f of dividends paid into idle cash, earning nothing; tear-sheet equity includes that cash.)\n", res.DividendCash)
	}
}

// totalReturnInfo keeps the raw (price-only) bars of a strategy whose bars were adjusted.
type totalReturnInfo struct {
	raw      map[string][]models.Bar
	divs     map[string]map[string]float64 // symbol → ex-date → $/share (non-reinvest mode)
	reinvest bool
}

type splitReturn struct {
	price, total float64
	start, end   string
}

// toTotalReturnBars returns a copy of barsBySymbol where the strategy's required
// symbols and benchmark have OHLC scaled by AdjClose/Close. Other symbols and the
// caller's slices are left untouched (strategies may run concurrently on one map).
func toTotalReturnBars(strat strategy.Strategy, cfg strategy.StrategyConfig, in map[string][]models.Bar, reinvest bool) (map[string][]models.Bar, *totalReturnInfo) {
	need := map[string]bool{strings.ToUpper(strings.TrimSpace(cfg.Benchmark)): true}
	if rp, ok := strat.(strategy.RequiredSymbolsProvider); ok {
		for _, s := range rp.RequiredSymbols() {
			need[strings.ToUpper(strings.TrimSpace(s))] = true
		}
	}
	out := make(map[string][]models.Bar, len(in))
	info := &totalReturnInfo{raw: map[string][]models.Bar{}, reinvest: reinvest}
	for sym, bars := range in {
		if !need[strings.ToUpper(sym)] {
			out[sym] = bars
			continue
		}
		info.raw[sym] = bars
		if !reinvest {
			// Keep raw prices; recover cash dividends from steps in AdjClose/Close.
			info.divs = ensureDivs(info.divs)
			m := options.DividendsFromAdjClose(bars)
			info.divs[sym] = m
			out[sym] = bars
			continue
		}
		adj := make([]models.Bar, len(bars))
		for i, b := range bars {
			if b.AdjClose > 0 && b.Close > 0 {
				f := b.AdjClose / b.Close
				b.Open, b.High, b.Low, b.Close = b.Open*f, b.High*f, b.Low*f, b.Close*f
			}
			adj[i] = b
		}
		out[sym] = adj
	}
	return out, info
}

func ensureDivs(m map[string]map[string]float64) map[string]map[string]float64 {
	if m == nil {
		return map[string]map[string]float64{}
	}
	return m
}

func (t *totalReturnInfo) breakdown(symbol, fromDate string) (splitReturn, bool) {
	var first, last *models.Bar
	bars := t.raw[symbol]
	for i := range bars {
		if bars[i].Date < fromDate {
			continue
		}
		if first == nil {
			first = &bars[i]
		}
		last = &bars[i]
	}
	if first == nil || first.Close <= 0 || last.Close <= 0 || first.AdjClose <= 0 || last.AdjClose <= 0 {
		return splitReturn{}, false
	}
	price := last.Close/first.Close - 1
	total := last.AdjClose/first.AdjClose - 1
	if !t.reinvest { // simple (uncompounded) dividend yield on the entry price
		var sum float64
		for d, v := range t.divs[symbol] {
			if d > first.Date && d <= last.Date {
				sum += v
			}
		}
		total = price + sum/first.Close
	}
	return splitReturn{price: price, total: total, start: first.Date, end: last.Date}, true
}

// RequiredSymbolsFor computes the union of every symbol a set of strategies
// actually needs — each strategy's RequiredSymbols() (if implemented) plus its
// DefaultConfig().Benchmark — optionally including an extra symbolFilter. Used
// to scope a bar fetch to only what's needed instead of loading an entire
// multi-thousand-symbol database for a strategy that trades one symbol.
func RequiredSymbolsFor(strategies []strategy.Strategy, symbolFilter string) []string {
	requiredSet := make(map[string]struct{})

	if sym := strings.ToUpper(strings.TrimSpace(symbolFilter)); sym != "" {
		requiredSet[sym] = struct{}{}
	}
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

	out := make([]string, 0, len(requiredSet))
	for sym := range requiredSet {
		out = append(out, sym)
	}
	sort.Strings(out)
	return out
}

func DetectAndDownloadMissingData(
	targetDb string,
	tableName string,
	strategies []strategy.Strategy,
	symbolFilter string,
	autoDownload bool,
	downloadYears int,
) error {
	requiredSet := make(map[string]struct{})
	for _, sym := range RequiredSymbolsFor(strategies, symbolFilter) {
		requiredSet[sym] = struct{}{}
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

	if err := RunDownload(targetDb, tableName, missingSymbols, downloadYears); err != nil {
		return fmt.Errorf("download command failed for symbols %v: %w", missingSymbols, err)
	}

	fmt.Printf("\n✅ Successfully updated market data for %v in %s (%s).\n", missingSymbols, targetDb, tableName)
	return nil
}

func RunDownload(targetDb, targetTable string, symbols []string, years int) error {
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

	var stdout, stderr bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &stdout)
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderr)
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		if detail == "" {
			detail = err.Error()
		}
		bin := downloadBin
		if bin == "" {
			bin = "go run ./cmd/download"
		}
		return fmt.Errorf("%s -db %s -target-table %s -symbols %s -years %s: %s", bin, targetDb, targetTable, symArg, yearsArg, detail)
	}
	return nil
}
