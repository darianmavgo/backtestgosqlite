package cliutils

import (
	"os"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
)

// GetDefaultMarketDB resolves the default market history database path.
func GetDefaultMarketDB() string {
	return appenv.MarketDB()
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
