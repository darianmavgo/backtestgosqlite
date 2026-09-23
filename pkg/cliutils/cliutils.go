package cliutils

import (
	"os"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
)

// GetDefaultMarketDB resolves the default market history database path.
// It checks for data/market_history.db, and falls back to data/leveraged_backtest.db if not found.
func GetDefaultMarketDB() string {
	defaultMarketDb := appenv.MarketDB()
	if _, err := os.Stat(defaultMarketDb); os.IsNotExist(err) {
		defaultMarketDb = appenv.DataFile("leveraged_backtest.db")
	}
	return defaultMarketDb
}

// PopSubcommand checks whether os.Args[1] is one of the given subcommand
// names (aliases map name -> canonical name). If so it removes it from
// os.Args, so flag.Parse still works, and returns the canonical name;
// otherwise it returns "".
func PopSubcommand(aliases map[string]string) string {
	if len(os.Args) > 1 {
		if canon, ok := aliases[os.Args[1]]; ok {
			os.Args = append(os.Args[:1], os.Args[2:]...)
			return canon
		}
	}
	return ""
}
