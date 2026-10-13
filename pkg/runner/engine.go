package runner

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/hoursim"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/options"
	"github.com/darianmavgo/backtestgosqlite/pkg/sessions"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

// legacyExtras are the figures only the daily simulator produces.
type legacyExtras struct {
	marginInterest, dividendCash float64
}

// signalSymbols lists the distinct symbols the signals name, in order of first appearance.
func signalSymbols(signals []models.Signal) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range signals {
		if !seen[s.Symbol] {
			seen[s.Symbol] = true
			out = append(out, s.Symbol)
		}
	}
	return out
}

// hourlyBars loads the hourly bars of the signals' symbols. A database without them gives none
// and the run plays each session as its daily bar, which hoursim reports.
func hourlyBars(marketDB string, signals []models.Signal) map[string][]models.Bar {
	syms := signalSymbols(signals)
	if len(syms) == 0 || marketDB == "" {
		return nil
	}
	// Opening a SQLite path creates it, and a market database with no hourly file beside it has
	// nothing to load.
	if _, err := os.Stat(appenv.BarDB(marketDB, "1h")); err != nil {
		return nil
	}
	in, err := LoadFillBars(marketDB, syms)
	if err != nil {
		log.Printf("Warning: no hourly bars from %s: %v; sessions are played as daily bars", marketDB, err)
		return nil
	}
	return in
}

// simulateRun plays a strategy's signals and returns the engine used. The hourly simulator is the
// default: it judges every fill and exit on the hour it can happen. A strategy that uses a setting
// it does not model (margin, ATR stops, dividends, other position sizing) runs on the daily
// simulator instead and says so, in the log and in the stored run.
func simulateRun(
	id string,
	cfg strategy.StrategyConfig,
	signals []models.Signal,
	daily map[string][]models.Bar,
	dates []string,
	capital float64,
	marketDB string,
	tr *totalReturnInfo,
) (legacyExtras, string, models.PerformanceReport, []models.Trade, []models.DailyEquityPoint) {
	var why error
	if tr != nil {
		why = hoursim.UnsupportedError{Feature: "dividends on total-return prices"}
	} else {
		out, err := hoursim.Run(hoursim.Input{
			Signals: signals, Daily: daily, Hourly: hourlyBars(marketDB, signals),
			Dates: dates, Config: cfg, Capital: capital,
		})
		if err == nil {
			if n := len(out.DailySessions); n > 0 {
				log.Printf("[%s] hourly simulator: no hourly bars for %v on some sessions, played as daily bars", id, hoursim.SortedSymbols(out.DailySessions))
			}
			return legacyExtras{}, storage.EngineHourly, out.Report, out.Trades, out.Curve
		}
		var u hoursim.UnsupportedError
		if !errors.As(err, &u) {
			log.Printf("[%s] hourly simulator failed: %v", id, err)
		}
		why = err
	}
	log.Printf("[%s] daily simulator (legacy): %v", id, why)

	sim := simulator.NewPortfolioSimulator(cfg, capital)
	if tr != nil {
		sim.Dividends = tr.divs
	}
	if cfg.EntryLimitPct > 0 {
		// Limit entries are judged on hourly bars where the market DB has them.
		syms := signalSymbols(signals)
		sort.Strings(syms)
		var err error
		if sim.Intraday, err = LoadFillBars(marketDB, syms); err != nil {
			log.Printf("Warning: no intraday bars for %s: %v", id, err)
			sim.Intraday = nil
		}
	}
	report, trades, curve := sim.Run(signals, daily, dates)
	return legacyExtras{marginInterest: sim.TotalMarginInterest, dividendCash: sim.DividendCash}, storage.EngineDaily, report, trades, curve
}

// stackRun is a finished shared-account run.
type stackRun struct {
	combined    models.PerformanceReport
	perStrategy map[string]models.PerformanceReport
	trades      []models.Trade
	curve       []models.DailyEquityPoint
	preempted   int
	parked      simulator.DefaultAssetResult
	engine      string
}

// simulateStack plays the members' signals in one shared account. It uses the hourly simulator
// unless a member uses a setting that simulator does not model: those run on the daily simulator,
// and the log says why. A default asset parks leftover cash (sim.Park).
func simulateStack(
	entries []simulator.StrategyPriorityEntry,
	signals []models.Signal,
	daily map[string][]models.Bar,
	dates []string,
	capital float64,
	marketDB string,
	defaultAsset string,
	benchmark map[string]models.Bar,
) stackRun {
	var why error
	byID := map[string][]models.Signal{}
	for _, s := range signals {
		byID[s.StrategyID] = append(byID[s.StrategyID], s)
	}
	members := make([]hoursim.Member, 0, len(entries))
	for _, e := range entries {
		members = append(members, hoursim.Member{ID: e.Strategy.ID(), Priority: e.Priority, Config: e.Config, Signals: byID[e.Strategy.ID()]})
	}
	hourlySyms := signals
	if defaultAsset != "" {
		hourlySyms = append(append([]models.Signal{}, signals...), models.Signal{Symbol: defaultAsset})
	}
	out, err := hoursim.RunStack(hoursim.StackInput{
		Members: members, Daily: daily, Hourly: hourlyBars(marketDB, hourlySyms),
		Dates: dates, Capital: capital, Benchmark: benchmark,
		DefaultAsset: defaultAsset, Dividends: options.DividendsFromAdjClose(daily[defaultAsset]),
	})
	if err == nil {
		if len(out.DailySessions) > 0 {
			log.Printf("hourly simulator: no hourly bars for %v on some sessions, played as daily bars", hoursim.SortedSymbols(out.DailySessions))
		}
		return stackRun{combined: out.Report, perStrategy: out.PerStrategy, trades: out.Trades, curve: out.Curve,
			preempted: out.Preempted, engine: storage.EngineHourly,
			parked: simulator.DefaultAssetResult{Symbol: out.Park.Symbol, AvgWeight: out.Park.AvgWeight, Dividends: out.Park.Dividends, DaysUnparked: out.Park.DaysUnparked}}
	}
	why = err
	log.Printf("stack: daily simulator (legacy): %v", why)

	sim := simulator.NewSharedAccountSimulator(entries, capital)
	if defaultAsset != "" {
		sim.SetDefaultAsset(defaultAsset, options.DividendsFromAdjClose(daily[defaultAsset]))
	}
	if benchmark != nil {
		sim.SetBenchmarkBars(benchmark)
	}
	combined, per, trades, curve := sim.Run(signals, daily, dates)
	return stackRun{combined: combined, perStrategy: per, trades: trades, curve: curve,
		preempted: sim.PreemptedTradeCount, parked: sim.DefaultAssetResult(), engine: storage.EngineDaily}
}

// splitSuspect says why a run should not be ranked: a symbol it traded has a close-to-close jump
// beyond sessions.DefaultSplitJumpRatio inside the dates it simulated, almost always an unadjusted
// split. It reads the market database and returns "" when nothing is wrong, when there is no
// database to read, or when the check itself fails (a check must never stop a run).
func splitSuspect(marketDB string, symbols, dates []string) string {
	if marketDB == "" || len(symbols) == 0 || len(dates) == 0 {
		return ""
	}
	if _, err := os.Stat(marketDB); err != nil {
		return ""
	}
	db, err := storage.OpenSQLiteReadOnly(marketDB)
	if err != nil {
		return ""
	}
	defer db.Close()
	jumps, err := sessions.SplitJumps(db, symbols, 0, dates[0], dates[len(dates)-1])
	if err != nil {
		log.Printf("Warning: split check on %s: %v", marketDB, err)
		return ""
	}
	if len(jumps) == 0 {
		return ""
	}
	var parts []string
	for i, j := range jumps {
		if i == 3 {
			parts = append(parts, fmt.Sprintf("and %d more", len(jumps)-3))
			break
		}
		parts = append(parts, j.String())
	}
	return "unadjusted split? " + strings.Join(parts, ", ")
}

// RowProblem says why a strategy cannot trade at all, or "" when it can. Such a row would run and
// report zero trades, which reads as "never signalled" when the truth is "never could".
//
// An entry limit at or below the stop (as fractions of the previous close) puts the stop at or
// above the fill price, so the position would exit on the next bar at a price it never traded and
// the hourly simulator refuses the fill.
func RowProblem(cfg strategy.StrategyConfig) string {
	if cfg.EntryLimitPct > 0 && cfg.StopLossPct > 0 && cfg.StopLossPct < 1 && cfg.EntryLimitPct <= cfg.StopLossPct+1e-9 {
		return fmt.Sprintf("entry limit %.1f%% of the previous close is at or below the stop (%.1f%%): the stop would sit at or above the fill, so the order can never fill",
			100*cfg.EntryLimitPct, 100*cfg.StopLossPct)
	}
	return ""
}
