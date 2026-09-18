// cmd/etf_universe — Discovers every active US-listed ETF ticker via Polygon.io's
// reference tickers API and saves them to the reference DB's etf_universe table (list "all") for
// consumption by cmd/download -list.
//
// Note: Polygon's list endpoint (v3/reference/tickers) does not return a listing
// date, so this tool only discovers the *symbol universe*. Filtering down to ETFs
// with 6+ years of actual price history happens after downloading (see
// cmd/filter_etf_history), since that's the only reliable source of truth for how
// far back a ticker's data actually goes.
//
// Usage:
//
//	go run cmd/etf_universe/main.go
//	go run cmd/etf_universe/main.go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/datasource"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
)

type tickerResult struct {
	Ticker string `json:"ticker"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type tickersResponse struct {
	Results []tickerResult `json:"results"`
	NextURL string         `json:"next_url"`
	Status  string         `json:"status"`
	Error   string         `json:"error"`
}

func main() {
	refPath := flag.String("ref-db", refdb.DefaultPath, "Reference DB to save the universe into (etf_universe, list \"all\")")
	polygonKey := flag.String("polygon-key", "", "Polygon.io API key (or set POLYGON_API_KEY / .env)")
	limit := flag.Int("limit", 1000, "Page size per API request (max 1000)")
	flag.Parse()

	apiKey := strings.TrimSpace(*polygonKey)
	if apiKey == "" {
		apiKey = datasource.ResolvePolygonAPIKey()
	}
	if apiKey == "" {
		log.Fatalf("Polygon API key is required. Pass -polygon-key or set POLYGON_API_KEY in environment or .env")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf("https://api.polygon.io/v3/reference/tickers?market=stocks&type=ETF&active=true&sort=ticker&order=asc&limit=%d&apiKey=%s", *limit, apiKey)

	var allTickers []string
	page := 0

	for url != "" {
		page++
		fmt.Printf("📥 Fetching page %d...\n", page)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			log.Fatalf("Failed to build request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			log.Fatalf("Request failed: %v", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			log.Fatalf("Failed to read response body: %v", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			fmt.Println("⏳ Rate limited (429). Waiting 60s before retry...")
			time.Sleep(60 * time.Second)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			log.Fatalf("Polygon API returned HTTP %d: %s", resp.StatusCode, string(body))
		}

		var parsed tickersResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			log.Fatalf("Failed to parse response JSON: %v", err)
		}
		if parsed.Error != "" {
			log.Fatalf("Polygon API error: %s", parsed.Error)
		}

		for _, t := range parsed.Results {
			if t.Active && t.Ticker != "" {
				allTickers = append(allTickers, strings.ToUpper(t.Ticker))
			}
		}
		fmt.Printf("   ...%d tickers so far\n", len(allTickers))

		if parsed.NextURL == "" {
			break
		}
		// next_url does not carry the API key forward
		if strings.Contains(parsed.NextURL, "apiKey=") {
			url = parsed.NextURL
		} else {
			sep := "&"
			if !strings.Contains(parsed.NextURL, "?") {
				sep = "?"
			}
			url = parsed.NextURL + sep + "apiKey=" + apiKey
		}

		// Free tier: 5 requests/minute
		time.Sleep(13 * time.Second)
	}

	ref, err := refdb.Open(*refPath)
	if err != nil {
		log.Fatalf("Failed to open reference DB %s: %v", *refPath, err)
	}
	defer ref.Close()
	if err := refdb.SaveUniverse(ref, refdb.ListAll, allTickers); err != nil {
		log.Fatalf("Failed to save universe: %v", err)
	}

	fmt.Printf("\n✨ Discovered %d active US ETF tickers. Saved to %s (etf_universe, list %q)\n", len(allTickers), *refPath, refdb.ListAll)
}
