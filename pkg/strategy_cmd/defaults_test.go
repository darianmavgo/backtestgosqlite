package strategy_cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

// defaultsWorld makes a reference database with one hmm row and a gridsearch database with the
// columns `strategy defaults` reads, both real SQLite files in a temp folder.
func defaultsWorld(t *testing.T) (ref, grid string) {
	t.Helper()
	dir := t.TempDir()
	ref, grid = filepath.Join(dir, "strategies.db"), filepath.Join(dir, "gridsearch.db")
	db, err := refdb.Open(ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO hmm_strategy (id, name, hmm_symbol, trade_symbol, rule, stress_prob, hold_days, take_profit_pct, stop_loss_pct, allocation_pct, cash_yield, slippage_pct)
		VALUES ('hmm-normal-tecl', 'TECL', 'QQQ', 'TECL', 'normal_filter', 0.5, 3, 0, 0.12, 0.2, 0.045, 0.0005)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	g, err := storage.OpenSQLite(grid)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	for _, q := range []string{
		`CREATE TABLE gridsearch_results (id INTEGER PRIMARY KEY, strategy_id TEXT, label TEXT, hold_days INTEGER, take_profit_pct REAL, stop_loss_pct REAL)`,
		`INSERT INTO gridsearch_results VALUES (65, 'hmm-normal-tecl', 'TECL/Hold-3d/TP+0%/SL-12%', 3, 0, 0.12)`,
		`INSERT INTO gridsearch_results VALUES (218, 'hmm_normal_tecl', 'TECL/Hold-8d/TP+0%/SL-2%', 8, 0, 0.02)`,
		`INSERT INTO gridsearch_results VALUES (300, 'hmm-calm-soxl', 'SOXL/Hold-5d/TP+4%/SL-6%', 5, 0.04, 0.06)`,
	} {
		if _, err := g.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return ref, grid
}

func hmmRow(t *testing.T, ref string) (hold int, tp, sl float64, rule string) {
	t.Helper()
	db, err := refdb.Open(ref)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT hold_days, take_profit_pct, stop_loss_pct, rule FROM hmm_strategy WHERE id = 'hmm-normal-tecl'`).Scan(&hold, &tp, &sl, &rule); err != nil {
		t.Fatal(err)
	}
	return
}

func TestDefaultsCopiesOnlyTheThreeExitParameters(t *testing.T) {
	ref, grid := defaultsWorld(t)
	var out bytes.Buffer

	if err := SetDefaults(&out, ref, grid, "hmm-normal-tecl", "TECL/Hold-8d/TP+0%/SL-2%", true); err != nil {
		t.Fatal(err)
	}
	if hold, _, sl, _ := hmmRow(t, ref); hold != 3 || sl != 0.12 {
		t.Fatalf("a dry run wrote: hold %d stop %v", hold, sl)
	}
	if !strings.Contains(out.String(), "3 -> 8") || !strings.Contains(out.String(), "dry run") {
		t.Fatalf("dry run output:\n%s", out.String())
	}

	out.Reset()
	if err := SetDefaults(&out, ref, grid, "hmm-normal-tecl", "TECL/Hold-8d/TP+0%/SL-2%", false); err != nil {
		t.Fatal(err)
	}
	hold, tp, sl, rule := hmmRow(t, ref)
	if hold != 8 || tp != 0 || sl != 0.02 || rule != "normal_filter" {
		t.Fatalf("row after: hold %d tp %v sl %v rule %s", hold, tp, sl, rule)
	}
	out.Reset()
	if err := SetDefaults(&out, ref, grid, "hmm-normal-tecl", "TECL/Hold-8d/TP+0%/SL-2%", false); err != nil || !strings.Contains(out.String(), "no change") {
		t.Fatalf("a second run must say no change: %v\n%s", err, out.String())
	}
}

func TestDefaultsRefusesWhatDoesNotMatch(t *testing.T) {
	ref, grid := defaultsWorld(t)
	g, err := storage.OpenSQLite(grid)
	if err != nil {
		t.Fatal(err)
	}
	// The same label twice with different parameters cannot be told apart.
	for _, q := range []string{
		`INSERT INTO gridsearch_results VALUES (400, 'hmm-normal-tecl', 'TECL/Hold-9d/TP+0%/SL-3%', 9, 0, 0.03)`,
		`INSERT INTO gridsearch_results VALUES (401, 'hmm-normal-tecl', 'TECL/Hold-9d/TP+0%/SL-3%', 9, 0, 0.04)`,
		`INSERT INTO gridsearch_results VALUES (402, 'hmm-normal-tecl', 'TECL/Hold-3d/TP+0%/SL-12%', 3, 0, 0.12)`,
	} {
		if _, err := g.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	g.Close()
	var out bytes.Buffer
	for name, c := range map[string]struct{ id, label string }{
		"a label of another strategy": {"hmm-normal-tecl", "SOXL/Hold-5d/TP+4%/SL-6%"},
		"no such label":               {"hmm-normal-tecl", "TECL/Hold-99d/TP+0%/SL-1%"},
		"no such strategy":            {"hmm-normal-nope", "TECL/Hold-8d/TP+0%/SL-2%"},
		"an ambiguous label":          {"hmm-normal-tecl", "TECL/Hold-9d/TP+0%/SL-3%"},
		"no label given":              {"hmm-normal-tecl", ""},
	} {
		if err := SetDefaults(&out, ref, grid, c.id, c.label, false); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if hold, _, sl, _ := hmmRow(t, ref); hold != 3 || sl != 0.12 {
		t.Fatalf("a refused call changed the row: hold %d stop %v", hold, sl)
	}
	// The same label twice with the same parameters is fine (the baseline repeats a grid row).
	if err := SetDefaults(&out, ref, grid, "hmm-normal-tecl", "TECL/Hold-3d/TP+0%/SL-12%", false); err != nil {
		t.Fatalf("identical duplicates must be accepted: %v", err)
	}
}
