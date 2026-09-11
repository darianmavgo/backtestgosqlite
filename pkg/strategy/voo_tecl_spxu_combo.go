package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// VOOTECLSPXUCombo implements the All-Weather Dual Combo strategy:
//
//   - LONG TECL when VOO closes down 3 consecutive days AND TECL closes down 3 consecutive days
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
type VOOTECLSPXUCombo struct{
	marketDBPath string
	calcDBPath   string
}

// NewVOOTECLSPXUCombo constructs and auto-registers the strategy.
func NewVOOTECLSPXUCombo() *VOOTECLSPXUCombo {
	s := &VOOTECLSPXUCombo{}
	Register(s)
	RegisterAlias("voo-tecl-spxu-combo", s)
	RegisterAlias("vooteclspxucombo", s)
	RegisterAlias("VOOTECLSPXUCombo", s)
	RegisterAlias("voo_tecl_spxu_combo", s)
	return s
}

func (s *VOOTECLSPXUCombo) ID() string { return "voo-tecl-spxu-combo" }

func (s *VOOTECLSPXUCombo) Name() string {
	return "VOO→TECL/SPXU All-Weather Combo (VOO+TECL 3-day decline)"
}

func (s *VOOTECLSPXUCombo) Description() string {
	return "Long TECL on 3-consecutive VOO down-closes AND 3-consecutive TECL down-closes (+5% TP / 8d hold) " +
		"combined with Short SPXU on 3-consecutive VOO up-closes in bear markets " +
		"(VOO < SMA200, +6% TP / -5% SL / 2d hold). Long takes priority on same-day conflicts. " +
		"65% allocation per trade. 4.5% T-bill yield on idle cash."
}

func (s *VOOTECLSPXUCombo) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

// RequiredSymbols returns the specific market symbols required by this combo strategy.
func (s *VOOTECLSPXUCombo) RequiredSymbols() []string {
	return []string{"VOO", "TECL", "SPXU"}
}

// DefaultConfig returns the canonical VOO-TECL-SPXU combo parameters.
func (s *VOOTECLSPXUCombo) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "VOO",
		AllocationPct:      0.65,
		TakeProfitPct:      0.05, // Long leg default; short overrides per-signal
		StopLossPct:        0.00, // Long leg: no stop-loss
		HoldingWindow:      8,    // Long leg default; short overrides per-signal (2)
		PositionCap:        1,    // One open position at a time
		CashYieldAnnual:    0.045,
		SlippagePct:        0.0,
		CommissionPerShare: 0.0,
	}
}

func (s *VOOTECLSPXUCombo) SetDatabases(marketDBPath, calcDBPath string) {
	if sqlStrat, exists := Get("voo_tecl_spxu_combo-sql"); exists {
		sqlStrat.SetDatabases(marketDBPath, calcDBPath)
	}
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *VOOTECLSPXUCombo) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// Delegate signal generation directly to the canonical SQLite pipeline
	if sqlStrat, exists := Get("voo_tecl_spxu_combo-sql"); exists {
		return sqlStrat.GenerateSignals(barsBySymbol)
	}
	pipe := NewSQLPipelineStrategy("voo_tecl_spxu_combo-pipeline", s.Name(), s.Description(), "sql/strategies/voo_tecl_spxu_combo", s.DefaultConfig())
	pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
	return pipe.GenerateSignals(barsBySymbol)
}

func init() {
	NewVOOTECLSPXUCombo()
}
