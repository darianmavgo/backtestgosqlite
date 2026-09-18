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
