package runner

import (
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

// This file is the single shared implementation of "has this strategy already
// been backtested, and is that result trustworthy?" — used by every command that
// runs strategies in bulk (cmd/backtest -strategy all, cmd/scoreboard) so that
// skip-duplicate-work logic lives in one place instead of being reinvented (or
// forgotten) per command.

// CompiledResult is the winning (highest-increment, non-compromised) result for
// one strategy found in an existing results directory.
type CompiledResult struct {
	StrategyID string
	Report     models.PerformanceReport
	DbPath     string
	Increment  int
}

// candidate is one result DB file competing to represent its strategy — there's
// one per run increment (dt_hibl.db, dt_hibl_2.db, ...).
type candidate struct {
	path      string
	increment int
}

// dbIncrementPattern matches storage.CreateUniqueDB's naming scheme: the first
// run of a strategy is "<id>.db", every repeat run after that is "<id>_<n>.db"
// for n = 2, 3, 4, .... Captures the base id and the trailing numeric suffix.
var dbIncrementPattern = regexp.MustCompile(`^(.+)_(\d+)$`)

// parseDBIncrement splits a result DB's filename into its strategy base name and
// run increment (1 for the un-suffixed first run, 2+ for "_<n>" repeat runs).
func parseDBIncrement(path string) (base string, increment int) {
	stem := strings.TrimSuffix(filepath.Base(path), ".db")
	if m := dbIncrementPattern.FindStringSubmatch(stem); m != nil {
		if n, err := strconv.Atoi(m[2]); err == nil {
			return m[1], n
		}
	}
	return stem, 1
}

// PerformanceRow is one row of a result DB's performance_summary table.
type PerformanceRow struct {
	StrategyID string
	Report     models.PerformanceReport
}

// ValidatePerformanceSummary opens a single result DB, confirms it isn't
// compromised, and reads every row of its performance_summary table (normally
// exactly one, keyed by strategy_id). "Compromised" covers everything that can go
// wrong with one of these files in practice: the SQLite file itself is corrupt or
// truncated (e.g. the process was killed mid-write), or it's a schema-only stub
// with no performance_summary rows at all (e.g. from an interrupted backtest run
// that created the file via storage.CreateUniqueDB but never got to write results).
// Any of these return an error so the caller can fall back to a previous run
// increment instead of silently compiling a blank/broken row.
func ValidatePerformanceSummary(path string) ([]PerformanceRow, error) {
	db, err := storage.OpenSQLite(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open: %w", err)
	}
	defer db.Close()

	var integrity string
	if err := db.QueryRow(`PRAGMA integrity_check;`).Scan(&integrity); err != nil {
		return nil, fmt.Errorf("integrity_check query failed: %w", err)
	}
	if integrity != "ok" {
		return nil, fmt.Errorf("integrity_check failed: %s", integrity)
	}

	var tableExists string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='performance_summary'`).Scan(&tableExists)
	if err != nil {
		return nil, fmt.Errorf("no performance_summary table (schema-only stub?)")
	}

	rows, err := db.Query(`
		SELECT strategy_id, start_date, end_date, total_trading_days, total_calendar_years,
			initial_capital, final_equity, net_profit, total_return_pct, cagr,
			sharpe_ratio, sortino_ratio, calmar_ratio, omega_ratio, ulcer_index,
			alpha, beta, benchmark_return_pct, max_drawdown_pct, max_drawdown_dollars,
			max_drawdown_peak_equity, max_drawdown_trough_equity,
			max_drawdown_peak_date, max_drawdown_trough_date, max_drawdown_days,
			total_trades, winning_trades, losing_trades, win_rate, profit_factor,
			avg_trade_return_pct, avg_win_amount, avg_loss_amount, payoff_ratio,
			avg_holding_days, avg_mae, avg_mfe, total_commission_paid
		FROM performance_summary
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query performance_summary: %w", err)
	}
	defer rows.Close()

	var out []PerformanceRow
	for rows.Next() {
		var r PerformanceRow
		var rep models.PerformanceReport
		if err := rows.Scan(
			&r.StrategyID, &rep.StartDate, &rep.EndDate, &rep.TotalTradingDays, &rep.TotalCalendarYears,
			&rep.InitialCapital, &rep.FinalEquity, &rep.NetProfit, &rep.TotalReturnPct, &rep.CAGR,
			&rep.SharpeRatio, &rep.SortinoRatio, &rep.CalmarRatio, &rep.OmegaRatio, &rep.UlcerIndex,
			&rep.Alpha, &rep.Beta, &rep.BenchmarkReturnPct, &rep.MaxDrawdownPct, &rep.MaxDrawdownDollars,
			&rep.MaxDrawdownPeakEquity, &rep.MaxDrawdownTroughEquity,
			&rep.MaxDrawdownPeakDate, &rep.MaxDrawdownTroughDate, &rep.MaxDrawdownDuration,
			&rep.TotalTrades, &rep.WinningTrades, &rep.LosingTrades, &rep.WinRate, &rep.ProfitFactor,
			&rep.AvgTradeReturnPct, &rep.AvgWinAmount, &rep.AvgLossAmount, &rep.PayoffRatio,
			&rep.AvgHoldingDays, &rep.AvgMAE, &rep.AvgMFE, &rep.TotalCommissionPaid,
		); err != nil {
			return nil, fmt.Errorf("malformed performance_summary row: %w", err)
		}
		r.Report = rep
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("performance_summary table is empty")
	}
	return out, nil
}

// fallbackNote describes what happens next when a candidate fails validation.
func fallbackNote(i int, cands []candidate) string {
	if i+1 < len(cands) {
		return fmt.Sprintf(" — reverting to run increment %d", cands[i+1].increment)
	}
	return " — no earlier run increment available, skipping this strategy"
}

// ScanAndValidate globs every result DB in outDir, groups them by strategy, and
// for each strategy tries its run increments highest-first (see
// ValidatePerformanceSummary), skipping any that are compromised, until it finds
// a usable one or runs out. This is the one shared "what's already been
// backtested, and can I trust it?" pass every bulk-strategy command should run
// before doing any work — never assume nothing's done, and never blindly redo
// everything.
func ScanAndValidate(outDir string, concurrency int) (byStrategy map[string]CompiledResult, totalFiles, totalGroups, usedFallback, allCompromised int) {
	files, err := filepath.Glob(filepath.Join(outDir, "*.db"))
	if err != nil {
		log.Fatalf("Failed to list %s/*.db: %v", outDir, err)
	}

	groups := make(map[string][]candidate)
	for _, f := range files {
		if filepath.Base(f) == "scoreboard.db" {
			continue
		}
		base, inc := parseDBIncrement(f)
		groups[base] = append(groups[base], candidate{path: f, increment: inc})
	}
	totalFiles = len(files)
	totalGroups = len(groups)
	if totalGroups == 0 {
		return map[string]CompiledResult{}, totalFiles, totalGroups, 0, 0
	}
	for base := range groups {
		sort.Slice(groups[base], func(i, j int) bool {
			return groups[base][i].increment > groups[base][j].increment // highest increment first
		})
	}

	fmt.Printf("   Found %d result databases across %d strategies. Validating with %d workers (highest run increment first, falling back on corruption)...\n\n",
		totalFiles, totalGroups, concurrency)

	var mu sync.Mutex
	byStrategy = make(map[string]CompiledResult)

	bases := make([]string, 0, len(groups))
	for base := range groups {
		bases = append(bases, base)
	}
	jobs := make(chan string, len(bases))
	for _, b := range bases {
		jobs <- b
	}
	close(jobs)

	var wg sync.WaitGroup
	if concurrency < 1 {
		concurrency = 1
	}
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for base := range jobs {
				cands := groups[base]
				var rows []PerformanceRow
				var chosen candidate
				fellBack := false
				for i, c := range cands {
					r, err := ValidatePerformanceSummary(c.path)
					if err != nil {
						log.Printf("⚠️  [%s] run increment %d (%s) is compromised (%v)%s",
							base, c.increment, filepath.Base(c.path), err, fallbackNote(i, cands))
						fellBack = true
						continue
					}
					rows = r
					chosen = c
					break
				}
				if rows == nil {
					mu.Lock()
					allCompromised++
					mu.Unlock()
					continue
				}

				mu.Lock()
				if fellBack {
					usedFallback++
				}
				for _, row := range rows {
					byStrategy[row.StrategyID] = CompiledResult{
						StrategyID: row.StrategyID,
						Report:     row.Report,
						DbPath:     chosen.path,
						Increment:  chosen.increment,
					}
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	return byStrategy, totalFiles, totalGroups, usedFallback, allCompromised
}

// MissingStrategies returns the IDs of every currently-registered strategy that
// has no usable (non-compromised) entry in byStrategy — i.e. it has genuinely
// never been backtested, or every run it has was corrupted/incomplete.
func MissingStrategies(byStrategy map[string]CompiledResult) []string {
	var missing []string
	for _, s := range strategy.List() {
		if _, ok := byStrategy[s.ID()]; !ok {
			missing = append(missing, s.ID())
		}
	}
	sort.Strings(missing)
	return missing
}

// ResolveStrategy looks up the live registered strategy for display purposes
// (name/description); falls back to a minimal stub carrying just the ID when a
// result DB refers to a strategy that's no longer registered (renamed/removed).
func ResolveStrategy(id string) strategy.Strategy {
	if s, ok := strategy.Get(id); ok {
		return s
	}
	return &staleStrategy{id: id}
}

type staleStrategy struct{ id string }

func (s *staleStrategy) ID() string                                              { return s.id }
func (s *staleStrategy) Name() string                                            { return s.id + " (not currently registered)" }
func (s *staleStrategy) Description() string                                     { return "" }
func (s *staleStrategy) DefaultConfig() strategy.StrategyConfig                  { return strategy.StrategyConfig{} }
func (s *staleStrategy) Validate() error                                         { return nil }
func (s *staleStrategy) SetDatabases(a, b string)                                {}
func (s *staleStrategy) GenerateSignals(map[string][]models.Bar) []models.Signal { return nil }

// PrintMissingList prints strategy IDs one per line, capped so output against
// hundreds of missing strategies doesn't flood the terminal.
func PrintMissingList(ids []string) {
	const maxShown = 25
	for i, id := range ids {
		if i >= maxShown {
			fmt.Printf("   ... and %d more\n", len(ids)-maxShown)
			break
		}
		fmt.Printf("   - %s\n", id)
	}
}
