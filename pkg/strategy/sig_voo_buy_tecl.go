package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// SigVooBuyTecl buys TECL only (no SPXU trades):
//
//   - LONG TECL when VOO closes down 3 consecutive days
//     (+5% take-profit, 0% stop-loss, 8-day max hold, 65% allocation)
//
// The SPXU short leg lives on in voo-tecl-spxu-combo.
//
// T-bill yield on idle cash is configured via DefaultConfig().CashYieldAnnual
// and applied by the PortfolioSimulator.
//
// Formerly VOOTECLCombo / ID "voo-tecl-combo" — renamed to SigVooBuyTecl / "sig-voo-buy-tecl".
type SigVooBuyTecl struct{
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

// NewSigVooBuyTecl constructs and auto-registers the strategy.
func NewSigVooBuyTecl() *SigVooBuyTecl {
	s := &SigVooBuyTecl{
		DeclineDays:        3,
		TakeProfitPct:      0.05,
		StopLossPct:        0.00,
		HoldingWindow:      8,
		ShortTakeProfitPct: 0.06,
		ShortStopLossPct:   0.95,
		ShortHoldingWindow: 2,
	}
	Register(s)
	RegisterAlias("sig-voo-buy-tecl", s)
	RegisterAlias("sigvoobuytecl", s)
	RegisterAlias("SigVooBuyTecl", s)
	return s
}

func (s *SigVooBuyTecl) ID() string { return "sig-voo-buy-tecl" }

func (s *SigVooBuyTecl) Name() string { return "VOO→TECL" }

func (s *SigVooBuyTecl) Description() string {
	return "Long TECL on 3-consecutive VOO down-closes (+5% TP / 8d hold). " +
		"TECL only, no SPXU trades. 65% allocation per trade. 4.5% T-bill yield on idle cash."
}

func (s *SigVooBuyTecl) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

// RequiredSymbols returns the specific market symbols required by this combo strategy.
func (s *SigVooBuyTecl) RequiredSymbols() []string {
	return []string{"VOO", "TECL"}
}

// DefaultConfig returns the canonical VOO-TECL combo parameters.
func (s *SigVooBuyTecl) DefaultConfig() StrategyConfig {
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

// MinHistoryBars implements MinHistoryProvider: the streak window needs
// DeclineDays+1 closes; +2 is slack for a partial/holiday bar.
func (s *SigVooBuyTecl) MinHistoryBars() int { return s.DefaultConfig().DeclineDays + 3 }

// SetDeclineDays implements DeclineDaysConfigurable.
func (s *SigVooBuyTecl) SetDeclineDays(n int) { s.DeclineDays = n }

func (s *SigVooBuyTecl) SetDatabases(marketDBPath, calcDBPath string) {
	if sqlStrat, exists := Get("sig_voo_buy_tecl-sql"); exists {
		sqlStrat.SetDatabases(marketDBPath, calcDBPath)
	}
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *SigVooBuyTecl) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// Run the canonical SQLite pipeline with this instance's own config, so a
	// non-default s.DeclineDays actually takes effect. Built fresh (not the
	// registered "sig_voo_buy_tecl-sql" singleton, which always uses its own
	// AutoRegisterSQLStrategies default) but reusing that singleton's already
	// -resolved pipeline dir when it's registered, since the literal
	// "sql/strategies/sig_voo_buy_tecl" only resolves correctly when the
	// process's working directory is the repo root.
	dir := "sql/strategies/sig_voo_buy_tecl"
	if sqlStrat, exists := Get("sig_voo_buy_tecl-sql"); exists {
		if sp, ok := sqlStrat.(*SQLPipelineStrategy); ok {
			dir = sp.PipelineDir()
		}
	}
	pipe := NewSQLPipelineStrategy("sig_voo_buy_tecl-pipeline", s.Name(), s.Description(), dir, s.DefaultConfig())
	pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
	return pipe.GenerateSignals(barsBySymbol)
}

// ParameterSpace returns the tailored parameter search space centered around VOO-TECL combo defaults.
func (s *SigVooBuyTecl) ParameterSpace() ParameterSpace {
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
	NewSigVooBuyTecl()
}
