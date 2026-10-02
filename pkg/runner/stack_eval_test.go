package runner

import (
	"strings"
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

type evalMock struct {
	id      string
	name    string
	cfg     strategy.StrategyConfig
	symbols []string
	sigs    []models.Signal
}

func (m *evalMock) ID() string                             { return m.id }
func (m *evalMock) Name() string                           { return m.name }
func (m *evalMock) Description() string                    { return m.name }
func (m *evalMock) DefaultConfig() strategy.StrategyConfig { return m.cfg }
func (m *evalMock) Validate() error                        { return nil }
func (m *evalMock) SetDatabases(string, string)            {}
func (m *evalMock) RequiredSymbols() []string              { return m.symbols }
func (m *evalMock) GenerateSignals(map[string][]models.Bar) []models.Signal {
	return m.sigs
}

func TestExecuteStackAllocOverrideSizesTenPercent(t *testing.T) {
	const price = 100.0
	bar := func(sym string) []models.Bar {
		return []models.Bar{
			{Date: "2026-01-02", Open: price, High: price, Low: price, Close: price},
			{Date: "2026-01-05", Open: price, High: price, Low: price, Close: price},
		}
	}
	mk := func(id, sym string, priority int) *evalMock {
		return &evalMock{
			id: id,
			cfg: strategy.StrategyConfig{
				ID: id, AllocationPct: 0.65, PositionCap: 1, HoldingWindow: 8,
				PositionSizing: "fixed_pct",
			},
			sigs: []models.Signal{{
				Date: "2026-01-02", Symbol: sym, Close: price, BuyLimit: price,
				StrategyID: id, Priority: priority, OrderType: "limit",
			}},
		}
	}
	primary := mk("primary", "AAA", 0)
	secondary := mk("secondary", "BBB", 1)
	res := ExecuteStack(StackRequest{
		Primary:      primary,
		Secondaries:  []strategy.Strategy{secondary},
		BarsBySymbol: map[string][]models.Bar{"AAA": bar("AAA"), "BBB": bar("BBB")},
		SortedDates:  []string{"2026-01-02", "2026-01-05"},
		Capital:      100000,
		Persist:      false,
		Signals:      append(append([]models.Signal{}, primary.sigs...), secondary.sigs...),
		Override:     ConfigOverride{AllocPct: 0.10},
	})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if res.AllocPct != 0.10 {
		t.Errorf("AllocPct = %v, want 0.10", res.AllocPct)
	}
	if res.PreemptedCount != 0 {
		t.Errorf("preempted %d trades; 10%% slots on different symbols should both fit", res.PreemptedCount)
	}
	if len(res.Trades) != 2 {
		t.Fatalf("trades = %d, want 2", len(res.Trades))
	}
	for _, tr := range res.Trades {
		if tr.Shares != 100 {
			t.Errorf("%s shares = %d, want 100 (10%% of $100k at $100), default alloc was 65%%", tr.StrategyID, tr.Shares)
		}
	}
}

func TestExecuteStackDefaultAssetNamesTheFile(t *testing.T) {
	const price = 100.0
	dates := []string{"2026-01-02", "2026-01-05"}
	bars := func(px float64) []models.Bar {
		out := make([]models.Bar, len(dates))
		for i, d := range dates {
			out[i] = models.Bar{Date: d, Open: px, High: px, Low: px, Close: px, AdjClose: px}
		}
		return out
	}
	primary := &evalMock{
		id: "primary",
		cfg: strategy.StrategyConfig{
			ID: "primary", AllocationPct: 0.10, PositionCap: 1, HoldingWindow: 8,
			PositionSizing: "fixed_pct",
		},
	}
	res := ExecuteStack(StackRequest{
		Primary:      primary,
		BarsBySymbol: map[string][]models.Bar{"PARK": bars(price), "AAA": bars(price)},
		SortedDates:  dates,
		Capital:      100000,
		Persist:      true,
		OutDir:       t.TempDir(),
		Signals:      []models.Signal{},
		DefaultAsset: "park",
	})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	if res.Default.Symbol != "PARK" {
		t.Fatalf("default symbol %q, want PARK", res.Default.Symbol)
	}
	if !strings.Contains(res.DbPath, "_default-park.db") {
		t.Fatalf("db path %s, want _default-park.db", res.DbPath)
	}
	if res.Default.AvgWeight < 0.9 {
		t.Fatalf("avg park weight %.3f, want most of the book", res.Default.AvgWeight)
	}
	if res.Idle.DaysFullyIdle != 0 {
		t.Fatalf("idle days %d, want 0 while the park is held", res.Idle.DaysFullyIdle)
	}
}

func TestBuildConfigAllocForcesFixedPct(t *testing.T) {
	m := &evalMock{
		id: "one-share",
		cfg: strategy.StrategyConfig{
			ID: "one-share", AllocationPct: 1, PositionSizing: "fixed_shares",
			FixedShares: 1, PositionCap: 1, HoldingWindow: 1,
		},
	}
	over := ConfigOverride{AllocPct: 0.10}.Apply(m.DefaultConfig())
	if over.AllocationPct != 0.10 || over.PositionSizing != "fixed_pct" {
		t.Fatalf("override = %+v, want 10%% fixed_pct", over)
	}
	kept := ConfigOverride{}.Apply(m.DefaultConfig())
	if kept.AllocationPct != 1 || kept.PositionSizing != "fixed_shares" {
		t.Fatalf("zero override changed config: %+v", kept)
	}
}

func TestOverlayCandidates_SkipsPrimarySQLAndSiblings(t *testing.T) {
	primary, ok := strategy.Get("sig-voo-buy-tecl")
	if !ok {
		t.Fatal("sig-voo-buy-tecl not registered")
	}
	cands := OverlayCandidates(primary, OverlayCandidateOptions{})
	seen := map[string]bool{}
	for _, c := range cands {
		if c.ID() == "sig-voo-buy-tecl" {
			t.Error("primary should not be an overlay candidate")
		}
		if c.ID() == "voo-tecl-spxu-combo" {
			t.Error("sibling combo should not be an overlay candidate")
		}
		if c.ID() == "voo-buy-hold" || c.ID() == "genetic-momentum" {
			t.Errorf("heavy strategy %s should be excluded by default", c.ID())
		}
		if len(c.ID()) >= 4 && c.ID()[len(c.ID())-4:] == "-sql" {
			t.Errorf("SQL duplicate %s should be excluded", c.ID())
		}
		seen[c.ID()] = true
	}
	for _, want := range []string{"gld-decline", "mara_tree", "pdd_tree", "nvdl_tree"} {
		if !seen[want] {
			t.Errorf("expected focused overlay %s in default candidates", want)
		}
	}
}

func TestOverlayCandidates_ExplicitIDs(t *testing.T) {
	primary, ok := strategy.Get("sig-voo-buy-tecl")
	if !ok {
		t.Fatal("sig-voo-buy-tecl not registered")
	}
	cands := OverlayCandidates(primary, OverlayCandidateOptions{
		ExplicitIDs: []string{"gld-decline", "sig-voo-buy-tecl", "no-such-strategy"},
	})
	if len(cands) != 1 || cands[0].ID() != "gld-decline" {
		t.Fatalf("explicit IDs = %v, want [gld-decline]", idsOf(cands))
	}
}

func idsOf(strats []strategy.Strategy) []string {
	out := make([]string, len(strats))
	for i, s := range strats {
		out[i] = s.ID()
	}
	return out
}

func TestExecuteStackEval_RanksProfitableOverlayFirst(t *testing.T) {
	dates := []string{"2026-01-01", "2026-01-02", "2026-01-03", "2026-01-04", "2026-01-05"}
	bars := map[string][]models.Bar{
		"TECL": {
			{Date: "2026-01-01", Open: 50, High: 50, Low: 50, Close: 50},
			{Date: "2026-01-02", Open: 50, High: 50, Low: 50, Close: 50},
			{Date: "2026-01-03", Open: 50, High: 50, Low: 50, Close: 50},
			{Date: "2026-01-04", Open: 50, High: 52, Low: 50, Close: 52},
			{Date: "2026-01-05", Open: 52, High: 54, Low: 52, Close: 54},
		},
		"GLD": {
			{Date: "2026-01-01", Open: 100, High: 100, Low: 100, Close: 100},
			{Date: "2026-01-02", Open: 100, High: 110, Low: 100, Close: 110},
			{Date: "2026-01-03", Open: 110, High: 120, Low: 110, Close: 120},
			{Date: "2026-01-04", Open: 120, High: 130, Low: 120, Close: 130},
			{Date: "2026-01-05", Open: 130, High: 140, Low: 130, Close: 140},
		},
		"LOS": {
			{Date: "2026-01-01", Open: 100, High: 100, Low: 90, Close: 90},
			{Date: "2026-01-02", Open: 90, High: 90, Low: 80, Close: 80},
			{Date: "2026-01-03", Open: 80, High: 80, Low: 70, Close: 70},
			{Date: "2026-01-04", Open: 70, High: 70, Low: 60, Close: 60},
			{Date: "2026-01-05", Open: 60, High: 60, Low: 50, Close: 50},
		},
	}

	primary := &evalMock{
		id:      "primary-tecl",
		name:    "Primary TECL",
		symbols: []string{"TECL"},
		cfg: strategy.StrategyConfig{
			ID: "primary-tecl", AllocationPct: 0.65, PositionCap: 1, HoldingWindow: 8, Benchmark: "TECL",
		},
		// Primary stays flat the whole window so overlays get the idle cash.
	}
	winner := &evalMock{
		id:      "gld-winner",
		name:    "GLD Winner",
		symbols: []string{"GLD"},
		cfg: strategy.StrategyConfig{
			ID: "gld-winner", AllocationPct: 0.50, PositionCap: 1, HoldingWindow: 8, TakeProfitPct: 0.50,
		},
		sigs: []models.Signal{
			{Date: "2026-01-01", Symbol: "GLD", Close: 100, BuyLimit: 100, OrderType: "limit"},
		},
	}
	loser := &evalMock{
		id:      "los-loser",
		name:    "Losing Overlay",
		symbols: []string{"LOS"},
		cfg: strategy.StrategyConfig{
			ID: "los-loser", AllocationPct: 0.50, PositionCap: 1, HoldingWindow: 8, StopLossPct: 0.50,
		},
		sigs: []models.Signal{
			{Date: "2026-01-01", Symbol: "LOS", Close: 100, BuyLimit: 100, OrderType: "limit"},
		},
	}

	tmp := t.TempDir()
	result := ExecuteStackEval(StackEvalOptions{
		Primary:      primary,
		Candidates:   []strategy.Strategy{loser, winner},
		BarsBySymbol: bars,
		SortedDates:  dates,
		Capital:      100000,
		OutDir:       tmp,
		Concurrency:  2,
		StackDepth:   1,
	})

	if len(result.Overlays) != 2 {
		t.Fatalf("overlays = %d, want 2", len(result.Overlays))
	}
	if result.Overlays[0].Secondary.ID() != "gld-winner" {
		t.Errorf("rank 1 = %s, want gld-winner (profitable idle overlay)", result.Overlays[0].Secondary.ID())
	}
	if result.Overlays[0].IncrementalEquity <= 0 {
		t.Errorf("gld-winner incremental equity = %.2f, want > 0", result.Overlays[0].IncrementalEquity)
	}
	if result.Baseline.Idle.DaysFullyIdle == 0 {
		t.Errorf("primary should be fully idle in this fixture, got %+v", result.Baseline.Idle)
	}
}

func TestPickComplementaryOverlays_SkipsSharedSymbols(t *testing.T) {
	a := OverlayEval{
		Secondary:         &evalMock{id: "a"},
		IncrementalEquity: 1000,
		TradedSymbols:     []string{"GLD"},
	}
	b := OverlayEval{
		Secondary:         &evalMock{id: "b"},
		IncrementalEquity: 900,
		TradedSymbols:     []string{"GLD", "SLV"},
	}
	c := OverlayEval{
		Secondary:         &evalMock{id: "c"},
		IncrementalEquity: 800,
		TradedSymbols:     []string{"MARA"},
	}
	picked := pickComplementaryOverlays([]OverlayEval{a, b, c}, 3)
	if len(picked) != 2 {
		t.Fatalf("picked %d, want 2 (a and c; b shares GLD with a)", len(picked))
	}
	if picked[0].Secondary.ID() != "a" || picked[1].Secondary.ID() != "c" {
		t.Errorf("picked %s, %s; want a, c", picked[0].Secondary.ID(), picked[1].Secondary.ID())
	}
}
