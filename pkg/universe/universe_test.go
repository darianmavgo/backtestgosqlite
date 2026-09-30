package universe

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClassifyTicker(t *testing.T) {
	tests := []struct {
		sym       string
		name      string
		assetType string
		wantETF   bool
		wantLev   string
		wantDir   string
		wantCat   string
	}{
		{
			sym:       "SPY",
			name:      "SPDR S&P 500 ETF Trust",
			assetType: "ETF",
			wantETF:   true,
			wantLev:   "none",
			wantDir:   "long",
			wantCat:   "broad_market_sp500",
		},
		{
			sym:       "TQQQ",
			name:      "ProShares UltraPro QQQ",
			assetType: "ETF",
			wantETF:   true,
			wantLev:   "3x",
			wantDir:   "long",
			wantCat:   "technology",
		},
		{
			sym:       "SOXL",
			name:      "Direxion Daily Semiconductor Bull 3X Shares",
			assetType: "ETF",
			wantETF:   true,
			wantLev:   "3x",
			wantDir:   "long",
			wantCat:   "semiconductors",
		},
		{
			sym:       "SOXS",
			name:      "Direxion Daily Semiconductor Bear 3X Shares",
			assetType: "ETF",
			wantETF:   true,
			wantLev:   "3x",
			wantDir:   "inverse",
			wantCat:   "semiconductors",
		},
		{
			sym:       "UPRO",
			name:      "ProShares UltraPro S&P500",
			assetType: "ETF",
			wantETF:   true,
			wantLev:   "3x",
			wantDir:   "long",
			wantCat:   "broad_market_sp500",
		},
		{
			sym:       "SSO",
			name:      "ProShares Ultra S&P500",
			assetType: "ETF",
			wantETF:   true,
			wantLev:   "2x",
			wantDir:   "long",
			wantCat:   "broad_market_sp500",
		},
		{
			sym:       "NVDL",
			name:      "GraniteShares 2x Long NVDA Daily ETF",
			assetType: "ETF",
			wantETF:   true,
			wantLev:   "2x",
			wantDir:   "long",
			wantCat:   "other_etf",
		},
		{
			sym:       "AAPL",
			name:      "Apple Inc. Common Stock",
			assetType: "CS",
			wantETF:   false,
			wantLev:   "none",
			wantDir:   "long",
			wantCat:   "common_stock",
		},
		{
			sym:       "BABA",
			name:      "Alibaba Group Holding Ltd American Depositary Shares",
			assetType: "ADRC",
			wantETF:   false,
			wantLev:   "none",
			wantDir:   "long",
			wantCat:   "adr",
		},
		{
			sym:       "TLT",
			name:      "iShares 20+ Year Treasury Bond ETF",
			assetType: "ETF",
			wantETF:   true,
			wantLev:   "none",
			wantDir:   "long",
			wantCat:   "fixed_income",
		},
	}

	for _, tt := range tests {
		isETF, lev, dir, cat := ClassifyTicker(tt.sym, tt.name, tt.assetType)
		if isETF != tt.wantETF {
			t.Errorf("[%s] isETF = %v, want %v", tt.sym, isETF, tt.wantETF)
		}
		if lev != tt.wantLev {
			t.Errorf("[%s] leverage = %s, want %s", tt.sym, lev, tt.wantLev)
		}
		if dir != tt.wantDir {
			t.Errorf("[%s] direction = %s, want %s", tt.sym, dir, tt.wantDir)
		}
		if cat != tt.wantCat {
			t.Errorf("[%s] category = %s, want %s", tt.sym, cat, tt.wantCat)
		}
	}
}

func TestUniverseDBSaveAndQuery(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "universe_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "universe.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open universe db: %v", err)
	}
	defer db.Close()

	records := []SymbolRecord{
		{
			Symbol:         "SPY",
			Name:           "SPDR S&P 500 ETF Trust",
			AssetType:      "ETF",
			IsETF:          true,
			Leverage:       "none",
			Direction:      "long",
			Category:       "broad_market_sp500",
			Exchange:       "ARCX",
			FirstTradeDate: "1993-01-29",
			Active:         true,
			Confirmed2021:  true,
		},
		{
			Symbol:         "TQQQ",
			Name:           "ProShares UltraPro QQQ",
			AssetType:      "ETF",
			IsETF:          true,
			Leverage:       "3x",
			Direction:      "long",
			Category:       "technology",
			Exchange:       "XNAS",
			FirstTradeDate: "2010-02-11",
			Active:         true,
			Confirmed2021:  true,
		},
		{
			Symbol:         "NEW_SYM",
			Name:           "Recent IPO Inc",
			AssetType:      "CS",
			IsETF:          false,
			Leverage:       "none",
			Direction:      "long",
			Category:       "common_stock",
			Exchange:       "XNAS",
			FirstTradeDate: "2023-05-15",
			Active:         true,
			Confirmed2021:  false,
		},
	}

	if err := SaveSymbols(db, records); err != nil {
		t.Fatalf("failed to save symbols: %v", err)
	}

	var count2021 int
	err = db.Get(&count2021, "SELECT COUNT(*) FROM universe WHERE confirmed_2021 = 1")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count2021 != 2 {
		t.Errorf("expected 2 confirmed 2021 symbols, got %d", count2021)
	}

	var lev3xCount int
	err = db.Get(&lev3xCount, "SELECT COUNT(*) FROM universe WHERE leverage = '3x'")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if lev3xCount != 1 {
		t.Errorf("expected 1 3x leveraged symbol, got %d", lev3xCount)
	}
}

func TestVerifySymbolHistoryLive(t *testing.T) {
	// Verify that checking Yahoo doesn't fetch full history and correctly identifies cutoff
	res := VerifySymbolHistory(nil, "AAPL")
	if !res.Valid {
		t.Fatalf("expected AAPL to be valid")
	}
	if !res.Confirmed2021 {
		t.Errorf("expected AAPL to be confirmed for Jan 2021, got %v (first trade %s)", res.Confirmed2021, res.FirstTradeDate)
	}

	resDate, err := time.Parse("2006-01-02", res.FirstTradeDate)
	if err != nil {
		t.Fatalf("invalid date format: %s", res.FirstTradeDate)
	}
	if resDate.After(CutoffDate) {
		t.Errorf("AAPL first trade date %s should be before cutoff %s", res.FirstTradeDate, CutoffDate)
	}
}
