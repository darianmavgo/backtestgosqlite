package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/runner"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

// runStackEvalCommand implements `backtest stack-eval`: rank existing strategies
// as idle-cash overlays on one primary, all sharing a single cash ledger.
// No new pkg/strategy types are created — stacking lives in the engine/runner.
func runStackEvalCommand(
	primaryID string,
	explicitSecondaries []string,
	includeUniverse, includeDT bool,
	dtTop, stackDepth, concurrency int,
	targetDb, tableName, outDir string,
	capital float64,
	symbolFilter string,
	autoDownload bool,
	downloadYears int,
	persistBest bool,
) {
	if primaryID == "" {
		log.Fatal("stack-eval requires a primary strategy (positional or -primary). Example:\n  ./bin/backtest stack-eval -primary sig-voo-buy-tecl")
	}
	primary, ok := strategy.Get(primaryID)
	if !ok {
		log.Fatalf("Primary strategy %q not found. Run ./bin/backtest -list", primaryID)
	}

	cands := runner.OverlayCandidates(primary, runner.OverlayCandidateOptions{
		ExplicitIDs:     explicitSecondaries,
		IncludeUniverse: includeUniverse,
		IncludeDT:       includeDT,
		DTTop:           dtTop,
	})
	if len(cands) == 0 {
		log.Fatalf("No overlay candidates for %s. Pass -secondary id1,id2 or -include-dt / -include-universe.", primary.ID())
	}

	all := append([]strategy.Strategy{primary}, cands...)
	if err := runner.DetectAndDownloadMissingData(targetDb, tableName, all, symbolFilter, autoDownload, downloadYears); err != nil {
		log.Fatalf("Market data resolution error: %v", err)
	}

	db, err := storage.OpenSQLite(targetDb)
	if err != nil {
		log.Fatalf("Failed to open source DB %s: %v", targetDb, err)
	}
	defer db.Close()

	reqSymbols := append(runner.RequiredSymbolsFor(all, symbolFilter), "SPY")
	// Universe overlays declare no RequiredSymbols — load the full table.
	var fetchSymbols []string
	if includeUniverse && len(explicitSecondaries) == 0 {
		fetchSymbols = nil
		fmt.Printf("\nLoading ALL bars from '%s' (universe overlays enabled)...\n", tableName)
	} else {
		fetchSymbols = reqSymbols
		fmt.Printf("\nLoading bars for %v from '%s' for stack-eval of %s (capital $%.0f)...\n",
			reqSymbols, tableName, primary.ID(), capital)
	}
	barsBySymbol, sortedDates, err := storage.FetchBars(db, tableName, fetchSymbols, "", "")
	if err != nil {
		log.Fatalf("Error loading historical bars: %v", err)
	}

	fmt.Printf("Evaluating %d overlay candidates on idle cash of %s...\n", len(cands), primary.ID())
	for _, c := range cands {
		fmt.Printf("  • %s\n", c.ID())
	}

	result := runner.ExecuteStackEval(runner.StackEvalOptions{
		Primary:      primary,
		Candidates:   cands,
		BarsBySymbol: barsBySymbol,
		SortedDates:  sortedDates,
		Capital:      capital,
		OutDir:       outDir,
		MarketDBPath: targetDb,
		Concurrency:  concurrency,
		StackDepth:   stackDepth,
		PersistBest:  persistBest,
	})
	runner.PrintStackEvalTearSheet(result)
}

func parseSecondaryList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, tok := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' }) {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			out = append(out, tok)
		}
	}
	return out
}
