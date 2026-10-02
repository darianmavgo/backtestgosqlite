package runner

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

// This file is the single shared implementation of "has this strategy already
// been backtested, and is that result trustworthy?" — used by every command that
// runs strategies in bulk (cmd/backtest -strategy all, cmd/scoreboard) so that
// skip-duplicate-work logic lives in one place instead of being reinvented (or
// forgotten) per command.

// CompiledResult is the newest finished run of one strategy in results.db.
type CompiledResult struct {
	StrategyID string
	Report     models.PerformanceReport
	DbPath     string
	Increment  int // run_id
	ComputedAt time.Time
}

// ScanAndValidate reads the newest finished run of every strategy from
// results.db in outDir. This is the one shared "what's already been
// backtested, and can I trust it?" pass every bulk-strategy command runs before
// doing any work. A run is written in one transaction, so a crashed run leaves
// nothing behind to validate; the file itself is integrity-checked once. The
// remaining return values keep the old signature: files seen (0 or 1),
// strategies found, always 0 fallbacks, and 1 when the file failed its check.
func ScanAndValidate(outDir string, concurrency int) (byStrategy map[string]CompiledResult, totalFiles, totalGroups, usedFallback, allCompromised int) {
	byStrategy = make(map[string]CompiledResult)
	path := filepath.Join(outDir, storage.ResultsFile)
	if _, err := os.Stat(path); err != nil {
		return byStrategy, 0, 0, 0, 0
	}
	results, err := storage.SharedResults(outDir)
	if err != nil {
		log.Printf("⚠️  cannot open %s: %v", path, err)
		return byStrategy, 1, 0, 0, 1
	}
	if err := results.IntegrityOK(); err != nil {
		log.Printf("⚠️  %s is compromised (%v)", path, err)
		return byStrategy, 1, 0, 0, 1
	}
	runs, err := results.LatestRuns()
	if err != nil {
		log.Printf("⚠️  cannot read runs from %s: %v", path, err)
		return byStrategy, 1, 0, 0, 1
	}
	for _, r := range runs {
		byStrategy[r.StrategyID] = CompiledResult{
			StrategyID: r.StrategyID, Report: r.Report, DbPath: path,
			Increment: int(r.RunID), ComputedAt: r.CreatedAt,
		}
	}
	fmt.Printf("   Found %d strategies with a finished run in %s.\n\n", len(byStrategy), path)
	return byStrategy, 1, len(byStrategy), 0, 0
}

// MissingStrategies returns the IDs of every currently-registered strategy that
// has no usable (non-compromised) entry in byStrategy — i.e. it has genuinely
// never been backtested, or every run it has was corrupted/incomplete.
func MissingStrategies(byStrategy map[string]CompiledResult) []string {
	var missing []string
	for _, s := range strategy.ListAll() {
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
