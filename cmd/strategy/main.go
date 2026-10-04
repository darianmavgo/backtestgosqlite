package main

import (
	"fmt"
	"os"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/stratreg"
	"github.com/olekukonko/tablewriter"
)

func main() {
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
