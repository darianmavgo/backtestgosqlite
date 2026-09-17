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
	CombinedID         string // e.g. "sig-voo-buy-tecl+mara_tree" — see SharedAccountID
	CombinedReport     models.PerformanceReport
	PerStrategyReports map[string]models.PerformanceReport
	Trades             []models.Trade
	EquityCurve        []models.DailyEquityPoint
	Signals            []models.Signal
	DbPath             string
	PreemptedCount     int
	Idle               simulator.IdleStats
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

// StackRequest is a first-class multi-strategy run on one cash ledger.
// List order is fill order: Primary = 0 (only it may preempt subordinates
// for capital or a symbol), Secondaries[i] = i+1 (idle-cash utilization;
// they never evict each other).
type StackRequest struct {
	Primary      strategy.Strategy
	Secondaries  []strategy.Strategy
	BarsBySymbol map[string][]models.Bar
	SortedDates  []string
	Capital      float64
	SymbolFilter string
	OutDir       string
	MarketDBPath string
	// Persist writes a unique reports/shared_*.db. Stack-eval pairwise
	// sweeps set this false so they don't flood reports/.
	Persist bool
	// Signals, when non-nil, skips GenerateSignals (used to reuse a primary's
	// already-generated signals across overlay candidates).
	Signals []models.Signal
	// CalcDir holds isolated SQL-pipeline calc DBs. Defaults to OutDir.
	CalcDir string
}

// ExecuteSharedAccount runs a stacked backtest and persists a unique results DB.
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
	return ExecuteStack(StackRequest{
		Primary:      primary,
		Secondaries:  secondaries,
		BarsBySymbol: barsBySymbol,
		SortedDates:  sortedDates,
		Capital:      capital,
		SymbolFilter: symbolFilter,
		OutDir:       outDir,
		MarketDBPath: marketDBPath,
		Persist:      true,
	})
}

// ExecuteStack runs existing strategies as a priority stack on one cash ledger.
// No new pkg/strategy types are created — stacking is an engine/runner concern.
func ExecuteStack(req StackRequest) SharedRunResult {
	if req.Primary == nil {
		return SharedRunResult{Err: fmt.Errorf("primary strategy cannot be nil")}
	}

	outDir := req.OutDir
	if outDir == "" {
		outDir = "reports"
	}
	calcDir := req.CalcDir
	if calcDir == "" {
		calcDir = outDir
	}

	var outDBPath string
	if req.Persist {
		baseName := fmt.Sprintf("shared_%s", req.Primary.ID())
		for _, sec := range req.Secondaries {
			baseName += fmt.Sprintf("_%s", sec.ID())
		}
		path, outDB, err := storage.CreateUniqueDB(outDir, baseName)
		if err != nil {
			return SharedRunResult{Err: fmt.Errorf("failed to create unique SQLite DB for shared account: %w", err)}
		}
		outDB.Close()
		outDBPath = path
	}

	allSignals := req.Signals
	if allSignals == nil {
		if err := os.MkdirAll(calcDir, 0755); err != nil {
			return SharedRunResult{Err: fmt.Errorf("failed to create calc dir %s: %w", calcDir, err)}
		}
		req.Primary.SetDatabases(req.MarketDBPath, filepath.Join(calcDir, fmt.Sprintf("calc_%s.db", req.Primary.ID())))
		primSignals := req.Primary.GenerateSignals(req.BarsBySymbol)
		for i := range primSignals {
			primSignals[i].StrategyID = req.Primary.ID()
			primSignals[i].Priority = 0
		}
		allSignals = append(allSignals, primSignals...)

		for secIdx, sec := range req.Secondaries {
			sec.SetDatabases(req.MarketDBPath, filepath.Join(calcDir, fmt.Sprintf("calc_%s.db", sec.ID())))
			secSignals := sec.GenerateSignals(req.BarsBySymbol)
			for i := range secSignals {
				secSignals[i].StrategyID = sec.ID()
				secSignals[i].Priority = secIdx + 1
			}
			allSignals = append(allSignals, secSignals...)
		}
	}

	symUpper := strings.ToUpper(strings.TrimSpace(req.SymbolFilter))
	if symUpper != "" {
		var filtered []models.Signal
		for _, s := range allSignals {
			if strings.ToUpper(s.Symbol) == symUpper {
				filtered = append(filtered, s)
			}
		}
		allSignals = filtered
	}

	entries := []simulator.StrategyPriorityEntry{
		{Strategy: req.Primary, Priority: 0, Config: req.Primary.DefaultConfig()},
	}
	for secIdx, sec := range req.Secondaries {
		entries = append(entries, simulator.StrategyPriorityEntry{
			Strategy: sec,
			Priority: secIdx + 1,
			Config:   sec.DefaultConfig(),
		})
	}

	sim := simulator.NewSharedAccountSimulator(entries, req.Capital)

	bmSymbol := req.Primary.DefaultConfig().Benchmark
	if bmSymbol == "" {
		bmSymbol = "SPY"
	}
	if bBars, hasBm := req.BarsBySymbol[bmSymbol]; hasBm {
		bmMap := make(map[string]models.Bar)
		for _, b := range bBars {
			bmMap[b.Date] = b
		}
		sim.SetBenchmarkBars(bmMap)
	}

	combinedReport, perStratReports, trades, equityCurve := sim.Run(allSignals, req.BarsBySymbol, req.SortedDates)
	combinedID := SharedAccountID(req.Primary, req.Secondaries)
	idle := simulator.CalculateIdleStats(equityCurve)

	if req.Persist && outDBPath != "" {
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
	}

	return SharedRunResult{
		Primary:            req.Primary,
		Secondaries:        req.Secondaries,
		CombinedID:         combinedID,
		CombinedReport:     combinedReport,
		PerStrategyReports: perStratReports,
		Trades:             trades,
		EquityCurve:        equityCurve,
		Signals:            allSignals,
		DbPath:             outDBPath,
		PreemptedCount:     sim.PreemptedTradeCount,
		Idle:               idle,
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
	fmt.Printf("   Idle Cash:          avg %.1f%% of equity (fully flat %.1f%% of days, deployed %.1f%%)\n",
		res.Idle.AvgCashPct*100, res.Idle.FullyIdlePct*100, res.Idle.AvgDeployedPct*100)
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
