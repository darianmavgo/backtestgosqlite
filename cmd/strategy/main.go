package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/stratreg"
	"github.com/olekukonko/tablewriter"
)

// export writes a reference database holding only the rows of the named strategies
// or stacks, for a program that cannot carry the whole file.
func export() {
	src := flag.String("src", appenv.RefDB(), "full reference database to copy rows from")
	out := flag.String("out", "", "reference database to write")
	ids := flag.String("ids", "", "strategy ids and a+b+c stacks, comma-separated")
	modelsOut := flag.String("models-out", "", "directory to also write markov_models.db and tree_models.db into, holding only the trained models of the exported rows' signal symbols")
	markovSrc := flag.String("markov-src", appenv.MarkovDB(), "full Markov model database to copy models from")
	treeSrc := flag.String("tree-src", appenv.TreeDB(), "full tree model database to copy models from")
	flag.Parse()
	if *out == "" || *ids == "" {
		log.Fatal("export needs -ids and -out")
	}
	var members []string
	seen := map[string]bool{}
	for _, entry := range strings.Split(*ids, ",") {
		for _, id := range strategy.ParseStack(strings.TrimSpace(entry)) {
			if !seen[id] {
				seen[id] = true
				members = append(members, id)
			}
		}
	}
	found, err := refdb.Export(*src, *out, members)
	for table, got := range found {
		fmt.Printf("  %-16s %v\n", table, got)
	}
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s with %d strategies\n", *out, len(members))
	if *modelsOut == "" {
		return
	}
	for family, src := range map[string]string{"markov": *markovSrc, "tree": *treeSrc} {
		dst := filepath.Join(*modelsOut, family+"_models.db")
		syms, err := refdb.ExportModels(family, src, *out, dst)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("wrote %s with %d %s models\n", dst, len(syms), family)
	}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "export" {
		os.Args = append(os.Args[:1], os.Args[2:]...)
		export()
		return
	}
	// Initialize databases and register all strategies.
	strategy.AutoRegisterSQLStrategies(appenv.Folder(), appenv.MarketDB())
	stratreg.RegisterFamilies()

	strategies := strategy.List()

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "Name", "Description"})
	table.SetBorder(true)
	table.SetRowLine(true)

	for _, s := range strategies {
		table.Append([]string{s.ID(), s.Name(), s.Description()})
	}

	fmt.Printf("\nFound %d Strategies defined by their own sql/strategies pipeline:\n", len(strategies))
	table.Render()
	fmt.Println("\nRow-backed families (not listed one by one):")
	strategy.PrintFamilyCounts(os.Stdout)
	fmt.Println()
}
