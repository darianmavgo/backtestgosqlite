package universe

import (
	"flag"
	"fmt"
	"log"
	"os"
)

// Main is the CLI entrypoint for cmd/universe.
func Main() {
	cfg := DefaultConfig()
	flag.StringVar(&cfg.DBPath, "db", cfg.DBPath, "Universe SQLite database path")
	flag.StringVar(&cfg.PolygonKey, "polygon-key", "", "Polygon.io API key (falls back to POLYGON_API_KEY / .env)")
	flag.IntVar(&cfg.Workers, "workers", cfg.Workers, "Number of concurrent verification workers")
	flag.IntVar(&cfg.Limit, "limit", cfg.Limit, "Page size for Polygon discovery")
	flag.IntVar(&cfg.MaxChecks, "max-checks", 0, "Limit number of candidate tickers to verify (0 = all)")
	flag.BoolVar(&cfg.ETFsOnly, "etfs-only", false, "Only process ETFs")
	flag.BoolVar(&cfg.StocksOnly, "stocks-only", false, "Only process non-ETF stocks")
	flag.Parse()

	cfg.Out = os.Stdout

	fmt.Println("==================================================")
	fmt.Println("🌌 UNIVERSE: Stock & ETF Discovery & Classification")
	fmt.Println("==================================================")

	if _, err := Run(cfg); err != nil {
		log.Fatalf("Universe error: %v", err)
	}
}
