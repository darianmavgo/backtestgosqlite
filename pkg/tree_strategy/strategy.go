package tree_strategy

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

const pipelineDir = "sql/strategies/tree_strategy"

type Strategy struct {
	Row          refdb.TreeStrategy
	marketDBPath string
	calcDBPath   string
	PipelineDir  string
}

func (s *Strategy) ID() string { return s.Row.ID }

func (s *Strategy) Name() string { return s.Row.Name }

func (s *Strategy) Description() string {
	return fmt.Sprintf("Decision Tree %s on %s. Coil <= %.2f, Bounce [%.2f, %.2f]. Hold %dd, TP +%.1f%%, SL -%.1f%%.",
		s.Row.Name, s.Row.SignalSymbol, s.Row.CoilRangeMax, s.Row.SMABounceMin, s.Row.SMABounceMax,
		s.Row.HoldDays, s.Row.TakeProfitPct*100, s.Row.StopLossPct*100)
}

func (s *Strategy) Validate() error { return ValidateRow(s.Row) }

func (s *Strategy) RequiredSymbols() []string {
	return []string{s.Row.SignalSymbol}
}

func (s *Strategy) MinHistoryBars() int {
	return 203 // 200 SMA + some slack
}

func (s *Strategy) DefaultConfig() strategy.StrategyConfig {
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
		Benchmark:         s.Row.SignalSymbol,
		TradeSymbol:       s.Row.TradeSymbol,
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
		
		// Tree-specific parameters mapped to the generic config
		TreeCoilMax:       s.Row.CoilRangeMax,
		TreeSMAMin:        s.Row.SMABounceMin,
		TreeSMAMax:        s.Row.SMABounceMax,
	}
}

func (s *Strategy) ParameterSpace() strategy.ParameterSpace {
	cfg := s.DefaultConfig()
	return strategy.ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      []string{cfg.TradeSymbol},
		SignalSymbol: cfg.Benchmark,
		Direction:    s.Row.Direction,
		HoldDays:     []int{s.Row.HoldDays},
		TakeProfits:  []float64{s.Row.TakeProfitPct},
		StopLosses:   []float64{s.Row.StopLossPct},
		Allocations:  []float64{s.Row.AllocationPct},
		CashYield:    s.Row.CashYield,
		Baseline: strategy.BaselineParams{
			HoldDays:   s.Row.HoldDays,
			TakeProfit: s.Row.TakeProfitPct,
			StopLoss:   s.Row.StopLossPct,
			Allocation: s.Row.AllocationPct,
		},
	}
}

func (s *Strategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *Strategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	if s.marketDBPath != "" && s.calcDBPath != "" {
		dir := s.PipelineDir
		if dir == "" {
			dir = pipelineDir
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
	
	// Fallback to empty if DB not supplied (decision tree requires SQL for SMA/ATR)
	return nil
}

func ValidateRow(row refdb.TreeStrategy) error {
	if strings.TrimSpace(row.ID) == "" {
		return fmt.Errorf("id is empty")
	}
	if row.HoldDays < 1 {
		return fmt.Errorf("hold_days %d", row.HoldDays)
	}
	return nil
}

func Register() {
	RegisterFrom(refdb.DefaultPath)
}

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
	rows, err := refdb.TreeStrategies(db)
	if err != nil {
		return
	}
	for _, row := range rows {
		if err := ValidateRow(row); err != nil {
			log.Printf("tree_strategy: skip %s: %v", row.ID, err)
			continue
		}
		if existing, ok := strategy.Get(row.ID); ok {
			if _, is := existing.(*Strategy); !is {
				log.Printf("tree_strategy: skip %s: id already used by %T", row.ID, existing)
				continue
			}
		}
		strategy.Register(&Strategy{Row: row})
	}
}
