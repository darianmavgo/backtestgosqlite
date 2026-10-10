package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMarketSourcesNameDailyAndHourlyDatabases(t *testing.T) {
	dir := t.TempDir()
	daily := filepath.Join(dir, "market_history.db")
	if err := os.WriteFile(daily, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcs := MarketSources(daily)
	if len(srcs) != 2 || srcs[0].Path != daily || srcs[1].Path != filepath.Join(dir, "market_history_hourly.db") {
		t.Fatalf("sources = %+v", srcs)
	}
	if srcs[1].Detail != "not found" || strings.Contains(srcs[0].Detail, "not found") {
		t.Errorf("details = %q, %q", srcs[0].Detail, srcs[1].Detail)
	}
	var out bytes.Buffer
	PrintMarketSources(&out, daily, "backtest_start")
	if !strings.Contains(out.String(), "market_history_hourly.db") || !strings.Contains(out.String(), "table backtest_start") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestMarketSourcesForStatesWhenHourlyIsNotRead(t *testing.T) {
	daily := filepath.Join(t.TempDir(), "market_history.db")
	var out bytes.Buffer
	PrintMarketSourcesFor(&out, daily, "backtest_start", false)
	if !strings.Contains(out.String(), "not read here") {
		t.Errorf("output:\n%s", out.String())
	}
	if got := MarketSourcesFor(daily, true)[1].Use; strings.Contains(got, "not read") {
		t.Errorf("hourly use = %q", got)
	}
}

func TestLoadFillBarsIsSkippedWithDailyFills(t *testing.T) {
	DailyFills = true
	defer func() { DailyFills = false }()
	got, err := LoadFillBars(filepath.Join(t.TempDir(), "market_history.db"), []string{"AAA"})
	if err != nil || got != nil {
		t.Fatalf("LoadFillBars with -daily-fills = %v, %v; want nil, nil", got, err)
	}
}
