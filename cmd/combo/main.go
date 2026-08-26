// cmd/combo — Thin CLI for running dual-engine Long/Short all-weather backtests.
//
// Example:
//
//	go run cmd/combo/main.go -long TECL -short SPXU -alloc 0.65 -html reports/combo_tecl_spxu.html
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
	signalSymbol := flag.String("signal", "VOO", "Signal generation symbol")
	longSymbol := flag.String("long", "TECL", "Asset to buy on market dips")
	shortSymbol := flag.String("short", "SPXU", "Asset to buy on bear market rallies")

	// Long engine flags
	longDays := flag.Int("long-days", 3, "Consecutive down days for long trigger")
	longHold := flag.Int("long-hold", 8, "Max hold days for long trades")
	longTP := flag.Float64("long-tp", 0.05, "Long take profit target (+5%)")
	longSL := flag.Float64("long-sl", 0.0, "Long stop loss (0 = none, 0.20 = -20% disaster guard)")

	// Short engine flags
	shortDays := flag.Int("short-days", 3, "Consecutive up days for short trigger")
	shortHold := flag.Int("short-hold", 2, "Max hold days for short trades")
	shortTP := flag.Float64("short-tp", 0.06, "Short take profit target (+6%)")
	shortSL := flag.Float64("short-sl", 0.05, "Short stop loss (-5%)")
	shortRegime := flag.String("short-regime", "VOO < SMA200", "Regime filter for short engine")

	// Sizing & Cash
	allocPct := flag.Float64("alloc", 0.65, "Dynamic capital allocation (65%)")
	capital := flag.Float64("capital", 100000.0, "Starting cash ($)")
	cashYield := flag.Float64("yield", 0.045, "Annual cash yield on idle reserves (4.5%)")
	htmlOutput := flag.String("html", "reports/combo_all_weather.html", "Export interactive HTML chart path")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// 1. Fetch Bars
	vooBars, err := storage.FetchDipBars(db, *signalSymbol)
	if err != nil {
		log.Fatalf("Failed to fetch %s bars: %v", *signalSymbol, err)
	}
	longBars, err := storage.FetchDipBars(db, *longSymbol)
	if err != nil {
		log.Fatalf("Failed to fetch %s bars: %v", *longSymbol, err)
	}
	shortBars, err := storage.FetchDipBars(db, *shortSymbol)
	if err != nil {
		log.Fatalf("Failed to fetch %s bars: %v", *shortSymbol, err)
	}
	sigBars, err := storage.FetchSignalBars(db, *signalSymbol)
	if err != nil {
		log.Fatalf("Failed to fetch signal bars: %v", err)
	}

	// 2. Generate Signals
	longSignals := signals.DetectConsecutiveDrops(vooBars, *longDays)
	shortSignals := signals.DetectConsecutiveRallies(vooBars, *shortDays)
	if *shortRegime != "" {
		shortSignals = signals.FilterByRegime(shortSignals, sigBars, *shortRegime)
	}

	// 3. Build Combo Config
	comboCfg := dipsim.ComboConfig{
		LongConfig: dipsim.DipSimConfig{
			Label:           fmt.Sprintf("%s Long (%dd Hold / +%.0f%% TP)", *longSymbol, *longHold, *longTP*100),
			SignalSymbol:    *signalSymbol,
			SignalDays:      *longDays,
			SignalDirection: "drop",
			TradeSymbol:     *longSymbol,
			AllocationPct:   *allocPct,
			TakeProfitPct:   *longTP,
			StopLossPct:     *longSL,
			MaxHoldDays:     *longHold,
			InitialCapital:  *capital,
			CashYieldAnnual: *cashYield,
		},
		ShortConfig: dipsim.DipSimConfig{
			Label:           fmt.Sprintf("%s Short (%dd Hold / +%.0f%% TP / -%.0f%% SL)", *shortSymbol, *shortHold, *shortTP*100, *shortSL*100),
			SignalSymbol:    *signalSymbol,
			SignalDays:      *shortDays,
			SignalDirection: "rally",
			TradeSymbol:     *shortSymbol,
			AllocationPct:   *allocPct,
			TakeProfitPct:   *shortTP,
			StopLossPct:     *shortSL,
			MaxHoldDays:     *shortHold,
			RegimeFilter:    *shortRegime,
			InitialCapital:  *capital,
			CashYieldAnnual: *cashYield,
		},
		InitialCapital:  *capital,
		CashYieldAnnual: *cashYield,
	}

	// 4. Run Simulation
	result := dipsim.RunCombo(comboCfg, longSignals, shortSignals, vooBars, longBars, shortBars)

	// Calculate VOO stats
	vooStart := vooBars[0].Close
	vooEnd := vooBars[len(vooBars)-1].Close
	vooFinal := (*capital / vooStart) * vooEnd
	vooProfit := vooFinal - *capital
	vooMaxDD := 25.41 // 5-year maximum drawdown of VOO

	// 5. Print Output Table
	dipsim.PrintComboResult(result, vooFinal, vooProfit, vooMaxDD)

	// 6. Generate HTML Report
	if *htmlOutput != "" {
		reportView := charting.FromComboResult(result, vooBars)
		if err := charting.GenerateHTML(*htmlOutput, reportView); err != nil {
			log.Printf("Warning: Failed to generate HTML report: %v", err)
		} else {
			fmt.Printf("✨ Interactive All-Weather Combo Chart saved to: %s\n\n", *htmlOutput)
		}
	}
}
