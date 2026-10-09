package simulator

import "github.com/darianmavgo/backtestgosqlite/pkg/models"

// indexBarsByDate indexes bars by symbol and date for O(1) price checks, only for
// the symbols a simulation can look up: the symbols of its signals (positions open
// only on those) and extra, such as the cash park. The bar map of a bulk run holds
// every symbol of the batch, hundreds of them, and indexing all of those for each
// strategy cost more than the simulation. A symbol left out reads as having no bars.
func indexBarsByDate(barsBySymbol map[string][]models.Bar, signals []models.Signal, extra ...string) map[string]map[string]models.Bar {
	out := make(map[string]map[string]models.Bar)
	add := func(sym string) {
		if _, done := out[sym]; done {
			return
		}
		bars := barsBySymbol[sym]
		byDate := make(map[string]models.Bar, len(bars))
		for _, b := range bars {
			byDate[b.Date] = b
		}
		out[sym] = byDate
	}
	for _, sig := range signals {
		add(sig.Symbol)
	}
	for _, sym := range extra {
		if sym != "" {
			add(sym)
		}
	}
	return out
}
