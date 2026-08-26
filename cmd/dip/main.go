// cmd/dip — Thin CLI for single-strategy dip-buy backtesting.
//
// Usage examples:
//
//	# TECL 3-day dip, 65% alloc, +5% TP, -20% disaster guard, 8-day hold:
//	go run cmd/dip/main.go -signal VOO -trade TECL -days 3 -hold 8 -tp 0.05 -sl 0.20 -alloc 0.65
//
//	# SPXU bear fade: 3-day rally, bear regime, +6% TP, -5% SL, 2-day hold:
//	go run cmd/dip/main.go -signal VOO -trade SPXU -days 3 -dir rally -hold 2 -tp 0.06 -sl 0.05 -regime "VOO < SMA200" -alloc 0.65
//
//	# Use SQL signal file:
//	go run cmd/dip/main.go -signal VOO -trade TECL -sqlfile sql/signals/consecutive_drop.sql -hold 8 -tp 0.05 -alloc 0.65
package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/darianmavgo/backtestgosqlite/internal/charting"
	"github.com/darianmavgo/backtestgosqlite/internal/dipsim"
	"github.com/darianmavgo/backtestgosqlite/internal/signals"
	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	signalSymbol := flag.String("signal", "VOO", "Symbol that generates entry signals")
	tradeSymbol := flag.String("trade", "TECL", "Symbol to buy on signal")
	signalDays := flag.Int("days", 3, "Consecutive days in streak")
	signalDir := flag.String("dir", "drop", "Signal direction: 'drop' or 'rally'")
	sqlFile := flag.String("sqlfile", "", "Optional path to SQL signal file (e.g. sql/signals/consecutive_drop.sql)")
	holdDays := flag.Int("hold", 8, "Max holding days (time barrier)")
	tpPct := flag.Float64("tp", 0.05, "Take-profit target (0.05 = +5%, 0 = none)")
	slPct := flag.Float64("sl", 0.0, "Stop-loss guard (0.20 = -20%, 0 = none)")
	allocPct := flag.Float64("alloc", 0.65, "Allocation fraction (0.65 = 65%)")
	capital := flag.Float64("capital", 100000.0, "Starting cash ($)")
	cashYield := flag.Float64("yield", 0.045, "Annual cash yield on idle reserves (0.045 = 4.5%)")
	regime := flag.String("regime", "", "Regime filter: 'VOO < SMA200', 'VOO < SMA50', 'VOO >= SMA200', or empty for all")
	htmlOutput := flag.String("html", "", "Optional path to export interactive HTML chart (e.g. reports/tecl_dip.html)")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// 1. Fetch bars
	vooBars, err := storage.FetchDipBars(db, *signalSymbol)
	if err != nil {
		log.Fatalf("Failed to fetch %s bars: %v", *signalSymbol, err)
	}
	tradeBars, err := storage.FetchDipBars(db, *tradeSymbol)
	if err != nil {
		log.Fatalf("Failed to fetch %s bars: %v", *tradeSymbol, err)
	}
	sigBars, err := storage.FetchSignalBars(db, *signalSymbol)
	if err != nil {
		log.Fatalf("Failed to fetch signal bars: %v", err)
	}

	// 2. Detect signal dates
	var signalDates map[string]bool

	if *sqlFile != "" {
		// SQL-driven signals
		params := map[string]string{"signal_symbol": *signalSymbol}
		result, err := signals.LoadFromSQL(db, *sqlFile, params)
		if err != nil {
			log.Fatalf("Failed to load SQL signal file: %v", err)
		}
		signalDates = result.Dates
		fmt.Printf("📡 Loaded %d signal dates from SQL: %s\n", len(signalDates), *sqlFile)
	} else {
		// Go-based streak detection
		if *signalDir == "rally" {
			signalDates = signals.DetectConsecutiveRallies(vooBars, *signalDays)
		} else {
			signalDates = signals.DetectConsecutiveDrops(vooBars, *signalDays)
		}
		fmt.Printf("📡 Detected %d signal dates (%s %d-day %s streaks)\n", len(signalDates), *signalSymbol, *signalDays, *signalDir)
	}

	// 3. Apply regime filter
	if *regime != "" {
		before := len(signalDates)
		signalDates = signals.FilterByRegime(signalDates, sigBars, *regime)
		fmt.Printf("🔍 Regime filter '%s': %d → %d signals\n", *regime, before, len(signalDates))
	}

	// 4. Build config
	cfg := dipsim.DipSimConfig{
		Label:           fmt.Sprintf("%s %d-Day %s → %s (%d-Day Hold / +%.0f%% TP / -%.0f%% SL / %.0f%% Alloc)", *signalSymbol, *signalDays, *signalDir, *tradeSymbol, *holdDays, *tpPct*100, *slPct*100, *allocPct*100),
		SignalSymbol:    *signalSymbol,
		SignalDays:      *signalDays,
		SignalDirection: *signalDir,
		SignalSQLFile:   *sqlFile,
		TradeSymbol:     *tradeSymbol,
		AllocationPct:   *allocPct,
		TakeProfitPct:   *tpPct,
		StopLossPct:     *slPct,
		MaxHoldDays:     *holdDays,
		RegimeFilter:    *regime,
		InitialCapital:  *capital,
		CashYieldAnnual: *cashYield,
	}

	// 5. Run simulation
	result := dipsim.Run(cfg, signalDates, tradeBars)

	// 6. Print results
	dipsim.PrintResult(result)

	// 7. Generate HTML Report if requested
	if *htmlOutput != "" {
		reportView := charting.FromResult(result, vooBars, *capital)
		if err := charting.GenerateHTML(*htmlOutput, reportView); err != nil {
			log.Printf("Warning: Failed to generate HTML report: %v", err)
		} else {
			fmt.Printf("\n✨ Interactive Chart saved to: %s\n\n", *htmlOutput)
		}
	}
}
