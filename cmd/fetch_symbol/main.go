package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	_ "github.com/mattn/go-sqlite3"
)

type YFEventResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol string `json:"symbol"`
			} `json:"meta"`
			Timestamp  []int64 `json:"timestamp"`
			Events     struct {
				Dividends map[string]struct {
					Amount float64 `json:"amount"`
					Date   int64   `json:"date"`
				} `json:"dividends"`
			} `json:"events"`
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

func main() {
	symbol := flag.String("symbol", "GOOGL", "Ticker symbol to download")
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	startDate := time.Date(2021, 8, 1, 0, 0, 0, 0, time.UTC)
	endDate := time.Now().UTC()

	url := fmt.Sprintf(
		"https://query1.finance.yahoo.com/v8/finance/chart/%s?period1=%d&period2=%d&interval=1d&events=div",
		*symbol, startDate.Unix(), endDate.Unix(),
	)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Failed to fetch %s: %v", *symbol, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read body: %v", err)
	}

	var yf YFEventResponse
	if err := json.Unmarshal(body, &yf); err != nil {
		log.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if len(yf.Chart.Result) == 0 {
		log.Fatalf("No result for %s", *symbol)
	}

	res := yf.Chart.Result[0]
	quotes := res.Indicators.Quote[0]
	timestamps := res.Timestamp

	tx, err := db.Beginx()
	if err != nil {
		log.Fatalf("Failed to begin tx: %v", err)
	}

	// Insert Bars into backtest_start
	barStmt, err := tx.Preparex(`
		INSERT INTO backtest_start (idx, symbol, Date, timeframe, asset_class, open, high, low, close, "Adj Close", volume)
		VALUES (?, ?, ?, '1d', 'equity', ?, ?, ?, ?, ?, ?)
		ON CONFLICT(symbol, Date) DO UPDATE SET
			open=excluded.open,
			high=excluded.high,
			low=excluded.low,
			close=excluded.close,
			"Adj Close"=excluded."Adj Close",
			volume=excluded.volume;
	`)
	if err != nil {
		log.Fatalf("Failed to prepare bar stmt: %v", err)
	}
	defer barStmt.Close()

	insertedBars := 0
	for i, ts := range timestamps {
		if i >= len(quotes.Open) || quotes.Open[i] == nil || quotes.Close[i] == nil {
			continue
		}
		t := time.Unix(ts, 0).UTC()
		dateStr := t.Format("2006-01-02")

		o := *quotes.Open[i]
		h := *quotes.High[i]
		l := *quotes.Low[i]
		c := *quotes.Close[i]
		var v int64
		if i < len(quotes.Volume) && quotes.Volume[i] != nil {
			v = *quotes.Volume[i]
		}
		adj := c
		if len(res.Indicators.Adjclose) > 0 && len(res.Indicators.Adjclose[0].Adjclose) > i && res.Indicators.Adjclose[0].Adjclose[i] != nil {
			adj = *res.Indicators.Adjclose[0].Adjclose[i]
		}

		_, err = barStmt.Exec(i+1, *symbol, dateStr, o, h, l, c, adj, v)
		if err != nil {
			log.Fatalf("Failed to insert bar %s %s: %v", *symbol, dateStr, err)
		}
		insertedBars++
	}

	// Insert Dividends into corporate_dividends
	divStmt, err := tx.Preparex(`
		INSERT INTO corporate_dividends (symbol, ex_date, amount)
		VALUES (?, ?, ?)
		ON CONFLICT(symbol, ex_date) DO UPDATE SET amount=excluded.amount;
	`)
	if err != nil {
		log.Fatalf("Failed to prepare div stmt: %v", err)
	}
	defer divStmt.Close()

	insertedDivs := 0
	for _, div := range res.Events.Dividends {
		t := time.Unix(div.Date, 0).UTC()
		exDate := t.Format("2006-01-02")
		_, err = divStmt.Exec(*symbol, exDate, div.Amount)
		if err != nil {
			log.Fatalf("Failed to insert dividend %s %s: %v", *symbol, exDate, err)
		}
		insertedDivs++
	}

	if err := tx.Commit(); err != nil {
		log.Fatalf("Failed to commit tx: %v", err)
	}

	fmt.Printf("✅ Successfully fetched & saved %s!\n", *symbol)
	fmt.Printf("   • Total Daily Price Bars: %d\n", insertedBars)
	fmt.Printf("   • Total Ex-Dividend Payments: %d\n\n", insertedDivs)
}
