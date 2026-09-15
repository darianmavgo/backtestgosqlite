package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// VOOTECLCombo implements the All-Weather Dual Combo strategy:
//
//   - LONG TECL when VOO closes down 3 consecutive days
//     (+5% take-profit, 0% stop-loss, 8-day max hold, 65% allocation)
//
//   - SHORT SPXU when VOO closes up 3 consecutive days AND VOO is below its SMA200
//     (+6% take-profit, -5% stop-loss, 2-day max hold, 65% allocation)
//
// Priority rule: if both signals fire on the same day, LONG takes priority.
// Regime filtering (VOO < SMA200) is performed inside GenerateSignals — the
// engine receives only pre-filtered signals and does not know about regimes.
//
// T-bill yield on idle cash is configured via DefaultConfig().CashYieldAnnual
// and applied by the PortfolioSimulator.
type VOOTECLCombo struct{
	// DeclineDays is the number of consecutive VOO down-closes (long leg) or
	// up-closes (short leg) required to enter (default: 3). TakeProfitPct/
	// StopLossPct/HoldingWindow are the long (TECL) leg's exit rules (default
	// +5% / no stop / 8d); ShortTakeProfitPct/ShortStopLossPct/
	// ShortHoldingWindow are the short (SPXU) leg's (default +6% / -5% / 2d).
	// All are the single source of truth for their values — flowing into
	// DefaultConfig(), which SQLPipelineStrategy substitutes into the SQL
	// pipeline — instead of being separately hardcoded literals that
	// DefaultConfig() had no actual effect on.
	DeclineDays        int
	TakeProfitPct      float64
	StopLossPct        float64
	HoldingWindow      int
	ShortTakeProfitPct float64
	ShortStopLossPct   float64
	ShortHoldingWindow int
	marketDBPath       string
	calcDBPath         string
}

// NewVOOTECLCombo constructs and auto-registers the strategy.
func NewVOOTECLCombo() *VOOTECLCombo {
	s := &VOOTECLCombo{
		DeclineDays:        3,
		TakeProfitPct:      0.05,
		StopLossPct:        0.00,
		HoldingWindow:      8,
		ShortTakeProfitPct: 0.06,
		ShortStopLossPct:   0.95,
		ShortHoldingWindow: 2,
	}
	Register(s)
	RegisterAlias("voo-tecl-combo", s)
	RegisterAlias("vooteclcombo", s)
	RegisterAlias("VOOTECLCombo", s)
	return s
}

func (s *VOOTECLCombo) ID() string { return "voo-tecl-combo" }

func (s *VOOTECLCombo) Name() string { return "VOO→TECL All-Weather Combo" }

func (s *VOOTECLCombo) Description() string {
	return "Long TECL on 3-consecutive VOO down-closes (+5% TP / 8d hold) " +
		"combined with Short SPXU on 3-consecutive VOO up-closes in bear markets " +
		"(VOO < SMA200, +6% TP / -5% SL / 2d hold). Long takes priority on same-day conflicts. " +
		"65% allocation per trade. 4.5% T-bill yield on idle cash."
}

func (s *VOOTECLCombo) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

// RequiredSymbols returns the specific market symbols required by this combo strategy.
func (s *VOOTECLCombo) RequiredSymbols() []string {
	return []string{"VOO", "TECL", "SPXU"}
}

// DefaultConfig returns the canonical VOO-TECL combo parameters.
func (s *VOOTECLCombo) DefaultConfig() StrategyConfig {
	declineDays := s.DeclineDays
	if declineDays <= 0 {
		declineDays = 3
	}
	takeProfitPct := s.TakeProfitPct
	if takeProfitPct <= 0 {
		takeProfitPct = 0.05
	}
	holdingWindow := s.HoldingWindow
	if holdingWindow <= 0 {
		holdingWindow = 8
	}
	shortTakeProfitPct := s.ShortTakeProfitPct
	if shortTakeProfitPct <= 0 {
		shortTakeProfitPct = 0.06
	}
	shortStopLossPct := s.ShortStopLossPct
	if shortStopLossPct <= 0 {
		shortStopLossPct = 0.95
	}
	shortHoldingWindow := s.ShortHoldingWindow
	if shortHoldingWindow <= 0 {
		shortHoldingWindow = 2
	}
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "VOO",
		AllocationPct:      0.65,
		TargetPct:          1.0 + takeProfitPct, // legacy-multiplier mirror of TakeProfitPct
		TakeProfitPct:      takeProfitPct,       // Long leg; short leg uses ShortTakeProfitPct
		StopLossPct:        s.StopLossPct,       // Long leg: 0.00 means no stop-loss (intentional, not "unset")
		HoldingWindow:      holdingWindow,       // Long leg; short leg uses ShortHoldingWindow
		PositionCap:        1,                   // One open position at a time
		CashYieldAnnual:    0.045,
		SlippagePct:        0.0,
		CommissionPerShare: 0.0,
		DeclineDays:        declineDays,
		ShortTakeProfitPct: shortTakeProfitPct,
		ShortStopLossPct:   shortStopLossPct,
		ShortHoldingWindow: shortHoldingWindow,
	}
}

func (s *VOOTECLCombo) SetDatabases(marketDBPath, calcDBPath string) {
	if sqlStrat, exists := Get("voo_tecl_combo-sql"); exists {
		sqlStrat.SetDatabases(marketDBPath, calcDBPath)
	}
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *VOOTECLCombo) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// Run the canonical SQLite pipeline with this instance's own config, so a
	// non-default s.DeclineDays actually takes effect. Built fresh (not the
	// registered "voo_tecl_combo-sql" singleton, which always uses its own
	// AutoRegisterSQLStrategies default) but reusing that singleton's already
	// -resolved pipeline dir when it's registered, since the literal
	// "sql/strategies/voo_tecl_combo" only resolves correctly when the
	// process's working directory is the repo root.
	dir := "sql/strategies/voo_tecl_combo"
	if sqlStrat, exists := Get("voo_tecl_combo-sql"); exists {
		if sp, ok := sqlStrat.(*SQLPipelineStrategy); ok {
			dir = sp.PipelineDir()
		}
	}
	pipe := NewSQLPipelineStrategy("voo_tecl_combo-pipeline", s.Name(), s.Description(), dir, s.DefaultConfig())
	pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
	return pipe.GenerateSignals(barsBySymbol)
}

// ParameterSpace returns the tailored parameter search space centered around VOO-TECL combo defaults.
func (s *VOOTECLCombo) ParameterSpace() ParameterSpace {
	cfg := s.DefaultConfig()
	return ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      []string{"TECL"},
		SignalSymbol: "VOO",
		Direction:    "drop",
		SignalDays:   []int{2, 3, 4, 5},
		HoldDays:     []int{2, 4, 6, 8, 10, 12, 14},
		TakeProfits:  []float64{0.03, 0.05, 0.07, 0.10},
		StopLosses:   []float64{0.0, 0.05, 0.10, 0.15, 0.20},
		Regimes:      []string{"All Regimes", "VOO>=SMA200"},
		Allocations:  []float64{cfg.AllocationPct},
		CashYield:    cfg.CashYieldAnnual,
		Baseline: BaselineParams{
			SignalDays: cfg.DeclineDays,
			HoldDays:   cfg.HoldingWindow,
			TakeProfit: cfg.TakeProfitPct,
			StopLoss:   stopLossOffset(cfg.StopLossPct), // gridsearch's grid is offsets; cfg.StopLossPct is a multiplier
			Allocation: cfg.AllocationPct,
			Regime:     "All Regimes",
		},
	}
}

func init() {
	NewVOOTECLCombo()
}
