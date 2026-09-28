package study

import (
	"flag"
	"fmt"
	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"log"
	"path/filepath"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/cliutils"
)

// Config holds the settings of a study run.
type Config struct {
	DB        string // source market DB
	Study     string // study ID
	OutDir    string // directory for the results DB
	ClusterDB string // regime-label DB for studies that implement ClusterDBSetter
}

// DefaultConfig returns the CLI defaults.
func DefaultConfig() Config {
	return Config{DB: cliutils.GetDefaultMarketDB(), OutDir: appenv.Reports()}
}

// Run executes one registered study and returns the path of its results DB.
func Run(cfg Config) (string, error) {
	if cfg.Study == "" {
		return "", fmt.Errorf("no study specified. Run with -list to see available studies or provide -study <id>")
	}
	s, exists := Get(strings.TrimSpace(cfg.Study))
	if !exists {
		return "", fmt.Errorf("study '%s' not found", cfg.Study)
	}
	resultsDBPath := filepath.Join(cfg.OutDir, fmt.Sprintf("%s.db", s.ID()))
	s.SetDatabases(cfg.DB, resultsDBPath)
	if setter, ok := s.(ClusterDBSetter); ok {
		clusterDB := cfg.ClusterDB
		if clusterDB == "" {
			clusterDB = filepath.Join(cfg.OutDir, "market_context_20d.db")
		}
		setter.SetClusterDB(clusterDB)
	}
	if err := s.Run(); err != nil {
		return "", fmt.Errorf("study execution failed: %w", err)
	}
	return resultsDBPath, nil
}

// Main is the CLI entry point.
func Main() {
	cfg := DefaultConfig()
	flag.StringVar(&cfg.DB, "db", cfg.DB, "Path to source SQLite DB containing historical market bars")
	flag.StringVar(&cfg.Study, "study", "", "Study ID to run (e.g. march_april_voo_gld_uten)")
	flag.StringVar(&cfg.OutDir, "out-dir", cfg.OutDir, "Directory to write study results (SQLite database)")
	flag.StringVar(&cfg.ClusterDB, "cluster-db", "", "Regime-label database for studies that use one (default: <out-dir>/market_context_20d.db)")
	listFlag := flag.Bool("list", false, "List all registered studies")
	flag.Parse()

	if *listFlag {
		fmt.Println("Available Studies:")
		studies := List()
		for _, s := range studies {
			fmt.Printf("  - %s: %s\n    %s\n", s.ID(), s.Name(), s.Description())
		}
		return
	}

	resultsDBPath, err := Run(cfg)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n✨ Study successfully saved to %s\n", resultsDBPath)
}
