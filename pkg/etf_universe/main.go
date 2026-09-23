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
package etf_universe

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
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

// Config holds the settings of an ETF universe discovery run.
type Config struct {
	RefDB      string    // reference DB to save the universe into
	PolygonKey string    // falls back to POLYGON_API_KEY / .env
	Limit      int       // page size per API request (max 1000)
	Out        io.Writer // progress output; nil discards
}

// DefaultConfig returns the CLI defaults.
func DefaultConfig() Config {
	return Config{RefDB: refdb.DefaultPath, Limit: 1000}
}

// Main is the CLI entry point.
func Main() {
	cfg := DefaultConfig()
	flag.StringVar(&cfg.RefDB, "ref-db", cfg.RefDB, "Reference DB to save the universe into (etf_universe, list \"all\")")
	flag.StringVar(&cfg.PolygonKey, "polygon-key", "", "Polygon.io API key (or set POLYGON_API_KEY / .env)")
	flag.IntVar(&cfg.Limit, "limit", cfg.Limit, "Page size per API request (max 1000)")
	flag.Parse()
	cfg.Out = os.Stdout
	if _, err := Run(cfg); err != nil {
		log.Fatal(err)
	}
}

// Run discovers active US ETF tickers via Polygon, saves them into the
// reference DB, and returns the number found.
func Run(cfg Config) (int, error) {
	out := cfg.Out
	if out == nil {
		out = io.Discard
	}
	apiKey := strings.TrimSpace(cfg.PolygonKey)
	if apiKey == "" {
		apiKey = datasource.ResolvePolygonAPIKey()
	}
	if apiKey == "" {
		return 0, fmt.Errorf("Polygon API key is required. Pass -polygon-key or set POLYGON_API_KEY in environment or .env")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf("https://api.polygon.io/v3/reference/tickers?market=stocks&type=ETF&active=true&sort=ticker&order=asc&limit=%d&apiKey=%s", cfg.Limit, apiKey)

	var allTickers []string
	page := 0

	for url != "" {
		page++
		fmt.Fprintf(out, "📥 Fetching page %d...\n", page)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return 0, fmt.Errorf("Failed to build request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return 0, fmt.Errorf("Request failed: %v", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return 0, fmt.Errorf("Failed to read response body: %v", err)
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			fmt.Fprintln(out, "⏳ Rate limited (429). Waiting 60s before retry...")
			time.Sleep(60 * time.Second)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return 0, fmt.Errorf("Polygon API returned HTTP %d: %s", resp.StatusCode, string(body))
		}

		var parsed tickersResponse
		if err := json.Unmarshal(body, &parsed); err != nil {
			return 0, fmt.Errorf("Failed to parse response JSON: %v", err)
		}
		if parsed.Error != "" {
			return 0, fmt.Errorf("Polygon API error: %s", parsed.Error)
		}

		for _, t := range parsed.Results {
			if t.Active && t.Ticker != "" {
				allTickers = append(allTickers, strings.ToUpper(t.Ticker))
			}
		}
		fmt.Fprintf(out, "   ...%d tickers so far\n", len(allTickers))

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

	ref, err := refdb.Open(cfg.RefDB)
	if err != nil {
		return 0, fmt.Errorf("Failed to open reference DB %s: %v", cfg.RefDB, err)
	}
	defer ref.Close()
	if err := refdb.SaveUniverse(ref, refdb.ListAll, allTickers); err != nil {
		return 0, fmt.Errorf("Failed to save universe: %v", err)
	}

	fmt.Fprintf(out, "\n✨ Discovered %d active US ETF tickers. Saved to %s (etf_universe, list %q)\n", len(allTickers), cfg.RefDB, refdb.ListAll)
	return len(allTickers), nil
}
