// Package rotation_strategy is the rotation family: every session it ranks a
// liquid universe on momentum and holds the top few names, rotating out of a
// name once it falls well down the ranking. The ranking is SQL
// (sql/strategies/rotation_strategy); this package builds each row's config and
// runs it.
package rotation_strategy

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

const pipelineDir = "sql/strategies/rotation_strategy"

var safeSymbol = regexp.MustCompile(`^[A-Z0-9][A-Z0-9.\-]*$`)

// Strategy is one rotation_strategy row.
type Strategy struct {
	Row          refdb.RotationStrategy
	marketDBPath string
	calcDBPath   string
}

func (s *Strategy) ID() string { return s.Row.ID }

// Family is the family name, for logs and listings.
func (s *Strategy) Family() string { return "rotation" }

func (s *Strategy) Name() string { return s.Row.Name }

func (s *Strategy) Description() string {
	desc := fmt.Sprintf("Holds the top %d of %d liquid names by momentum, sold once ranked worse than %d.",
		s.Row.TopK, s.Row.UniverseSize, s.Row.TopK+s.Row.ExitBuffer)
	if s.Row.RegimeSMA > 0 {
		desc += fmt.Sprintf(" Only while %s closes above its %d session average.", s.Row.RegimeSymbol, s.Row.RegimeSMA)
	}
	return desc
}

// candidates is the row's symbol list, upper case. Validate has already refused an unsafe entry.
func (s *Strategy) candidates() []string {
	var out []string
	for sym := range strings.SplitSeq(s.Row.Symbols, ",") {
		if sym = strings.ToUpper(strings.TrimSpace(sym)); sym != "" {
			out = append(out, sym)
		}
	}
	return out
}

// RequiredSymbols is every candidate, so the runner loads bars for any name the
// ranking may pick, plus the regime symbol when the gate is on.
func (s *Strategy) RequiredSymbols() []string {
	out := s.candidates()
	if s.Row.RegimeSMA > 0 {
		out = append(out, strings.ToUpper(strings.TrimSpace(s.Row.RegimeSymbol)))
	}
	return out
}

// weight is the share of equity in one name: an even split of the sleeve across
// top_k names, capped at max_weight_pct.
func (s *Strategy) weight() float64 {
	w := s.Row.AllocationPct / float64(max(s.Row.TopK, 1))
	if s.Row.MaxWeightPct > 0 && w > s.Row.MaxWeightPct {
		w = s.Row.MaxWeightPct
	}
	return w
}

func (s *Strategy) DefaultConfig() strategy.StrategyConfig {
	return strategy.StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "QQQ",
		TargetPct:          999.0,  // the ranking is the only exit
		StopLossPct:        0.0001, // never exit via stop loss
		HoldingWindow:      99999,  // never exit via time limit
		PositionCap:        max(s.Row.TopK, 1),
		AllocationPct:      s.Row.AllocationPct,
		CashYieldAnnual:    s.Row.CashYield,
		SlippagePct:        s.Row.SlippagePct,
		CommissionPerShare: 0.005,
	}
}

func (s *Strategy) PipelineDir() string { return filepath.Join(appenv.Folder(), pipelineDir) }

func (s *Strategy) Validate() error {
	r := s.Row
	switch {
	case r.TopK < 1:
		return fmt.Errorf("top_k must be at least 1, got %d", r.TopK)
	case r.UniverseSize < r.TopK:
		return fmt.Errorf("universe_size %d is smaller than top_k %d", r.UniverseSize, r.TopK)
	case r.ExitBuffer < 0:
		return fmt.Errorf("exit_buffer must not be negative, got %d", r.ExitBuffer)
	case r.MaxWeightPct <= 0 || r.MaxWeightPct > 1:
		return fmt.Errorf("max_weight_pct must be in (0, 1], got %v", r.MaxWeightPct)
	case r.RegimeSMA < 0:
		return fmt.Errorf("regime_sma must not be negative, got %d", r.RegimeSMA)
	case r.RegimeSMA > 0 && !safeSymbol.MatchString(strings.ToUpper(strings.TrimSpace(r.RegimeSymbol))):
		return fmt.Errorf("regime_symbol %q is not a symbol", r.RegimeSymbol)
	}
	syms := s.candidates()
	if len(syms) == 0 {
		return fmt.Errorf("symbols is empty: the runner loads bars only for the listed candidates")
	}
	for _, sym := range syms {
		if !safeSymbol.MatchString(sym) {
			return fmt.Errorf("symbols: %q is not a symbol", sym)
		}
	}
	return strategy.ValidateConfig(s.DefaultConfig())
}

func (s *Strategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

// SQLParams are the values the pipeline's __PLACEHOLDERS__ take for this row.
func (s *Strategy) SQLParams() map[string]string {
	quoted := make([]string, 0, len(s.candidates()))
	for _, sym := range s.candidates() {
		quoted = append(quoted, "'"+sym+"'")
	}
	regimeOn := "0"
	if s.Row.RegimeSMA > 0 {
		regimeOn = "1"
	}
	return map[string]string{
		"USE_LIST":         "1",
		"SYMBOLS":          strings.Join(quoted, ","),
		"UNIVERSE_SIZE":    strconv.Itoa(s.Row.UniverseSize),
		"TOP_K":            strconv.Itoa(s.Row.TopK),
		"EXIT_RANK":        strconv.Itoa(s.Row.TopK + s.Row.ExitBuffer),
		"REGIME_ON":        regimeOn,
		"REGIME_SYMBOL":    strings.ToUpper(strings.TrimSpace(s.Row.RegimeSymbol)),
		"REGIME_SMA":       strconv.Itoa(max(s.Row.RegimeSMA, 1)),
		"REGIME_PRECEDING": strconv.Itoa(max(s.Row.RegimeSMA, 1) - 1),
		"ALLOC":            strconv.FormatFloat(s.weight(), 'f', 6, 64),
	}
}

func (s *Strategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	cfg := s.DefaultConfig()
	cfg.SQLParams = s.SQLParams()
	return strategy.RunPipeline(s.ID(), s.Name(), s.Description(), pipelineDir, cfg, s.marketDBPath, s.calcDBPath, "market", barsBySymbol)
}

func (s *Strategy) ParameterSpace() strategy.ParameterSpace {
	cfg := s.DefaultConfig()
	return strategy.ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      s.RequiredSymbols(),
		Direction:    "long",
		FixedEntries: true,
		SignalDays:   []int{1},
		HoldDays:     []int{cfg.HoldingWindow},
		TakeProfits:  []float64{0},
		StopLosses:   []float64{0},
		Regimes:      []string{"All Regimes"},
		Allocations:  []float64{s.Row.AllocationPct},
		CashYield:    s.Row.CashYield,
		Baseline: strategy.BaselineParams{
			HoldDays:   cfg.HoldingWindow,
			Allocation: s.Row.AllocationPct,
		},
	}
}

// NewFamily is the rotation family over the reference database path returns.
func NewFamily(path func() string) *strategy.RowFamily {
	return &strategy.RowFamily{
		FamilyName: "rotation",
		Table:      "rotation_strategy",
		Path:       path,
		Build: func(db *sqlx.DB, id string) (strategy.Strategy, bool, error) {
			row, ok, err := refdb.RotationStrategyByID(db, id)
			if err != nil || !ok {
				return nil, false, err
			}
			s := &Strategy{Row: row}
			if err := s.Validate(); err != nil {
				return nil, false, err
			}
			return s, true, nil
		},
	}
}

// Register adds the rotation family reading the default reference database.
func Register() { RegisterFrom(refdb.DefaultPath) }

// RegisterFrom adds the rotation family reading the reference database at path.
func RegisterFrom(path string) {
	strategy.RegisterFamily(NewFamily(func() string { return path }))
}
