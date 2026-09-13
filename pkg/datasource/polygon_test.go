package datasource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParsePolygonTimeframe(t *testing.T) {
	tests := []struct {
		input       string
		expectedMult int
		expectedSpan string
	}{
		{"1d", 1, "day"},
		{"day", 1, "day"},
		{"", 1, "day"},
		{"1h", 1, "hour"},
		{"hour", 1, "hour"},
		{"5m", 5, "minute"},
		{"15m", 15, "minute"},
		{"1m", 1, "minute"},
		{"1w", 1, "week"},
		{"week", 1, "week"},
	}

	for _, tt := range tests {
		mult, span := parsePolygonTimeframe(tt.input)
		if mult != tt.expectedMult || span != tt.expectedSpan {
			t.Errorf("parsePolygonTimeframe(%q) = (%d, %q), expected (%d, %q)",
				tt.input, mult, span, tt.expectedMult, tt.expectedSpan)
		}
	}
}

func TestPolygonDataSource_Fetch_Success(t *testing.T) {
	mockResponse := PolygonAggsResponse{
		Ticker:       "AAPL",
		QueryCount:   2,
		ResultsCount: 2,
		Status:       "OK",
		Results: []PolygonAggResult{
			{
				O: 150.0,
				H: 155.0,
				L: 149.0,
				C: 153.0,
				V: 5000000,
				T: time.Date(2023, 1, 3, 0, 0, 0, 0, time.UTC).UnixMilli(),
			},
			{
				O: 153.0,
				H: 158.0,
				L: 152.0,
				C: 157.0,
				V: 6000000,
				T: time.Date(2023, 1, 4, 0, 0, 0, 0, time.UTC).UnixMilli(),
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-api-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockResponse)
	}))
	defer server.Close()

	src := NewPolygonDataSource("test-api-key", server.Client())
	src.BaseURL = server.URL

	ctx := context.Background()
	bars, err := src.Fetch(ctx, FetchRequest{
		Symbol:    "AAPL",
		StartDate: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2023, 1, 10, 0, 0, 0, 0, time.UTC),
		Timeframe: "1d",
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(bars) != 2 {
		t.Fatalf("expected 2 bars, got %d", len(bars))
	}

	if bars[0].Symbol != "AAPL" || bars[0].Open != 150.0 || bars[0].Close != 153.0 || bars[0].Volume != 5000000 {
		t.Errorf("unexpected bar 0: %+v", bars[0])
	}
	if bars[1].Date != "2023-01-04" || bars[1].Close != 157.0 {
		t.Errorf("unexpected bar 1: %+v", bars[1])
	}
}

func TestPolygonDataSource_Fetch_AuthError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	src := NewPolygonDataSource("invalid-key", server.Client())
	src.BaseURL = server.URL

	_, err := src.Fetch(context.Background(), FetchRequest{Symbol: "MSFT"})
	if err == nil {
		t.Fatalf("expected error for HTTP 401, got nil")
	}
}

func TestPolygonDataSource_Fetch_RateLimitError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	src := NewPolygonDataSource("valid-key", server.Client())
	src.BaseURL = server.URL

	_, err := src.Fetch(context.Background(), FetchRequest{Symbol: "NVDA"})
	if err == nil {
		t.Fatalf("expected error for HTTP 429, got nil")
	}
}

func TestResolvePolygonAPIKey_EnvFile(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, ".env")
	envContent := `
# Sample comment
POLYGON_API_KEY="test_key_from_env"
SOME_OTHER_VAR=123
`
	if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write temp .env: %v", err)
	}

	envMap, err := parseSimpleEnvFile(envPath)
	if err != nil {
		t.Fatalf("failed to parse env file: %v", err)
	}

	if envMap["POLYGON_API_KEY"] != "test_key_from_env" {
		t.Errorf("expected test_key_from_env, got %s", envMap["POLYGON_API_KEY"])
	}
}
