package runner

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/olekukonko/tablewriter"
)

// SharedRunResult holds the complete output of a multi-strategy shared account backtest.
type SharedRunResult struct {
	Primary            strategy.Strategy
	Secondaries        []strategy.Strategy
	CombinedID         string // e.g. "voo-tecl-combo+mara_tree" — see SharedAccountID
	CombinedReport     models.PerformanceReport
	PerStrategyReports map[string]models.PerformanceReport
	Trades             []models.Trade
	EquityCurve        []models.DailyEquityPoint
	Signals            []models.Signal
	DbPath             string
	PreemptedCount     int
	Err                error
}

// SharedAccountID builds the combined-portfolio strategy_id used to persist and
// look up a shared account's consolidated rows (performance_summary, trades,
// equity_curve, signals). Previously this was the generic literal
// "SHARED_ACCOUNT" regardless of which strategies were actually combined — this
// makes the underlying strategy names visible directly in the ID, matching the
// output DB's own filename convention (shared_<primary>_<secondaries...>.db).
func SharedAccountID(primary strategy.Strategy, secondaries []strategy.Strategy) string {
	id := primary.ID()
	for _, sec := range secondaries {
		id += "+" + sec.ID()
	}
	return id
}

// ExecuteSharedAccount runs a multi-strategy backtest in a single shared cash account with priority preemption.
func ExecuteSharedAccount(
	primary strategy.Strategy,
	secondaries []strategy.Strategy,
	barsBySymbol map[string][]models.Bar,
	sortedDates []string,
	capital float64,
	symbolFilter string,
	outDir string,
	marketDBPath string,
) SharedRunResult {
	if primary == nil {
		return SharedRunResult{Err: fmt.Errorf("primary strategy cannot be nil")}
	}

	// 1. Create unique SQLite database for shared account
	baseName := fmt.Sprintf("shared_%s", primary.ID())
	for _, sec := range secondaries {
		baseName += fmt.Sprintf("_%s", sec.ID())
	}

	outDBPath, outDB, err := storage.CreateUniqueDB(outDir, baseName)
	if err != nil {
		return SharedRunResult{Err: fmt.Errorf("failed to create unique SQLite DB for shared account: %w", err)}
	}
	outDB.Close()

	// 2. Configure isolated calculation DBs for signal generation
	primaryCalcPath := filepath.Join(outDir, fmt.Sprintf("calc_%s.db", primary.ID()))
	primary.SetDatabases(marketDBPath, primaryCalcPath)

	for _, sec := range secondaries {
		secCalcPath := filepath.Join(outDir, fmt.Sprintf("calc_%s.db", sec.ID()))
		sec.SetDatabases(marketDBPath, secCalcPath)
	}

	// 3. Generate and tag signals with StrategyID and Priority
	var allSignals []models.Signal

	primSignals := primary.GenerateSignals(barsBySymbol)
	for i := range primSignals {
		primSignals[i].StrategyID = primary.ID()
		primSignals[i].Priority = 0
	}
	allSignals = append(allSignals, primSignals...)

	for secIdx, sec := range secondaries {
		secSignals := sec.GenerateSignals(barsBySymbol)
		for i := range secSignals {
			secSignals[i].StrategyID = sec.ID()
			secSignals[i].Priority = secIdx + 1
		}
		allSignals = append(allSignals, secSignals...)
	}

	// Optional symbol filter
	symUpper := strings.ToUpper(strings.TrimSpace(symbolFilter))
	if symUpper != "" {
		var filtered []models.Signal
		for _, s := range allSignals {
			if strings.ToUpper(s.Symbol) == symUpper {
				filtered = append(filtered, s)
			}
		}
		allSignals = filtered
	}

	// 4. Set up priority entries
	entries := []simulator.StrategyPriorityEntry{
		{
			Strategy: primary,
			Priority: 0,
			Config:   primary.DefaultConfig(),
		},
	}
	for secIdx, sec := range secondaries {
		entries = append(entries, simulator.StrategyPriorityEntry{
			Strategy: sec,
			Priority: secIdx + 1,
			Config:   sec.DefaultConfig(),
		})
	}

	// 5. Initialize and run SharedAccountSimulator
	sim := simulator.NewSharedAccountSimulator(entries, capital)

	// Set benchmark bars (default to Primary's benchmark, e.g. VOO or SPY)
	bmSymbol := primary.DefaultConfig().Benchmark
	if bmSymbol == "" {
		bmSymbol = "SPY"
	}
	if bBars, hasBm := barsBySymbol[bmSymbol]; hasBm {
		bmMap := make(map[string]models.Bar)
		for _, b := range bBars {
			bmMap[b.Date] = b
		}
		sim.SetBenchmarkBars(bmMap)
	}

	combinedReport, perStratReports, trades, equityCurve := sim.Run(allSignals, barsBySymbol, sortedDates)
	combinedID := SharedAccountID(primary, secondaries)

	// 6. Persist results to the shared SQLite database
	db, err := storage.OpenSQLite(outDBPath)
	if err != nil {
		log.Printf("Warning: Failed to re-open %s for shared account results: %v", outDBPath, err)
	} else {
		defer db.Close()

		if err := storage.SaveSignals(db, combinedID, allSignals); err != nil {
			log.Printf("Warning: Failed to save signals to %s: %v", outDBPath, err)
		}
		if err := storage.SaveTrades(db, combinedID, trades); err != nil {
			log.Printf("Warning: Failed to save trades to %s: %v", outDBPath, err)
		}
		if err := storage.SaveEquityCurve(db, combinedID, equityCurve); err != nil {
			log.Printf("Warning: Failed to save equity curve to %s: %v", outDBPath, err)
		}
		if err := storage.SavePerformanceReport(db, combinedID, combinedReport); err != nil {
			log.Printf("Warning: Failed to save combined performance summary to %s: %v", outDBPath, err)
		}

		for sID, rep := range perStratReports {
			if err := storage.SavePerformanceReport(db, sID, rep); err != nil {
				log.Printf("Warning: Failed to save performance summary for %s: %v", sID, err)
			}
		}
	}

	return SharedRunResult{
		Primary:            primary,
		Secondaries:        secondaries,
		CombinedID:         combinedID,
		CombinedReport:     combinedReport,
		PerStrategyReports: perStratReports,
		Trades:             trades,
		EquityCurve:        equityCurve,
		Signals:            allSignals,
		DbPath:             outDBPath,
		PreemptedCount:     sim.PreemptedTradeCount,
	}
}

// PrintSharedAccountTearSheet displays the consolidated portfolio and strategy attribution tables.
func PrintSharedAccountTearSheet(res SharedRunResult) {
	fmt.Printf("\n========================================================================================================================\n")
	fmt.Printf("🏛️ SHARED-ACCOUNT MULTI-STRATEGY QUANTITATIVE AUDIT\n")
	fmt.Printf("   Account Model:      Shared Cash Ledger with Dynamic Priority Preemption\n")
	fmt.Printf("   Primary Strategy:   %s (ID: %s) [Priority 0 - Capital Precedence]\n", res.Primary.Name(), res.Primary.ID())
	for i, sec := range res.Secondaries {
		fmt.Printf("   Secondary Strategy: %s (ID: %s) [Priority %d - Idle Cash Utilization]\n", sec.Name(), sec.ID(), i+1)
	}
	fmt.Printf("   Preempted Trades:   %d secondary positions liquidated to obey primary signals\n", res.PreemptedCount)
	fmt.Printf("   Results Database:   %s\n", res.DbPath)
	fmt.Printf("========================================================================================================================\n")

	// Print Consolidated Tear Sheet
	PrintPerformanceTearSheet("Shared Account Portfolio", res.CombinedReport)

	// Strategy Attribution Breakdown Table
	fmt.Printf("\n========================================================================================================================\n")
	fmt.Printf("📊 STRATEGY CONTRIBUTION & PREEMPTION BREAKDOWN\n")
	fmt.Printf("========================================================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Role", "Strategy ID", "Total Trades", "Wins / Losses", "Win Rate", "Preempted", "Net Realized PnL", "CAGR", "Sharpe", "Max DD", "DD Duration"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	// Primary
	primID := res.Primary.ID()
	primRep := res.PerStrategyReports[primID]
	table.Append([]string{
		"PRIMARY (P0)",
		primID,
		fmt.Sprintf("%d", primRep.TotalTrades),
		fmt.Sprintf("%d / %d", primRep.WinningTrades, primRep.LosingTrades),
		fmt.Sprintf("%.1f%%", primRep.WinRate*100),
		"0 (Never)",
		fmt.Sprintf("$%.2f", primRep.NetProfit),
		fmt.Sprintf("%.2f%%", primRep.CAGR*100),
		fmt.Sprintf("%.2f", primRep.SharpeRatio),
		fmt.Sprintf("%.2f%%", primRep.MaxDrawdownPct*100),
		fmt.Sprintf("%dd", primRep.MaxDrawdownDuration),
	})

	// Secondaries
	for _, sec := range res.Secondaries {
		secID := sec.ID()
		secRep := res.PerStrategyReports[secID]

		secPreempted := 0
		for _, t := range res.Trades {
			if t.StrategyID == secID && t.ExitReason == models.ExitReasonPreempted {
				secPreempted++
			}
		}

		table.Append([]string{
			"SECONDARY (P1+)",
			secID,
			fmt.Sprintf("%d", secRep.TotalTrades),
			fmt.Sprintf("%d / %d", secRep.WinningTrades, secRep.LosingTrades),
			fmt.Sprintf("%.1f%%", secRep.WinRate*100),
			fmt.Sprintf("%d Trades", secPreempted),
			fmt.Sprintf("$%.2f", secRep.NetProfit),
			fmt.Sprintf("%.2f%%", secRep.CAGR*100),
			fmt.Sprintf("%.2f", secRep.SharpeRatio),
			fmt.Sprintf("%.2f%%", secRep.MaxDrawdownPct*100),
			fmt.Sprintf("%dd", secRep.MaxDrawdownDuration),
		})
	}

	table.Render()

	// Print Preemption Audit Table if any positions were preempted
	var preemptedTrades []models.Trade
	for _, t := range res.Trades {
		if t.ExitReason == models.ExitReasonPreempted {
			preemptedTrades = append(preemptedTrades, t)
		}
	}

	if len(preemptedTrades) > 0 {
		fmt.Printf("\n⚡ PREEMPTED POSITIONS AUDIT (Liquidated at Market to Fund Primary Signals):\n")
		pTable := tablewriter.NewWriter(os.Stdout)
		pTable.SetHeader([]string{"Trade ID", "Strategy", "Symbol", "Entry Date", "Entry Price", "Exit Date", "Exit Price", "Hold Days", "Preemption PnL", "Return %"})
		pTable.SetBorder(true)

		for _, t := range preemptedTrades {
			pTable.Append([]string{
				fmt.Sprintf("%d", t.ID),
				t.StrategyID,
				t.Symbol,
				t.EntryDate,
				fmt.Sprintf("$%.2f", t.EntryPrice),
				t.ExitDate,
				fmt.Sprintf("$%.2f", t.ExitPrice),
				fmt.Sprintf("%d d", t.HoldDays),
				fmt.Sprintf("$%.2f", t.NetPnL),
				fmt.Sprintf("%.2f%%", t.ReturnPct*100),
			})
		}
		pTable.Render()
	}

	// Print Sample Recent Trades
	PrintTradesTable(res.Trades, "")
}
