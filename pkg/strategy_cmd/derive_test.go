package strategy_cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
)

func TestDeriveCommandCopiesARowFromASweepLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "strategies.db")
	db, err := refdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO rotation_strategy
		(id, name, symbols, universe_size, top_k, exit_buffer, max_weight_pct, regime_symbol, regime_sma, allocation_pct, cash_yield, slippage_pct, period, side, pick)
		VALUES ('base', 'Base', 'AAA,BBB', 2, 2, 0, 0.1, 'QQQ', 0, 0.1, 0, 0.0005, '1d', 'long', 'all')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	var out bytes.Buffer
	line := "#1  Period-1d/Limit-95%/Hold-2d/TP+3%/SL-3%   Profit=+$258505.67  CAGR=37.73%"
	if err := Derive(&out, path, "base", "base-95", "Base 95", line); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "./bin/backtest -strategy base-95") {
		t.Errorf("output should say how to run it:\n%s", out.String())
	}
	db, err = refdb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	row, ok, err := refdb.RotationStrategyByID(db, "base-95")
	if err != nil || !ok || row.Name != "Base 95" || row.EntryLimitPct != 0.95 || row.HoldDays != 2 || row.Symbols != "AAA,BBB" {
		t.Fatalf("row %+v ok=%v err=%v", row, ok, err)
	}

	if err := Derive(&out, path, "", "", "", line); err == nil {
		t.Error("-from is required")
	}
	if err := Derive(&out, path, "base", "", "", "no parameters here"); err == nil {
		t.Error("text with no parameter set must be refused")
	}
}
