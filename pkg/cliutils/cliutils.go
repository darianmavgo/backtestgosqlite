package cliutils

import "os"

// GetDefaultMarketDB resolves the default market history database path.
// It checks for data/market_history.db, and falls back to data/leveraged_backtest.db if not found.
func GetDefaultMarketDB() string {
	defaultMarketDb := "data/market_history.db"
	if _, err := os.Stat(defaultMarketDb); os.IsNotExist(err) {
		defaultMarketDb = "data/leveraged_backtest.db"
	}
	return defaultMarketDb
}
