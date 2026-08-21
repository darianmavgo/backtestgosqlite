package datasource

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// StooqDataSource fetches historical daily data from Stooq CSV feeds.
type StooqDataSource struct {
	HTTPClient *http.Client
}

// NewStooqDataSource creates a new Stooq data source.
func NewStooqDataSource(client *http.Client) *StooqDataSource {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &StooqDataSource{
		HTTPClient: client,
	}
}

func (s *StooqDataSource) Name() string {
	return "stooq"
}

func (s *StooqDataSource) Fetch(ctx context.Context, req FetchRequest) ([]models.Bar, error) {
	symLower := strings.ToLower(req.Symbol)
	if !strings.Contains(symLower, ".") {
		symLower += ".us"
	}

	url := fmt.Sprintf("https://stooq.com/q/d/l/?s=%s&i=d", symLower)
	httpReq, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := s.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("stooq request failed for %s: %w", req.Symbol, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stooq HTTP status %d for %s", resp.StatusCode, req.Symbol)
	}

	r := csv.NewReader(resp.Body)
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read stooq csv: %w", err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("insufficient rows returned from stooq for %s", req.Symbol)
	}

	var bars []models.Bar
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) < 6 {
			continue
		}

		rawDate := strings.TrimSpace(row[0])
		parsedDt, normalizedDate := parseDateString(rawDate)

		// Filter by requested date range if set
		if !req.StartDate.IsZero() && !parsedDt.IsZero() && parsedDt.Before(req.StartDate) {
			continue
		}
		if !req.EndDate.IsZero() && !parsedDt.IsZero() && parsedDt.After(req.EndDate) {
			continue
		}

		o, _ := strconv.ParseFloat(row[1], 64)
		h, _ := strconv.ParseFloat(row[2], 64)
		l, _ := strconv.ParseFloat(row[3], 64)
		c, _ := strconv.ParseFloat(row[4], 64)
		v, _ := strconv.ParseInt(row[5], 10, 64)

		bars = append(bars, models.Bar{
			Idx:        len(bars) + 1,
			Symbol:     req.Symbol,
			Date:       normalizedDate,
			Timeframe:  "1d",
			AssetClass: "equity",
			Open:       o,
			High:       h,
			Low:        l,
			Close:      c,
			AdjClose:   c,
			Volume:     v,
			ParsedDt:   parsedDt,
		})
	}

	return bars, nil
}
