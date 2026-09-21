// cmd/livescan — thin wrapper around runner.RunSignalScan: the same
// GenerateSignals path backtest uses, with the time window fixed to the
// market DB tip (last completed session). ENTER means a buy signal as of
// that tip for the next equity session (today if after tip close, else the
// upcoming session).
//
// Flags mirror cmd/backtest where they mean the same thing; -bars is
// livescan-specific. Market history is refreshed for the live window by
// default; refresh failure aborts — never scan a stale tip.
//
// Usage:
//
//	go run ./cmd/livescan -strategy bb-capitulation
//	go run ./cmd/livescan -strategy gld-decline,sig-voo-buy-tecl
//	go run ./cmd/livescan all
//	go run ./cmd/livescan -list
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/cliutils"
	"github.com/darianmavgo/backtestgosqlite/pkg/runner"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/olekukonko/tablewriter"
)

func main() {
	defaultMarketDb := cliutils.GetDefaultMarketDB()

	targetDb := flag.String("db", defaultMarketDb, "Path to source SQLite DB containing historical market bars")
	tableName := flag.String("table", "backtest_start", "Table name containing historical bars")
	strategyFlag := flag.String("strategy", "", "Strategy ID, comma-separated IDs, or \"all\"")
	symbolFilter := flag.String("symbol", "", "Optional comma-separated symbol filter")
	outDir := flag.String("out-dir", appenv.Reports(), "Directory for livescan.db")
	autoDownload := flag.Bool("auto-download", true, "Refresh market history for the live window before scanning (default on; failure aborts)")
	downloadYears := flag.Int("download-years", 0, "Years of history to download (0 = derive from -bars / strategy min)")
	concurrency := flag.Int("concurrency", runtime.NumCPU(), "Parallel strategy workers")
	barsLimit := flag.Int("bars", 0, "Recent bars per symbol; 0 = strategy MinHistoryBars")
	listStrategies := flag.Bool("list", false, "List registered strategies and exit")
	jsonOut := flag.Bool("json", false, "Emit machine-readable SignalScanResult JSON to stdout (for trade_orchestrator)")
	flag.Parse()

	strategy.AutoRegisterSQLStrategies(appenv.Folder(), *targetDb)

	if *listStrategies {
		fmt.Println("Available Strategies:")
		for _, s := range strategy.List() {
			fmt.Printf("  %-28s %s\n", s.ID(), s.Name())
		}
		return
	}

	stratArg := *strategyFlag
	if stratArg == "" && flag.NArg() > 0 {
		stratArg = flag.Arg(0)
	}
	selected, err := runner.ResolveStrategies(stratArg, "bb-capitulation")
	if err != nil {
		log.Fatalf("%v. Run with -list to view available strategies.", err)
	}

	fmt.Printf("\n📡 livescan = backtest signal phase (live window)\n")
	fmt.Printf("   strategies=%d db=%s table=%s\n", len(selected), *targetDb, *tableName)

	res, err := runner.RunSignalScan(runner.SignalScanOptions{
		MarketDB:      *targetDb,
		Table:         *tableName,
		Strategies:    selected,
		SymbolFilter:  *symbolFilter,
		BarsLimit:     *barsLimit,
		AutoDownload:  *autoDownload,
		DownloadYears: *downloadYears,
		Concurrency:   *concurrency,
		OutDir:        *outDir,
	})
	if err != nil {
		log.Fatalf("livescan: %v", err)
	}

	fmt.Printf("\n📅 Tip / as-of        : %s\n", res.AsOf)
	fmt.Printf("🎯 Next session (entry): %s\n", res.NextSession)
	fmt.Printf("📊 Symbols loaded     : %d\n\n", res.SymbolsLoaded)

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			log.Fatalf("json encode: %v", err)
		}
		return
	}

	printScanTable(res.Rows)
	fmt.Printf("\n💾 Wrote %s\n", res.OutDBPath)
	if n := len(res.Signals); n > 0 {
		fmt.Printf("📌 %d priced ENTER signal(s) in livescan_signals\n", n)
	}
}

func printScanTable(results []runner.SignalScanRow) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Strategy", "Status", "Symbols", "Signal Date"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	entering := 0
	for _, r := range results {
		status := "⚪ NO_SIGNAL"
		if r.Status == "ENTER" {
			status = "🟢 ENTER"
			entering++
		}
		table.Append([]string{r.StrategyID, status, truncateSymbols(r.Symbols, 8), r.SignalDate})
	}
	table.Render()
	fmt.Printf("\n%d/%d strategies have an entry signal as of the tip bar (for next session).\n", entering, len(results))
}

func truncateSymbols(symbols string, maxShown int) string {
	if symbols == "" {
		return ""
	}
	parts := strings.Split(symbols, ", ")
	if len(parts) <= maxShown {
		return symbols
	}
	return fmt.Sprintf("%s, ... (+%d more, see livescan.db)", strings.Join(parts[:maxShown], ", "), len(parts)-maxShown)
}
