//go:build ignore

package strategy

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/ryanbressler/CloudForest"
)

// VOOTreeStrategy fits a single depth-3 CloudForest tree on VOO using only
// open, close, and volume-derived features (no high/low, no other symbols).
// The tree is trained exclusively on 2021–2022 labeled days and emits BUY
// signals only on 2020 and 2023–2026 — a reverse-chronological holdout plus a
// true forward holdout. 2020 could not have been traded with this exact tree
// (the fit uses later data); 2023–2026 is the walk-forward test.
type VOOTreeStrategy struct {
	marketDBPath string
	calcDBPath   string
}

func NewVOOTreeStrategy() *VOOTreeStrategy {
	s := &VOOTreeStrategy{}
	Register(s)
	RegisterAlias("voo-tree", s)
	RegisterAlias("voo_tree", s)
	RegisterAlias("VOOTree", s)
	return s
}

func (s *VOOTreeStrategy) ID() string { return "voo_tree" }

func (s *VOOTreeStrategy) Name() string {
	return "VOO Tree (Open/Close/Volume, train 2021–2022)"
}

func (s *VOOTreeStrategy) Description() string {
	return fmt.Sprintf(
		"Single depth-%d CloudForest tree on VOO open/close/volume features only. "+
			"Trained on 2021–2022 dual-barrier labels (+%.0f%% / -%.0f%% / %dd). "+
			"Signals emitted on 2020 and 2023–2026 (not on the training years).",
		vooTreeMaxDepth, vooTreeTargetPct*100, vooTreeStopPct*100, vooTreeHoldDays)
}

func (s *VOOTreeStrategy) Validate() error { return ValidateConfig(s.DefaultConfig()) }

func (s *VOOTreeStrategy) RequiredSymbols() []string { return []string{"VOO"} }

func (s *VOOTreeStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "VOO",
		Timeframe:          "1d",
		PositionSizing:     "fixed_pct",
		AllocationPct:      0.65,
		TargetPct:          1.0 + vooTreeTargetPct,
		TakeProfitPct:      vooTreeTargetPct,
		StopLossPct:        1.0 - vooTreeStopPct,
		HoldingWindow:      vooTreeHoldDays,
		PositionCap:        1,
		CashYieldAnnual:    0.045,
		SlippagePct:        0.0005,
		CommissionPerShare: 0.0001,
	}
}

func (s *VOOTreeStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

const (
	vooTreeTargetPct = 0.03
	vooTreeStopPct   = 0.02
	vooTreeHoldDays  = 15
	vooTreeMaxDepth  = 3
	vooTreeLeafSize  = 15
	vooTreeMinTrain  = 100
	vooTreeWarmup    = 200 // SMA200 of close
)

func vooTreeDateYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return y
}

func vooTreeIsTrainYear(y int) bool { return y == 2021 || y == 2022 }

func vooTreeIsEvalYear(y int) bool {
	switch y {
	case 2020, 2023, 2024, 2025, 2026:
		return true
	}
	return false
}

type vooOCVSample struct {
	Date            string
	Open            float64
	Close           float64
	Volume          int64
	High            float64
	Low             float64
	Idx             int
	Ret1d           float64
	Ret3d           float64
	Ret5d           float64
	Ret10d          float64
	Intraday        float64
	Gap             float64
	VolVs5          float64
	VolVs20         float64
	VolChg1d        float64
	CloseVsSMA20    float64
	CloseVsSMA50    float64
	CloseVsSMA200   float64
	ConsecutiveDown float64
}

func computeVOOOCVSamples(bars []models.Bar) []vooOCVSample {
	if len(bars) < vooTreeWarmup+11 {
		return nil
	}
	out := make([]vooOCVSample, 0, len(bars)-vooTreeWarmup)
	for i := vooTreeWarmup; i < len(bars); i++ {
		curr := bars[i]
		prev := bars[i-1]
		if curr.Open <= 0 || curr.Close <= 0 || prev.Close <= 0 {
			continue
		}
		var sma20, sma50, sma200 float64
		for j := 0; j < 20; j++ {
			sma20 += bars[i-j].Close
		}
		for j := 0; j < 50; j++ {
			sma50 += bars[i-j].Close
		}
		for j := 0; j < 200; j++ {
			sma200 += bars[i-j].Close
		}
		sma20 /= 20
		sma50 /= 50
		sma200 /= 200

		var vol5, vol20 float64
		for j := 0; j < 5; j++ {
			vol5 += float64(bars[i-j].Volume)
		}
		for j := 0; j < 20; j++ {
			vol20 += float64(bars[i-j].Volume)
		}
		volVs5, volVs20 := 1.0, 1.0
		if vol5 > 0 {
			volVs5 = float64(curr.Volume) / (vol5 / 5.0)
		}
		if vol20 > 0 {
			volVs20 = float64(curr.Volume) / (vol20 / 20.0)
		}
		volChg := 0.0
		if prev.Volume > 0 {
			volChg = float64(curr.Volume)/float64(prev.Volume) - 1.0
		}

		consec := 0.0
		for k := i; k > i-10 && k > 0; k-- {
			if bars[k].Close < bars[k-1].Close {
				consec++
			} else {
				break
			}
		}

		out = append(out, vooOCVSample{
			Date:            curr.Date,
			Open:            curr.Open,
			Close:           curr.Close,
			Volume:          curr.Volume,
			High:            curr.High,
			Low:             curr.Low,
			Idx:             curr.Idx,
			Ret1d:           (curr.Close - prev.Close) / prev.Close,
			Ret3d:           (curr.Close - bars[i-3].Close) / bars[i-3].Close,
			Ret5d:           (curr.Close - bars[i-5].Close) / bars[i-5].Close,
			Ret10d:          (curr.Close - bars[i-10].Close) / bars[i-10].Close,
			Intraday:        (curr.Close - curr.Open) / curr.Open,
			Gap:             (curr.Open - prev.Close) / prev.Close,
			VolVs5:          volVs5,
			VolVs20:         volVs20,
			VolChg1d:        volChg,
			CloseVsSMA20:    (curr.Close - sma20) / sma20,
			CloseVsSMA50:    (curr.Close - sma50) / sma50,
			CloseVsSMA200:   (curr.Close - sma200) / sma200,
			ConsecutiveDown: consec,
		})
	}
	return out
}

func (s *VOOTreeStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var bars []models.Bar
	for sym, b := range barsBySymbol {
		if strings.EqualFold(sym, "VOO") {
			bars = b
			break
		}
	}
	if len(bars) < vooTreeWarmup+vooTreeHoldDays+10 {
		return nil
	}

	samples := computeVOOOCVSamples(bars)
	if len(samples) < vooTreeMinTrain {
		return nil
	}
	nCases := len(samples)

	barByDate := make(map[string]models.Bar, len(bars))
	idxByDate := make(map[string]int, len(bars))
	for i, b := range bars {
		barByDate[b.Date] = b
		idxByDate[b.Date] = i
	}

	names := []string{
		"Ret1d", "Ret3d", "Ret5d", "Ret10d",
		"Intraday", "Gap",
		"VolVs5", "VolVs20", "VolChg1d",
		"CloseVsSMA20", "CloseVsSMA50", "CloseVsSMA200",
		"ConsecutiveDown",
	}
	cols := make([]*CloudForest.DenseNumFeature, len(names))
	for i, name := range names {
		cols[i] = &CloudForest.DenseNumFeature{
			Name:    name,
			Missing: make([]bool, nCases),
			NumData: make([]float64, nCases),
		}
	}
	for i, s := range samples {
		vals := []float64{
			s.Ret1d, s.Ret3d, s.Ret5d, s.Ret10d,
			s.Intraday, s.Gap,
			s.VolVs5, s.VolVs20, s.VolChg1d,
			s.CloseVsSMA20, s.CloseVsSMA50, s.CloseVsSMA200,
			s.ConsecutiveDown,
		}
		for c, v := range vals {
			cols[c].NumData[i] = v
		}
	}

	targetF := &CloudForest.DenseCatFeature{
		Name:    "Target",
		Missing: make([]bool, nCases),
		CatData: make([]int, nCases),
		CatMap: &CloudForest.CatMap{
			Map:  map[string]int{"AVOID": 0, "BUY": 1},
			Back: []string{"AVOID", "BUY"},
		},
	}

	var trainIdx []int
	for i, sm := range samples {
		entryIdx, ok := idxByDate[sm.Date]
		if !ok || sm.Close <= 0 {
			continue
		}
		future := bars[entryIdx+1:]
		hit, valid := dualBarrierLabel(sm.Close, future, vooTreeTargetPct, vooTreeStopPct, vooTreeHoldDays)
		if !valid {
			continue
		}
		if hit {
			targetF.CatData[i] = 1
		}
		if vooTreeIsTrainYear(vooTreeDateYear(sm.Date)) {
			trainIdx = append(trainIdx, i)
		}
	}
	if len(trainIdx) < vooTreeMinTrain {
		fmt.Printf("   [voo_tree] not enough 2021–2022 labeled cases (%d, need >= %d)\n", len(trainIdx), vooTreeMinTrain)
		return nil
	}
	fmt.Printf("   [voo_tree] training on 2021–2022 (%d labeled cases); signals only on 2020 and 2023–2026\n", len(trainIdx))

	fm := &CloudForest.FeatureMatrix{Map: make(map[string]int)}
	for i, col := range cols {
		fm.Map[col.Name] = i
		fm.Data = append(fm.Data, col)
	}
	fm.Map["Target"] = len(fm.Data)
	fm.Data = append(fm.Data, targetF)

	cands := make([]int, len(cols))
	for i := range cols {
		cands[i] = i
	}
	weights := map[string]float64{"AVOID": 1.0, "BUY": 1.0}
	wrfTarget := CloudForest.NewWRFTarget(targetF, weights)
	allocs := CloudForest.NewBestSplitAllocs(len(trainIdx), wrfTarget)

	tree := CloudForest.NewTree()
	tree.Target = "Target"
	tree.Grow(fm, wrfTarget, trainIdx, cands, nil, len(cands), vooTreeLeafSize, vooTreeMaxDepth, false, false, false, false, false, nil, nil, allocs)

	bb := CloudForest.NewCatBallotBox(nCases)
	tree.Vote(fm, bb)

	var signals []models.Signal
	for i, sm := range samples {
		if !vooTreeIsEvalYear(vooTreeDateYear(sm.Date)) {
			continue
		}
		if bb.Tally(i) != "BUY" {
			continue
		}
		bar, ok := barByDate[sm.Date]
		if !ok || bar.Close <= 0 {
			continue
		}
		signals = append(signals, models.Signal{
			Idx:              sm.Idx,
			Symbol:           "VOO",
			Date:             sm.Date,
			Open:             bar.Open,
			High:             bar.High,
			Low:              bar.Low,
			Close:            bar.Close,
			Volume:           bar.Volume,
			BuyLimit:         bar.Close,
			Entry:            1,
			OrderType:        "limit",
			Direction:        "LONG",
			Regime:           "All Regimes",
			TakeProfit:       bar.Close * (1.0 + vooTreeTargetPct),
			StopLoss:         bar.Close * (1.0 - vooTreeStopPct),
			HoldDaysOverride: vooTreeHoldDays,
			AssetClass:       "equity",
			StrategyID:       s.ID(),
			Priority:         0,
			Metadata: map[string]float64{
				"train_on_2021_2022": 1,
				"eval_year":          float64(vooTreeDateYear(sm.Date)),
			},
		})
	}
	return signals
}

func (s *VOOTreeStrategy) ParameterSpace() ParameterSpace {
	cfg := s.DefaultConfig()
	return ParameterSpace{
		StrategyID:   s.ID(),
		StrategyName: s.Name(),
		Description:  s.Description(),
		Symbols:      []string{"VOO"},
		SignalSymbol: "VOO",
		Direction:    "tree",
		SignalDays:   []int{1},
		HoldDays:     []int{5, 10, 15, 20},
		TakeProfits:  []float64{0.02, 0.03, 0.04, 0.05},
		StopLosses:   []float64{0.02, 0.03, 0.04, 0.05},
		Regimes:      []string{"All Regimes"},
		Allocations:  []float64{cfg.AllocationPct},
		CashYield:    cfg.CashYieldAnnual,
		Baseline: BaselineParams{
			SignalDays: 1,
			HoldDays:   cfg.HoldingWindow,
			TakeProfit: cfg.TakeProfitPct,
			StopLoss:   vooTreeStopPct,
			Allocation: cfg.AllocationPct,
			Regime:     "All Regimes",
		},
	}
}

func init() {
	NewVOOTreeStrategy()
}
