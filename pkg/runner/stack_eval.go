package runner

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/olekukonko/tablewriter"
)

// OverlayCandidateOptions controls which existing strategies are tried as
// idle-cash overlays on a primary. Nothing here registers a new strategy.
type OverlayCandidateOptions struct {
	// ExplicitIDs, when non-empty, is the exact overlay list (minus the primary).
	ExplicitIDs []string
	// IncludeUniverse adds strategies that scan the full bar universe
	// (bb-capitulation, rsi2, ...). Off by default — they need every symbol.
	IncludeUniverse bool
	// IncludeDT adds auto-fit dt_* ETF decision trees. Off by default because
	// 500+ trees make a sweep too slow; use DTTop to cap the strongest ones.
	IncludeDT bool
	// DTTop is how many highest-scored dt_* trees to include when IncludeDT.
	// 0 defaults to 15.
	DTTop int
}

// OverlayEval is one pairwise stack: primary + a single overlay candidate.
type OverlayEval struct {
	Secondary         strategy.Strategy
	Combined          models.PerformanceReport
	SecondaryReport   models.PerformanceReport
	Idle              simulator.IdleStats
	IncrementalEquity float64 // combined final equity minus primary-only baseline
	SecondaryPnL      float64
	SecondaryTrades   int
	Preempted         int
	TradedSymbols     []string
	Err               error
}

// StackEvalResult is the full idle-overlay sweep for one primary.
type StackEvalResult struct {
	Primary        strategy.Strategy
	Baseline       SharedRunResult
	Overlays       []OverlayEval
	BestStack      *SharedRunResult // greedy N-way stack of complementary overlays
	BestStackIDs   []string
	RankingsDBPath string
}

// OverlayCandidates returns existing registered strategies suitable as idle-cash
// overlays on primary. SQL-pipeline duplicates, buy-and-hold, and the primary
// itself are never returned.
func OverlayCandidates(primary strategy.Strategy, opts OverlayCandidateOptions) []strategy.Strategy {
	if primary == nil {
		return nil
	}
	primaryID := primary.ID()

	if len(opts.ExplicitIDs) > 0 {
		var out []strategy.Strategy
		seen := map[string]bool{primaryID: true}
		for _, id := range opts.ExplicitIDs {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			s, ok := strategy.Get(id)
			if !ok {
				log.Printf("stack-eval: unknown overlay strategy %q — skipping", id)
				continue
			}
			if s.ID() == primaryID {
				continue
			}
			seen[s.ID()] = true
			out = append(out, s)
		}
		return out
	}

	dtLimit := opts.DTTop
	if dtLimit <= 0 {
		dtLimit = 15
	}
	allowedDT := map[string]bool{}
	if opts.IncludeDT {
		for _, id := range strategy.RankedETFDecisionTreeIDs(dtLimit) {
			allowedDT[id] = true
		}
	}

	var out []strategy.Strategy
	for _, s := range strategy.List() {
		if !isEligibleOverlay(primary, s, opts, allowedDT) {
			continue
		}
		out = append(out, s)
	}
	return out
}

func isEligibleOverlay(primary, cand strategy.Strategy, opts OverlayCandidateOptions, allowedDT map[string]bool) bool {
	id := cand.ID()
	if id == primary.ID() {
		return false
	}
	if strings.HasSuffix(id, "-sql") {
		return false
	}
	if id == "voo-buy-hold" || id == "genetic-momentum" {
		return false
	}
	if isSiblingCombo(primary.ID(), id) {
		return false
	}
	if strings.HasPrefix(id, "dt_") {
		return opts.IncludeDT && allowedDT[id]
	}
	if _, ok := cand.(strategy.RequiredSymbolsProvider); !ok {
		return opts.IncludeUniverse
	}
	return true
}

func isSiblingCombo(primaryID, candID string) bool {
	siblings := map[string]string{
		"sig-voo-buy-tecl":    "voo-tecl-spxu-combo",
		"voo-tecl-spxu-combo": "sig-voo-buy-tecl",
	}
	return siblings[primaryID] == candID
}

// StackEvalOptions is the idle-overlay sweep configuration.
type StackEvalOptions struct {
	Primary      strategy.Strategy
	Candidates   []strategy.Strategy
	BarsBySymbol map[string][]models.Bar
	SortedDates  []string
	Capital      float64
	OutDir       string
	MarketDBPath string
	Concurrency  int
	// StackDepth is how many complementary overlays to greedily combine after
	// the pairwise ranking. 1 = pairwise only; default 3.
	StackDepth  int
	PersistBest bool
}

// ExecuteStackEval runs the primary standalone, then each candidate as a
// secondary on the same cash ledger, ranking overlays by incremental equity
// versus the primary-only baseline. The winning complementary overlays are
// then re-run as one N-way stack.
func ExecuteStackEval(opts StackEvalOptions) StackEvalResult {
	res := StackEvalResult{Primary: opts.Primary}
	if opts.Primary == nil {
		return res
	}
	if opts.Capital <= 0 {
		opts.Capital = 100000
	}
	if opts.OutDir == "" {
		opts.OutDir = "reports"
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.StackDepth <= 0 {
		opts.StackDepth = 3
	}

	calcDir := filepath.Join(opts.OutDir, "stack_eval_calc")
	_ = os.MkdirAll(calcDir, 0755)

	dates := scopeDatesToStrategy(opts.Primary, opts.Primary.DefaultConfig(), opts.BarsBySymbol, opts.SortedDates)

	// Generate primary signals once and reuse across every overlay.
	opts.Primary.SetDatabases(opts.MarketDBPath, filepath.Join(calcDir, fmt.Sprintf("calc_%s.db", opts.Primary.ID())))
	primSignals := tagSignals(opts.Primary.GenerateSignals(opts.BarsBySymbol), opts.Primary.ID(), 0)

	baseline := ExecuteStack(StackRequest{
		Primary:      opts.Primary,
		BarsBySymbol: opts.BarsBySymbol,
		SortedDates:  dates,
		Capital:      opts.Capital,
		MarketDBPath: opts.MarketDBPath,
		Persist:      false,
		Signals:      primSignals,
		CalcDir:      calcDir,
	})
	res.Baseline = baseline
	baselineEquity := baseline.CombinedReport.FinalEquity

	type job struct {
		idx  int
		cand strategy.Strategy
	}
	overlays := make([]OverlayEval, len(opts.Candidates))
	jobs := make(chan job)
	var wg sync.WaitGroup
	workers := opts.Concurrency
	if workers > len(opts.Candidates) && len(opts.Candidates) > 0 {
		workers = len(opts.Candidates)
	}
	if workers < 1 {
		workers = 1
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				overlays[j.idx] = evaluateOverlay(opts, dates, calcDir, primSignals, baselineEquity, j.cand)
			}
		}()
	}
	for i, cand := range opts.Candidates {
		jobs <- job{idx: i, cand: cand}
	}
	close(jobs)
	wg.Wait()

	sort.Slice(overlays, func(i, j int) bool {
		if overlays[i].Err != nil && overlays[j].Err == nil {
			return false
		}
		if overlays[i].Err == nil && overlays[j].Err != nil {
			return true
		}
		if overlays[i].IncrementalEquity != overlays[j].IncrementalEquity {
			return overlays[i].IncrementalEquity > overlays[j].IncrementalEquity
		}
		return overlays[i].Secondary.ID() < overlays[j].Secondary.ID()
	})
	res.Overlays = overlays

	if opts.StackDepth >= 1 {
		picked := pickComplementaryOverlays(overlays, opts.StackDepth)
		if len(picked) > 0 {
			var secs []strategy.Strategy
			var ids []string
			merged := append([]models.Signal{}, primSignals...)
			for i, ov := range picked {
				secs = append(secs, ov.Secondary)
				ids = append(ids, ov.Secondary.ID())
				ov.Secondary.SetDatabases(opts.MarketDBPath, filepath.Join(calcDir, fmt.Sprintf("calc_%s.db", ov.Secondary.ID())))
				secSigs := tagSignals(ov.Secondary.GenerateSignals(opts.BarsBySymbol), ov.Secondary.ID(), i+1)
				merged = append(merged, secSigs...)
			}
			best := ExecuteStack(StackRequest{
				Primary:      opts.Primary,
				Secondaries:  secs,
				BarsBySymbol: opts.BarsBySymbol,
				SortedDates:  dates,
				Capital:      opts.Capital,
				OutDir:       opts.OutDir,
				MarketDBPath: opts.MarketDBPath,
				Persist:      opts.PersistBest,
				Signals:      merged,
				CalcDir:      calcDir,
			})
			res.BestStack = &best
			res.BestStackIDs = ids
		}
	}

	res.RankingsDBPath = persistStackEval(opts.OutDir, res)
	return res
}

func evaluateOverlay(
	opts StackEvalOptions,
	dates []string,
	calcDir string,
	primSignals []models.Signal,
	baselineEquity float64,
	cand strategy.Strategy,
) OverlayEval {
	eval := OverlayEval{Secondary: cand}
	cand.SetDatabases(opts.MarketDBPath, filepath.Join(calcDir, fmt.Sprintf("calc_%s.db", cand.ID())))
	secSigs := tagSignals(cand.GenerateSignals(opts.BarsBySymbol), cand.ID(), 1)
	eval.TradedSymbols = uniqueSignalSymbols(secSigs)

	merged := append([]models.Signal{}, primSignals...)
	merged = append(merged, secSigs...)

	run := ExecuteStack(StackRequest{
		Primary:      opts.Primary,
		Secondaries:  []strategy.Strategy{cand},
		BarsBySymbol: opts.BarsBySymbol,
		SortedDates:  dates,
		Capital:      opts.Capital,
		MarketDBPath: opts.MarketDBPath,
		Persist:      false,
		Signals:      merged,
		CalcDir:      calcDir,
	})
	if run.Err != nil {
		eval.Err = run.Err
		return eval
	}
	eval.Combined = run.CombinedReport
	eval.SecondaryReport = run.PerStrategyReports[cand.ID()]
	eval.Idle = run.Idle
	eval.IncrementalEquity = run.CombinedReport.FinalEquity - baselineEquity
	eval.SecondaryPnL = eval.SecondaryReport.NetProfit
	eval.SecondaryTrades = eval.SecondaryReport.TotalTrades
	eval.Preempted = run.PreemptedCount
	return eval
}

func pickComplementaryOverlays(overlays []OverlayEval, depth int) []OverlayEval {
	var picked []OverlayEval
	used := map[string]bool{}
	for _, ov := range overlays {
		if ov.Err != nil || ov.IncrementalEquity <= 0 {
			continue
		}
		overlap := false
		for _, sym := range ov.TradedSymbols {
			if used[sym] {
				overlap = true
				break
			}
		}
		if overlap {
			continue
		}
		for _, sym := range ov.TradedSymbols {
			used[sym] = true
		}
		picked = append(picked, ov)
		if len(picked) >= depth {
			break
		}
	}
	return picked
}

func tagSignals(sigs []models.Signal, strategyID string, priority int) []models.Signal {
	out := make([]models.Signal, len(sigs))
	copy(out, sigs)
	for i := range out {
		out[i].StrategyID = strategyID
		out[i].Priority = priority
	}
	return out
}

func uniqueSignalSymbols(sigs []models.Signal) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range sigs {
		sym := strings.ToUpper(strings.TrimSpace(s.Symbol))
		if sym == "" || seen[sym] {
			continue
		}
		seen[sym] = true
		out = append(out, sym)
	}
	sort.Strings(out)
	return out
}

func persistStackEval(outDir string, res StackEvalResult) string {
	if res.Primary == nil {
		return ""
	}
	path := filepath.Join(outDir, fmt.Sprintf("stack_eval_%s.db", sanitizeID(res.Primary.ID())))
	db, err := storage.OpenSQLite(path)
	if err != nil {
		log.Printf("Warning: failed to open stack-eval rankings DB %s: %v", path, err)
		return ""
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS overlay_rankings (
			rank INTEGER,
			secondary_id TEXT,
			combined_final_equity REAL,
			combined_cagr REAL,
			combined_sharpe REAL,
			combined_max_dd REAL,
			incremental_equity REAL,
			secondary_pnl REAL,
			secondary_trades INTEGER,
			preempted INTEGER,
			avg_cash_pct REAL,
			fully_idle_pct REAL
		);
		DELETE FROM overlay_rankings;
	`)
	if err != nil {
		log.Printf("Warning: failed to create overlay_rankings: %v", err)
		return path
	}

	tx, err := db.Beginx()
	if err != nil {
		return path
	}
	stmt, err := tx.Preparex(`INSERT INTO overlay_rankings
		(rank, secondary_id, combined_final_equity, combined_cagr, combined_sharpe, combined_max_dd,
		 incremental_equity, secondary_pnl, secondary_trades, preempted, avg_cash_pct, fully_idle_pct)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return path
	}
	defer stmt.Close()
	for i, ov := range res.Overlays {
		if ov.Err != nil {
			continue
		}
		_, _ = stmt.Exec(
			i+1,
			ov.Secondary.ID(),
			ov.Combined.FinalEquity,
			ov.Combined.CAGR,
			ov.Combined.SharpeRatio,
			ov.Combined.MaxDrawdownPct,
			ov.IncrementalEquity,
			ov.SecondaryPnL,
			ov.SecondaryTrades,
			ov.Preempted,
			ov.Idle.AvgCashPct,
			ov.Idle.FullyIdlePct,
		)
	}
	_ = tx.Commit()
	return path
}

func sanitizeID(id string) string {
	id = strings.ReplaceAll(id, "+", "_")
	id = strings.ReplaceAll(id, "/", "_")
	id = strings.ReplaceAll(id, " ", "_")
	return id
}

// PrintStackEvalTearSheet prints the primary idle snapshot, overlay ranking,
// and optional greedy N-way stack.
func PrintStackEvalTearSheet(res StackEvalResult) {
	if res.Primary == nil {
		fmt.Println("stack-eval: no primary strategy")
		return
	}
	base := res.Baseline.CombinedReport
	idle := res.Baseline.Idle

	fmt.Printf("\n========================================================================================================================\n")
	fmt.Printf("STACK EVAL — idle-cash overlays on %s\n", res.Primary.ID())
	fmt.Printf("   One cash ledger; only the primary may preempt subordinates. Overlays are existing strategies, not new pkg/strategy types.\n")
	fmt.Printf("   Window:              %s ➔ %s (%.1f years, %d trading days)\n",
		base.StartDate, base.EndDate, base.TotalCalendarYears, base.TotalTradingDays)
	fmt.Printf("   Primary-only equity: $%.2f  (CAGR %.2f%%, Sharpe %.2f, MaxDD %.2f%%)\n",
		base.FinalEquity, base.CAGR*100, base.SharpeRatio, base.MaxDrawdownPct*100)
	fmt.Printf("   Primary idle cash:   avg %.1f%% of equity | fully flat %.1f%% of days (%d / %d) | deployed avg %.1f%%\n",
		idle.AvgCashPct*100, idle.FullyIdlePct*100, idle.DaysFullyIdle, idle.TradingDays, idle.AvgDeployedPct*100)
	if res.RankingsDBPath != "" {
		fmt.Printf("   Rankings database:   %s\n", res.RankingsDBPath)
	}
	fmt.Printf("========================================================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{
		"Rank", "Overlay", "Δ Equity vs Primary", "Stacked Equity", "Stacked CAGR", "Sharpe", "Max DD",
		"Overlay PnL", "Trades", "Preempted", "Avg Cash %",
	})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for i, ov := range res.Overlays {
		if ov.Err != nil {
			table.Append([]string{
				fmt.Sprintf("%d", i+1),
				ov.Secondary.ID(),
				"ERROR",
				ov.Err.Error(),
				"", "", "", "", "", "", "",
			})
			continue
		}
		delta := ov.IncrementalEquity
		sign := "+"
		if delta < 0 {
			sign = ""
		}
		table.Append([]string{
			fmt.Sprintf("%d", i+1),
			ov.Secondary.ID(),
			fmt.Sprintf("%s$%.2f", sign, delta),
			fmt.Sprintf("$%.2f", ov.Combined.FinalEquity),
			fmt.Sprintf("%.2f%%", ov.Combined.CAGR*100),
			fmt.Sprintf("%.2f", ov.Combined.SharpeRatio),
			fmt.Sprintf("%.2f%%", ov.Combined.MaxDrawdownPct*100),
			fmt.Sprintf("$%.2f", ov.SecondaryPnL),
			fmt.Sprintf("%d", ov.SecondaryTrades),
			fmt.Sprintf("%d", ov.Preempted),
			fmt.Sprintf("%.1f%%", ov.Idle.AvgCashPct*100),
		})
	}
	table.Render()

	if len(res.Overlays) > 0 && res.Overlays[0].Err == nil {
		best := res.Overlays[0]
		fmt.Printf("\nBest pairwise overlay: %s  (adds $%.2f vs running %s alone)\n",
			best.Secondary.ID(), best.IncrementalEquity, res.Primary.ID())
	}

	if res.BestStack != nil && len(res.BestStackIDs) > 0 {
		fmt.Printf("\nGreedy complementary stack: %s\n", res.BestStack.CombinedID)
		PrintSharedAccountTearSheet(*res.BestStack)
	}
}
