package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

type YFResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol string `json:"symbol"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Close  []*float64 `json:"close"`
					Volume []*int64   `json:"volume"`
				} `json:"quote"`
				Adjclose []struct {
					Adjclose []*float64 `json:"adjclose"`
				} `json:"adjclose"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

func getSymbolsFromTable(settingsDbPath, tableName string, limit int) ([]string, error) {
	db, err := sqlx.Open("sqlite3", settingsDbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var symbols []string
	query := fmt.Sprintf("SELECT DISTINCT symbol FROM %s WHERE symbol != ''", tableName)
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}
	err = db.Select(&symbols, query)
	return symbols, err
}

func fetchYahooChart(client *http.Client, symbol string, start, end time.Time) (*YFResponse, error) {
	url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?period1=%d&period2=%d&interval=1d&events=history",
		symbol, start.Unix(), end.Unix())

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var yf YFResponse
	err = json.Unmarshal(body, &yf)
	if err != nil {
		return nil, err
	}
	return &yf, nil
}

func fetchStooqCSV(client *http.Client, symbol string) ([][]string, error) {
	// Stooq backup: https://stooq.com/q/d/l/?s=tqqq.us&i=d
	symLower := strings.ToLower(symbol)
	if !strings.Contains(symLower, ".") {
		symLower += ".us"
	}
	url := fmt.Sprintf("https://stooq.com/q/d/l/?s=%s&i=d", symLower)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	r := csv.NewReader(resp.Body)
	return r.ReadAll()
}

func main() {
	targetDb := flag.String("db", "data/leveraged_backtest.db", "Target SQLite DB")
	settingsDb := flag.String("settings", "data/settings.db", "Settings DB path")
	table := flag.String("table", "leveraged_etf", "Table name with symbols")
	limit := flag.Int("limit", 50, "Limit number of symbols (0 for all)")
	years := flag.Int("years", 4, "Number of years of history")
	flag.Parse()

	symbols, err := getSymbolsFromTable(*settingsDb, *table, *limit)
	if err != nil {
		log.Fatalf("Failed to fetch symbols: %v", err)
	}
	fmt.Printf("Fetched %d symbols from %s: %v\n", len(symbols), *table, symbols)

	db, err := sqlx.Open("sqlite3", *targetDb)
	if err != nil {
		log.Fatalf("Failed to open target DB: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS backtest_start (
			idx INTEGER,
			Date DATETIME,
			open FLOAT,
			high FLOAT,
			low FLOAT,
			close FLOAT,
			"Adj Close" FLOAT,
			volume BIGINT,
			symbol TEXT
		);
	`)

	client := &http.Client{Timeout: 15 * time.Second}
	now := time.Now().UTC()
	start := now.AddDate(-*years, 0, 0)
	end := now

	fmt.Printf("Downloading daily bars from %s to %s for %d symbols...\n\n", start.Format("2006-01-02"), end.Format("2006-01-02"), len(symbols))

	totalBars := 0
	successfulSymbols := 0

	for idx, sym := range symbols {
		yf, err := fetchYahooChart(client, sym, start, end)
		var barCount int

		if err == nil && len(yf.Chart.Result) > 0 {
			res := yf.Chart.Result[0]
			timestamps := res.Timestamp
			quotes := res.Indicators.Quote[0]

			tx, _ := db.Begin()
			stmt, _ := tx.Prepare(`
				INSERT INTO backtest_start (symbol, Date, open, high, low, close, volume)
				VALUES (?, ?, ?, ?, ?, ?, ?)
			`)

			for i, ts := range timestamps {
				if i >= len(quotes.Open) || quotes.Open[i] == nil || quotes.Close[i] == nil {
					continue
				}
				dateStr := time.Unix(ts, 0).UTC().Format("2006-01-02 00:00:00")
				o := *quotes.Open[i]
				h := *quotes.High[i]
				l := *quotes.Low[i]
				c := *quotes.Close[i]
				var v int64
				if i < len(quotes.Volume) && quotes.Volume[i] != nil {
					v = *quotes.Volume[i]
				}
				stmt.Exec(sym, dateStr, o, h, l, c, v)
				barCount++
			}
			stmt.Close()
			tx.Commit()
		} else {
			// Fallback to Stooq
			records, sErr := fetchStooqCSV(client, sym)
			if sErr == nil && len(records) > 1 {
				tx, _ := db.Begin()
				stmt, _ := tx.Prepare(`
					INSERT INTO backtest_start (symbol, Date, open, high, low, close, volume)
					VALUES (?, ?, ?, ?, ?, ?, ?)
				`)
				// Header: Date,Open,High,Low,Close,Volume
				for rIdx := 1; rIdx < len(records); rIdx++ {
					row := records[rIdx]
					if len(row) < 6 {
						continue
					}
					d := row[0] + " 00:00:00"
					o, _ := strconv.ParseFloat(row[1], 64)
					h, _ := strconv.ParseFloat(row[2], 64)
					l, _ := strconv.ParseFloat(row[3], 64)
					c, _ := strconv.ParseFloat(row[4], 64)
					v, _ := strconv.ParseInt(row[5], 10, 64)
					stmt.Exec(sym, d, o, h, l, c, v)
					barCount++
				}
				stmt.Close()
				tx.Commit()
			} else {
				log.Printf("[%d/%d] Failed %s: YF err: %v, Stooq err: %v", idx+1, len(symbols), sym, err, sErr)
				continue
			}
		}

		successfulSymbols++
		totalBars += barCount
		fmt.Printf("[%d/%d] %-6s : %4d bars saved (Total: %d bars)\n", idx+1, len(symbols), sym, barCount, totalBars)
		time.Sleep(150 * time.Millisecond) // gentle rate limit
	}

	fmt.Printf("\nDone! Downloaded %d total bars across %d/%d symbols into %s.\n", totalBars, successfulSymbols, len(symbols), *targetDb)
}
