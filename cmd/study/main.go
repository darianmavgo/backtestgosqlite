package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/cliutils"
	"github.com/darianmavgo/backtestgosqlite/pkg/study"
)

func main() {
	defaultMarketDb := cliutils.GetDefaultMarketDB()

	targetDb := flag.String("db", defaultMarketDb, "Path to source SQLite DB containing historical market bars")
	studyID := flag.String("study", "", "Study ID to run (e.g. march_april_voo_gld_uten)")
	outDir := flag.String("out-dir", "reports", "Directory to write study results (SQLite database)")
	listFlag := flag.Bool("list", false, "List all registered studies")
	flag.Parse()

	if *listFlag {
		fmt.Println("Available Studies:")
		studies := study.List()
		for _, s := range studies {
			fmt.Printf("  - %s: %s\n    %s\n", s.ID(), s.Name(), s.Description())
		}
		return
	}

	if *studyID == "" {
		log.Fatalf("No study specified. Run with -list to see available studies or provide -study <id>")
	}

	s, exists := study.Get(strings.TrimSpace(*studyID))
	if !exists {
		log.Fatalf("Study '%s' not found.", *studyID)
	}

	resultsDBPath := filepath.Join(*outDir, fmt.Sprintf("%s.db", s.ID()))

	s.SetDatabases(*targetDb, resultsDBPath)

	if err := s.Run(); err != nil {
		log.Fatalf("Study execution failed: %v", err)
	}

	fmt.Printf("\n✨ Study successfully saved to %s\n", resultsDBPath)
}
