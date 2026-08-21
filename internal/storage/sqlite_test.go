package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

func TestValidateTableName(t *testing.T) {
	valid := []string{"backtest_start", "trades", "wc_summary", "test_123", ""}
	for _, name := range valid {
		if err := ValidateTableName(name); err != nil {
			t.Errorf("expected %q to be valid, got %v", name, err)
		}
	}

	invalid := []string{"table; DROP TABLE users;", "table name", "foo-bar", "test--comment", "table'"}
	for _, name := range invalid {
		if err := ValidateTableName(name); err == nil {
			t.Errorf("expected %q to be rejected, got nil error", name)
		}
	}
}

func TestTradePersistenceAndSQLAggregates(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sqlitetest_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "test.db")
	db, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer db.Close()

	trades := []models.Trade{
		{
			Symbol:         "AAPL",
			OrderType:      "limit",
			EntryDate:      "2023-01-05",
			EntryPrice:     150.0,
			ExitDate:       "2023-01-10",
			ExitPrice:      165.0,
			ExitReason:     models.ExitReasonProfitTarget,
			Shares:         100,
			NetPnL:         1500.0,
			ReturnPct:      0.10,
			HoldDays:       5,
			CommissionPaid: 2.0,
			MaxAdverseExcursion: -0.01,
			MaxFavorableExcursion: 0.11,
		},
		{
			Symbol:         "MSFT",
			OrderType:      "limit",
			EntryDate:      "2023-01-06",
			EntryPrice:     240.0,
			ExitDate:       "2023-01-11",
			ExitPrice:      228.0,
			ExitReason:     models.ExitReasonStopLoss,
			Shares:         50,
			NetPnL:         -600.0,
			ReturnPct:      -0.05,
			HoldDays:       5,
			CommissionPaid: 2.0,
			MaxAdverseExcursion: -0.05,
			MaxFavorableExcursion: 0.01,
		},
	}

	stratID := "test-strat"
	if err := SaveTrades(db, stratID, trades); err != nil {
		t.Fatalf("failed to save trades: %v", err)
	}

	report, err := FetchTradeSummaryStats(db, stratID)
	if err != nil {
		t.Fatalf("failed to fetch trade summary stats: %v", err)
	}

	if report.TotalTrades != 2 {
		t.Errorf("expected 2 total trades, got %d", report.TotalTrades)
	}
	if report.WinningTrades != 1 {
		t.Errorf("expected 1 winning trade, got %d", report.WinningTrades)
	}
	if report.LosingTrades != 1 {
		t.Errorf("expected 1 losing trade, got %d", report.LosingTrades)
	}
	if report.WinRate != 0.5 {
		t.Errorf("expected 0.5 win rate, got %f", report.WinRate)
	}
	if report.NetProfit != 900.0 {
		t.Errorf("expected net profit 900.0, got %f", report.NetProfit)
	}
	if report.TotalCommissionPaid != 4.0 {
		t.Errorf("expected total commission 4.0, got %f", report.TotalCommissionPaid)
	}
	if report.ProfitFactor != 2.5 {
		t.Errorf("expected profit factor 2.5, got %f", report.ProfitFactor)
	}
}
