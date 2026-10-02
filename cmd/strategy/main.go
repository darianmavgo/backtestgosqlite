package main

import (
	"fmt"
	"os"
	"reflect"
	"strings"

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
	table.SetHeader([]string{"ID", "Name", "Definition Type / Source"})
	table.SetBorder(true)
	table.SetRowLine(true)

	for _, s := range strategies {
		t := reflect.TypeOf(s)
		var source string

		// If it's a pointer, get the underlying element
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}

		pkgPath := t.PkgPath()
		typeName := t.Name()

		if strings.Contains(pkgPath, "streak_strategy") {
		} else if strings.Contains(pkgPath, "markov_strategy") {
		} else if strings.Contains(pkgPath, "hold_strategy") {
		} else if strings.Contains(pkgPath, "hold_bail_strategy") {
		} else if typeName == "SQLPipelineStrategy" {
			// Try to extract the directory if possible, but fallback to general text
			source = "sql/strategies/... (SQL Pipeline) -> pkg/strategy"
		} else if strings.Contains(pkgPath, "pkg/strategy") {
			source = "pkg/strategy/*.go (Hardcoded Go)"
		} else {
			source = pkgPath + "." + typeName
		}

		table.Append([]string{
			s.ID(),
			s.Name(),
			source,
		})
	}

	fmt.Printf("\nFound %d Registered Strategies:\n", len(strategies))
	table.Render()
	fmt.Println("\nRow-backed families (not listed one by one):")
	strategy.PrintFamilyCounts(os.Stdout)
	fmt.Println()
}
