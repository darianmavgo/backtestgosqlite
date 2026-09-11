package main

import (
	"path/filepath"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

func TestDetectAndDownloadMissingData_Disabled(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_market.db")
	tableName := "backtest_start"

	db, err := storage.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("OpenSQLite failed: %v", err)
	}
	if err := storage.EnsureBarTable(db, tableName); err != nil {
		t.Fatalf("EnsureBarTable failed: %v", err)
	}
	db.Close()

	comboStrat := strategy.NewVOOTECLSPXUCombo()
	err = detectAndDownloadMissingData(dbPath, tableName, []strategy.Strategy{comboStrat}, "", false, 5)
	if err == nil {
		t.Fatalf("expected error when autoDownload=false and data missing, got nil")
	}
}
