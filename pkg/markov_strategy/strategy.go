package markov_strategy

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

const pipelineDir = "sql/strategies/markov_model"

// Strategy is one markov_strategy row, runnable by backtest, gridsearch,
// scoreboard, livescan, and strateval.
type Strategy struct {
	Row          refdb.MarkovStrategy
	marketDBPath string
	calcDBPath   string
	PipelineDir  string
}

// ID is the table's id column.
func (s *Strategy) ID() string { return s.Row.ID }

// Name is the table's name column.
func (s *Strategy) Name() string { return s.Row.Name }

// Description summarizes the strategy.
func (s *Strategy) Description() string {
	return fmt.Sprintf("Trade %s %s when %s enters %s state. Hold %dd, TP +%.1f%%, SL -%.1f%%.",
		s.Row.TradeSymbol, s.Row.Direction, s.Row.SignalSymbol, s.Row.TargetState,
		s.Row.HoldDays, s.Row.TakeProfitPct*100, s.Row.StopLossPct*100)
}

// Validate checks the row.
func (s *Strategy) Validate() error { return ValidateRow(s.Row) }

// RequiredSymbols is the signal symbol and the traded symbol.
func (s *Strategy) RequiredSymbols() []string {
	sig := strings.ToUpper(strings.TrimSpace(s.Row.SignalSymbol))
	tr := strings.ToUpper(strings.TrimSpace(s.Row.TradeSymbol))
	if sig == tr {
		return []string{sig}
	}
	return []string{sig, tr}
}

// MinHistoryBars is 100 for the Markov Model lookback (assuming ~100 is enough to establish initial states, can be tuned).
func (s *Strategy) MinHistoryBars() int {
	return 100
}

// DefaultConfig maps every execution column.
func (s *Strategy) DefaultConfig() strategy.StrategyConfig {
	sig := strings.ToUpper(strings.TrimSpace(s.Row.SignalSymbol))
	tr := strings.ToUpper(strings.TrimSpace(s.Row.TradeSymbol))
	tp := s.Row.TakeProfitPct
	var target, stop float64
	if tp > 0 {
		target = 1 + tp
	}
	if s.Row.StopLossPct > 0 {
		stop = 1 - s.Row.StopLossPct
	}
	return strategy.StrategyConfig{
		ID:                s.ID(),
		Name:              s.Name(),
		Description:       s.Description(),
		Benchmark:         sig,
		TradeSymbol:       tr,
		StreakDirection:   s.Row.Direction,
		Regime:            s.Row.TargetState,
		Timeframe:         "1d",
		PositionSizing:    "fixed_pct",
		AllocationPct:     s.Row.AllocationPct,
		TargetPct:         target,
		TakeProfitPct:     tp,
		StopLossPct:       stop,
		HoldingWindow:     s.Row.HoldDays,
		PositionCap:       1,
		CashYieldAnnual:   s.Row.CashYield,
		SlippagePct:       s.Row.SlippagePct,
		NextDayLimitEntry: s.Row.NextDayLimit != 0,
	}
}

// ParameterSpace returns the search space based on the row.
func (s *Strategy) ParameterSpace() strategy.ParameterSpace {
	cfg := s.DefaultConfig()
	return strategy.ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      []string{cfg.TradeSymbol},
		SignalSymbol: cfg.Benchmark,
		Direction:    s.Row.Direction,
		SignalDays:   []int{0},
		HoldDays:     []int{s.Row.HoldDays},
		TakeProfits:  []float64{s.Row.TakeProfitPct},
		StopLosses:   []float64{s.Row.StopLossPct},
		Regimes:      []string{s.Row.TargetState},
		Allocations:  []float64{s.Row.AllocationPct},
		CashYield:    s.Row.CashYield,
		Baseline: strategy.BaselineParams{
			SignalDays: 0,
			HoldDays:   s.Row.HoldDays,
			TakeProfit: s.Row.TakeProfitPct,
			StopLoss:   s.Row.StopLossPct,
			Allocation: s.Row.AllocationPct,
			Regime:     s.Row.TargetState,
		},
	}
}

// SetDatabases stores the market and calc database paths.
func (s *Strategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

// GenerateSignals runs the shared SQL pipeline.
func (s *Strategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	if s.marketDBPath != "" && s.calcDBPath != "" {
		dir := s.PipelineDir
		if dir == "" {
			if strings.Contains(s.ID(), "hmm") {
				dir = "sql/strategies/markov_hmm"
			} else {
				dir = pipelineDir
			}
		}
		pipe := strategy.NewSQLPipeline(s.ID()+"-run", s.Name(), s.Description(), dir, s.DefaultConfig())
		pipe.SetDatabases(s.marketDBPath, s.calcDBPath)
		sigs := pipe.GenerateSignals(barsBySymbol)
		for i := range sigs {
			sigs[i].StrategyID = s.ID()
			if sigs[i].OrderType == "" {
				sigs[i].OrderType = "limit"
			}
		}
		return sigs
	}
	return nil // Go fallback not implemented for MarkovModel
}

// ValidateRow reports why a markov_strategy row cannot be registered.
func ValidateRow(row refdb.MarkovStrategy) error {
	if strings.TrimSpace(row.ID) == "" {
		return fmt.Errorf("id is empty")
	}
	if reservedID(row.ID) {
		return fmt.Errorf("id %q is reserved", row.ID)
	}
	if strings.TrimSpace(row.SignalSymbol) == "" {
		return fmt.Errorf("signal_symbol is empty")
	}
	if strings.TrimSpace(row.TradeSymbol) == "" {
		return fmt.Errorf("trade_symbol is empty")
	}
	if row.Direction != "long" && row.Direction != "short" {
		return fmt.Errorf("direction %q must be long or short", row.Direction)
	}
	if strings.TrimSpace(row.TargetState) == "" {
		return fmt.Errorf("target_state is empty")
	}
	if row.HoldDays < 1 {
		return fmt.Errorf("hold_days %d", row.HoldDays)
	}
	if row.TakeProfitPct < 0 {
		return fmt.Errorf("take_profit_pct %v", row.TakeProfitPct)
	}
	if row.StopLossPct < 0 || row.StopLossPct >= 1 {
		return fmt.Errorf("stop_loss_pct %v", row.StopLossPct)
	}
	if row.AllocationPct <= 0 || row.AllocationPct > 1 {
		return fmt.Errorf("allocation_pct %v", row.AllocationPct)
	}
	if row.CashYield < 0 {
		return fmt.Errorf("cash_yield %v", row.CashYield)
	}
	if row.SlippagePct < 0 {
		return fmt.Errorf("slippage_pct %v", row.SlippagePct)
	}
	if row.NextDayLimit != 0 && row.NextDayLimit != 1 {
		return fmt.Errorf("next_day_limit %d", row.NextDayLimit)
	}
	return nil
}

func reservedID(id string) bool {
	n := strings.ToLower(id)
	n = strings.ReplaceAll(n, "-", "")
	n = strings.ReplaceAll(n, "_", "")
	n = strings.ReplaceAll(n, " ", "")
	return n == "markovstrategy"
}

// Register loads markov_strategy from the default reference DB and registers
// each valid row.
func Register() {
	RegisterFrom(refdb.DefaultPath)
}

// RegisterFrom loads markov_strategy from path.
func RegisterFrom(path string) {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() == 0 {
		return
	}
	db, err := refdb.Open(path)
	if err != nil || db == nil {
		return
	}
	defer db.Close()
	rows, err := refdb.MarkovStrategies(db)
	if err != nil {
		return
	}
	for _, row := range rows {
		if err := ValidateRow(row); err != nil {
			log.Printf("markov_strategy: skip %s: %v", row.ID, err)
			continue
		}
		if existing, ok := strategy.Get(row.ID); ok {
			if _, is := existing.(*Strategy); !is {
				log.Printf("markov_strategy: skip %s: id already used by %T", row.ID, existing)
				continue
			}
		}
		strategy.Register(&Strategy{Row: row})
	}
}
