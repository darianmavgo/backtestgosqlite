package strategy

import (
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
)

// spxuCandidates returns the gridsearch trade-ticker universe from the
// reference DB's "sweep" list, falling back to SPXU alone if unavailable.
func spxuCandidates() []string {
	if db, err := refdb.OpenExisting(refdb.DefaultPath); err == nil && db != nil {
		defer db.Close()
		if syms, err := refdb.Universe(db, refdb.ListSweep); err == nil && len(syms) > 0 {
			return syms
		}
	}
	return []string{"SPXU"}
}

// SigVooBuySpxu buys SPXU (3x inverse S&P 500) after VOO closes up N
// consecutive days, exiting on a take-profit, stop-loss, or max hold. It is
// the standalone counterpart of sig-voo-buy-tecl (which buys TECL after VOO
// closes down N days) and is not regime filtered.
type SigVooBuySpxu struct {
	// RallyDays is the number of consecutive VOO up-closes required to enter
	// (default 3). TakeProfitPct is a fractional offset (0.06 = +6%),
	// StopLossPct a direct multiplier (0.95 = -5%), HoldingWindow the max
	// hold in days. All flow into DefaultConfig() and from there into the
	// SQL pipeline placeholders.
	RallyDays     int
	TakeProfitPct float64
	StopLossPct   float64
	HoldingWindow int
	marketDBPath  string
	calcDBPath    string
}

// NewSigVooBuySpxu constructs and auto-registers the strategy.
func NewSigVooBuySpxu() *SigVooBuySpxu {
	s := &SigVooBuySpxu{
		RallyDays:     3,
		TakeProfitPct: 0.06,
		StopLossPct:   0.95,
		HoldingWindow: 2,
	}
	Register(s)
	RegisterAlias("sig-voo-buy-spxu", s)
	RegisterAlias("sigvoobuyspxu", s)
	RegisterAlias("SigVooBuySpxu", s)
	return s
}

func (s *SigVooBuySpxu) ID() string { return "sig-voo-buy-spxu" }

func (s *SigVooBuySpxu) Name() string { return "VOO→SPXU" }

func (s *SigVooBuySpxu) Description() string {
	return "Buy SPXU on 3-consecutive VOO up-closes (+6% TP / -5% SL / 2d hold). " +
		"SPXU only, all regimes. 65% allocation per trade. 4.5% T-bill yield on idle cash."
}

func (s *SigVooBuySpxu) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

// RequiredSymbols returns the market symbols this strategy needs.
func (s *SigVooBuySpxu) RequiredSymbols() []string {
	return []string{"VOO", "SPXU"}
}

// MinHistoryBars implements MinHistoryProvider: RallyDays+1 closes, +2 slack.
func (s *SigVooBuySpxu) MinHistoryBars() int { return s.DefaultConfig().DeclineDays + 3 }

// DefaultConfig returns the canonical parameters. The pipeline's
// __DECLINE_DAYS__ placeholder carries the rally-streak length.
func (s *SigVooBuySpxu) DefaultConfig() StrategyConfig {
	rallyDays := s.RallyDays
	if rallyDays <= 0 {
		rallyDays = 3
	}
	takeProfitPct := s.TakeProfitPct
	if takeProfitPct <= 0 {
		takeProfitPct = 0.06
	}
	stopLossPct := s.StopLossPct
	if stopLossPct <= 0 {
		stopLossPct = 0.95
	}
	holdingWindow := s.HoldingWindow
	if holdingWindow <= 0 {
		holdingWindow = 2
	}
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "VOO",
		AllocationPct:      0.65,
		TargetPct:          1.0 + takeProfitPct,
		TakeProfitPct:      takeProfitPct,
		StopLossPct:        stopLossPct,
		HoldingWindow:      holdingWindow,
		PositionCap:        1,
		CashYieldAnnual:    0.045,
		// SPXU (ProShares UltraPro Short S&P500, -3x) has meaningfully lower
		// volume/AUM than the long-leveraged index ETFs and an inverse
		// product's quotes widen further in the down-market conditions this
		// strategy specifically buys into -- 15bp reflects that. Commission
		// matches Schwab's real $0 online equity/ETF commission; the token
		// $0.0001/share stands in for SEC/FINRA TAF pass-through fees.
		SlippagePct:        0.0015,
		CommissionPerShare: 0.0001,
		DeclineDays:        rallyDays,
	}
}

// SetDeclineDays implements DeclineDaysConfigurable (the rally-streak length).
func (s *SigVooBuySpxu) SetDeclineDays(n int) { s.RallyDays = n }

func (s *SigVooBuySpxu) SetDatabases(marketDBPath, calcDBPath string) {
	if sqlStrat, exists := Get("sig_voo_buy_spxu-sql"); exists {
		sqlStrat.SetDatabases(marketDBPath, calcDBPath)
	}
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *SigVooBuySpxu) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	// Same approach as SigVooBuyTecl: a fresh pipeline with this instance's
	// own config, reusing the registered SQL singleton's resolved directory.
	dir := "sql/strategies/sig_voo_buy_spxu"
	if sqlStrat, exists := Get("sig_voo_buy_spxu-sql"); exists {
		if sp, ok := sqlStrat.(*SQLPipelineStrategy); ok {
			dir = sp.PipelineDir()
		}
	}
	pipe := NewSQLPipelineStrategy("sig_voo_buy_spxu-pipeline", s.Name(), s.Description(), dir, s.DefaultConfig())
	pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
	return pipe.GenerateSignals(barsBySymbol)
}

// ParameterSpace returns the gridsearch space: which ticker to buy after a
// VOO rally (SPXU is just the current hardcoded default).
func (s *SigVooBuySpxu) ParameterSpace() ParameterSpace {
	cfg := s.DefaultConfig()
	return ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      spxuCandidates(), // sweeps the buy ticker; the live strategy is still SPXU
		SignalSymbol: "VOO",
		Direction:    "rally",
		SignalDays:   []int{2, 3, 4, 5},
		HoldDays:     []int{1, 2, 3, 4, 6, 8},
		TakeProfits:  []float64{0.03, 0.05, 0.06, 0.08, 0.10},
		StopLosses:   []float64{0.0, 0.03, 0.05, 0.08, 0.10},
		Regimes:      []string{"All Regimes", "VOO<SMA200"},
		Allocations:  []float64{cfg.AllocationPct},
		CashYield:    cfg.CashYieldAnnual,
		Baseline: BaselineParams{
			SignalDays: cfg.DeclineDays,
			HoldDays:   cfg.HoldingWindow,
			TakeProfit: cfg.TakeProfitPct,
			StopLoss:   stopLossOffset(cfg.StopLossPct),
			Allocation: cfg.AllocationPct,
			Regime:     "All Regimes",
		},
	}
}

func init() {
	NewSigVooBuySpxu()
}
