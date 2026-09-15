package strategy

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/ryanbressler/CloudForest"
)

// VOOContextForestStrategy predicts VOO entries with a genuine CloudForest random
// forest (many bagged, feature-subsampled trees voting together), unlike the dt_*
// strategies which each fit a single tree. It's also the only strategy in this
// registry whose feature set spans multiple assets: alongside VOO's own technicals,
// every case carries GLD (gold), USO (oil), and UTEN (10yr Treasury) technicals from
// the same date as cross-asset context.
//
// Because it's a real ensemble, every signal gets a genuine confidence score — the
// weighted share of trees voting BUY — surfaced via Signal.Metadata["confidence"].
// That's the piece a single-tree dt_* strategy structurally cannot produce (see
// pkg/strategy/decisiontree.go: one tree, one hard vote, no vote-share to average).
//
// Label: does a trade entered at close hit +VOOContextTargetPct before
// -VOOContextStopPct within VOOContextHoldDays trading days? This dual-barrier label
// mirrors exactly what pkg/simulator scores a real trade on, so the model is trained
// against the same outcome the backtest will grade it on. (Duplicated in-line rather
// than calling pkg/simulator.EvaluateTradeOutcome, which already imports this
// package — importing it back here would be a cycle.)
type VOOContextForestStrategy struct {
	marketDBPath string
	calcDBPath   string
}

// NewVOOContextForestStrategy constructs and registers the strategy.
func NewVOOContextForestStrategy() *VOOContextForestStrategy {
	s := &VOOContextForestStrategy{}
	Register(s)
	RegisterAlias("voo-context-forest", s)
	return s
}

func (s *VOOContextForestStrategy) ID() string { return "voo_context_forest" }

func (s *VOOContextForestStrategy) Name() string {
	return "VOO Context Forest (CloudForest Random Forest: VOO+GLD+USO+UTEN)"
}

func (s *VOOContextForestStrategy) Description() string {
	return fmt.Sprintf(
		"CloudForest random forest (%d bagged trees, mtry feature-subsampling) trained on VOO's own "+
			"technicals plus GLD/USO/UTEN cross-asset context on the same date. Label: trade entered at "+
			"close hits +%.0f%% before -%.0f%% within %d trading days. Emits a per-signal confidence "+
			"score (weighted BUY vote share) via Signal.Metadata[\"confidence\"].",
		VOOContextNTrees, VOOContextTargetPct*100, VOOContextStopPct*100, VOOContextHoldDays)
}

func (s *VOOContextForestStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

// RequiredSymbols declares VOO as the trade target plus GLD/USO/UTEN as context-only
// inputs (features are derived from their bars but no position is ever taken in them).
func (s *VOOContextForestStrategy) RequiredSymbols() []string {
	return []string{"VOO", "GLD", "USO", "UTEN"}
}

func (s *VOOContextForestStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "VOO",
		Timeframe:          "1d",
		PositionSizing:     "fixed_pct",
		AllocationPct:      0.90,
		TargetPct:          1.0 + VOOContextTargetPct,
		TakeProfitPct:      VOOContextTargetPct,
		StopLossPct:        1.0 - VOOContextStopPct,
		HoldingWindow:      VOOContextHoldDays,
		PositionCap:        1,
		CashYieldAnnual:    0.045,
		SlippagePct:        0.0005,
		CommissionPerShare: 0.0001,
	}
}

func (s *VOOContextForestStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

// Label/model parameters. Exported so DefaultConfig, Description, and any future
// caller stay in sync with what the forest was actually trained on.
const (
	VOOContextTargetPct = 0.03 // +3% take-profit barrier used both to label training data and to exit real trades
	VOOContextStopPct   = 0.02 // -2% stop-loss barrier, same dual role
	VOOContextHoldDays  = 15   // trading days a case has to hit one barrier before it's labeled a time-exit (AVOID)
	VOOContextNTrees    = 300  // bagged trees in the ensemble
	VOOContextLeafSize  = 5
	VOOContextMaxDepth  = 8

	// VOOContextTrainFrac is the chronological split point: only the earliest
	// fraction of cases is eligible to train the forest. The remainder is a genuine
	// held-out period the forest never saw during training — necessary because a
	// 300-tree/52-feature ensemble fit and evaluated on the same ~1300 rows with no
	// holdout will essentially memorize the training path (this strategy originally
	// shipped without a split and produced a 100% win rate over 70 trades, which is
	// an overfitting signature, not an edge). Signals are still generated for the
	// training period too (so the backtest can show both halves), but only trades
	// entered after the split date are a trustworthy read on the model.
	VOOContextTrainFrac = 0.65
)

// voo_context_target_symbol is the only symbol positions are ever taken in.
const voo_context_target_symbol = "VOO"

// voo_context_symbols is every symbol whose technicals feed the feature matrix,
// target included (its own technicals are also predictive context for itself).
var voo_context_symbols = []string{"VOO", "GLD", "USO", "UTEN"}

// voo_context_feature_order is the 13-feature set from computeDecisionTreeSamples,
// repeated once per symbol in voo_context_symbols (e.g. "GLD_RSI14", "USO_Return5d").
var voo_context_feature_order = []string{
	"Return1d", "Return3d", "Return5d", "Return10d", "RSI14",
	"PriceVsSMA20", "PriceVsSMA50", "PriceVsSMA200", "SMA20Vs50",
	"VolRatio20", "RangeVsATR14", "CloseNearHigh", "ConsecutiveDown",
}

func (s *VOOContextForestStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	bySymbol := make(map[string][]models.Bar, len(voo_context_symbols))
	for _, want := range voo_context_symbols {
		for sym, bars := range barsBySymbol {
			if strings.EqualFold(sym, want) {
				bySymbol[want] = bars
				break
			}
		}
	}

	targetBars, ok := bySymbol[voo_context_target_symbol]
	if !ok || len(targetBars) < 250 {
		return nil
	}

	signals, err := ContextForestSignals(targetBars, bySymbol, VOOContextTargetPct, VOOContextStopPct, VOOContextHoldDays, VOOContextNTrees, VOOContextTrainFrac)
	if err != nil {
		return nil
	}
	return signals
}

// decisionTreeSampleField pulls one named feature out of an already-computed sample
// (see computeDecisionTreeSamples), so callers can build a feature matrix generically
// over voo_context_feature_order instead of hand-listing every field.
func decisionTreeSampleField(cs decisionTreeSample, name string) float64 {
	switch name {
	case "Return1d":
		return cs.Return1d
	case "Return3d":
		return cs.Return3d
	case "Return5d":
		return cs.Return5d
	case "Return10d":
		return cs.Return10d
	case "RSI14":
		return cs.RSI14
	case "PriceVsSMA20":
		return cs.PriceVsSMA20
	case "PriceVsSMA50":
		return cs.PriceVsSMA50
	case "PriceVsSMA200":
		return cs.PriceVsSMA200
	case "SMA20Vs50":
		return cs.SMA20Vs50
	case "VolRatio20":
		return cs.VolRatio20
	case "RangeVsATR14":
		return cs.RangeVsATR14
	case "CloseNearHigh":
		return cs.CloseNearHigh
	case "ConsecutiveDown":
		return float64(cs.ConsecutiveDown)
	}
	return 0
}

// dualBarrierLabel walks forward day-by-day (risk-first tie-break, same convention as
// pkg/simulator.EvaluateTradeOutcome) to decide whether a trade entered at entryPrice
// would hit its profit target before its stop-loss within holdDays. ok=false means
// there wasn't enough forward data to judge this case (e.g. it's within holdDays of
// the end of the dataset) — the caller should exclude it from training rather than
// guess a label.
func dualBarrierLabel(entryPrice float64, futureBars []models.Bar, targetPct, stopPct float64, holdDays int) (hit bool, ok bool) {
	if len(futureBars) < holdDays {
		return false, false
	}
	targetPrice := entryPrice * (1.0 + targetPct)
	stopPrice := entryPrice * (1.0 - stopPct)
	for day := 0; day < holdDays; day++ {
		bar := futureBars[day]
		if bar.Low <= stopPrice {
			return false, true
		}
		if bar.High >= targetPrice {
			return true, true
		}
	}
	return false, true
}

// ContextForestSignals grows a random forest against targetBars using its own
// technicals plus every symbol in contextBars as extra per-date features, and
// converts the forest's vote for each historical date into an entry signal carrying
// a confidence score. Only dates where the dual-barrier label is BUY are returned as
// signals; every other date is implicitly AVOID.
//
// trainFrac restricts training to the earliest fraction of chronologically-ordered
// cases (see VOOContextTrainFrac) — the forest still votes on every case, so signals
// are returned across the whole date range, but only signals dated after the split
// are out-of-sample. Pass 1.0 to disable the split (train on everything, matching
// the in-sample convention every other dt_* strategy in this registry uses).
func ContextForestSignals(targetBars []models.Bar, contextBars map[string][]models.Bar, tpPct, slPct float64, holdDays, nTrees int, trainFrac float64) ([]models.Signal, error) {
	targetSamples := computeDecisionTreeSamples(targetBars)
	if len(targetSamples) < 250 {
		return nil, fmt.Errorf("insufficient target samples (%d, need >= 250)", len(targetSamples))
	}
	nCases := len(targetSamples)

	// Index every context symbol's own technicals by date so they can be joined onto
	// the target's date axis. A symbol with a shorter history (e.g. UTEN, which only
	// started trading in 2022) simply has no entry for early dates — those specific
	// feature columns are marked Missing for those cases rather than back-filled or
	// dropped, and CloudForest routes missing values during tree growth.
	contextSymbols := make([]string, 0, len(contextBars))
	for sym := range contextBars {
		contextSymbols = append(contextSymbols, sym)
	}
	sort.Strings(contextSymbols)

	contextByDate := make(map[string]map[string]decisionTreeSample, len(contextSymbols))
	for _, sym := range contextSymbols {
		samples := computeDecisionTreeSamples(contextBars[sym])
		byDate := make(map[string]decisionTreeSample, len(samples))
		for _, cs := range samples {
			byDate[cs.Date] = cs
		}
		contextByDate[sym] = byDate
	}

	// Build the (symbol x feature) column set and populate every case.
	type column struct {
		name    string
		data    []float64
		missing []bool
	}
	var columns []column
	for _, sym := range contextSymbols {
		for _, fname := range voo_context_feature_order {
			columns = append(columns, column{
				name:    sym + "_" + fname,
				data:    make([]float64, nCases),
				missing: make([]bool, nCases),
			})
		}
	}
	for ci := range columns {
		col := &columns[ci]
		sym := contextSymbols[ci/len(voo_context_feature_order)]
		fname := voo_context_feature_order[ci%len(voo_context_feature_order)]
		byDate := contextByDate[sym]
		for i, ts := range targetSamples {
			if cs, ok := byDate[ts.Date]; ok {
				col.data[i] = decisionTreeSampleField(cs, fname)
			} else {
				col.missing[i] = true
			}
		}
	}

	// Labels: dual-barrier forward outcome on the target symbol only.
	barByDate := make(map[string]models.Bar, len(targetBars))
	idxByDate := make(map[string]int, len(targetBars))
	for i, b := range targetBars {
		barByDate[b.Date] = b
		idxByDate[b.Date] = i
	}

	if trainFrac <= 0 || trainFrac > 1 {
		trainFrac = 1.0
	}
	splitIdx := int(float64(nCases) * trainFrac)
	if splitIdx > 0 && splitIdx < nCases {
		fmt.Printf("   [voo_context_forest] train/test split: training on cases before %s (%d/%d cases), voting on all %d\n",
			targetSamples[splitIdx].Date, splitIdx, nCases, nCases)
	}

	targetCatData := make([]int, nCases)
	var trainableCases []int
	for i, ts := range targetSamples {
		entryIdx, ok := idxByDate[ts.Date]
		if !ok {
			continue
		}
		entryPrice := targetBars[entryIdx].Close
		if entryPrice <= 0 {
			continue
		}
		future := targetBars[entryIdx+1:]
		hit, valid := dualBarrierLabel(entryPrice, future, tpPct, slPct, holdDays)
		if !valid {
			continue // too close to the end of history to judge — excluded from training, still eligible for a vote below
		}
		if hit {
			targetCatData[i] = 1
		}
		if i < splitIdx { // chronological split: only earlier cases may train the forest
			trainableCases = append(trainableCases, i)
		}
	}
	if len(trainableCases) < 250 {
		return nil, fmt.Errorf("insufficient labeled training cases before the train/test split (%d, need >= 250)", len(trainableCases))
	}

	// Assemble the CloudForest feature matrix: one DenseNumFeature per column plus
	// the categorical Target. Every case (including the untrainable tail) is present
	// so the forest can still cast a vote for every historical date once grown.
	fm := &CloudForest.FeatureMatrix{Map: make(map[string]int)}
	for _, col := range columns {
		fm.Map[col.name] = len(fm.Data)
		fm.Data = append(fm.Data, &CloudForest.DenseNumFeature{Name: col.name, Missing: col.missing, NumData: col.data})
	}
	targetFeature := &CloudForest.DenseCatFeature{
		Name:    "Target",
		Missing: make([]bool, nCases),
		CatData: targetCatData,
		CatMap: &CloudForest.CatMap{
			Map:  map[string]int{"AVOID": 0, "BUY": 1},
			Back: []string{"AVOID", "BUY"},
		},
	}
	fm.Map["Target"] = len(fm.Data)
	fm.Data = append(fm.Data, targetFeature)

	candidateIndices := make([]int, len(columns))
	for i := range columns {
		candidateIndices[i] = i
	}

	weights := map[string]float64{"AVOID": 1.0, "BUY": 1.0} // balanced — the dual-barrier label isn't a rare-event target the way dt_*'s +-5% extreme-move label is
	wrfTarget := CloudForest.NewWRFTarget(targetFeature, weights)

	mTry := int(math.Sqrt(float64(len(columns))))
	if mTry < 1 {
		mTry = 1
	}
	allocs := CloudForest.NewBestSplitAllocs(len(trainableCases), wrfTarget)

	bb := CloudForest.NewCatBallotBox(nCases)
	for t := 0; t < nTrees; t++ {
		positions := CloudForest.SampleWithReplacment(len(trainableCases), len(trainableCases))
		bootstrap := make([]int, len(positions))
		for k, p := range positions {
			bootstrap[k] = trainableCases[p]
		}

		tree := CloudForest.NewTree()
		tree.Target = "Target"
		tree.Grow(fm, wrfTarget, bootstrap, candidateIndices, nil, mTry, VOOContextLeafSize, VOOContextMaxDepth, true, false, false, false, false, nil, nil, allocs)
		tree.Vote(fm, bb) // votes on every case in fm, not just bootstrap — this is how out-of-bag/tail cases get a prediction
	}

	// CatBallotBox keeps its own internal category-index map, assigned in whatever
	// order labels are first voted — it is NOT the same as targetFeature.CatMap
	// above, so "BUY"/"AVOID"'s indices into bb.Box[i].Map must be looked up from
	// bb itself, never assumed to be 0/1. (CatToNum registers the label if it
	// somehow was never voted for any case, returning a fresh, harmless index.)
	buyIdx := bb.CatToNum("BUY")
	avoidIdx := bb.CatToNum("AVOID")

	var signals []models.Signal
	for i, ts := range targetSamples {
		if bb.Tally(i) != "BUY" {
			continue
		}
		ballot := bb.Box[i]
		buyVotes := ballot.Map[buyIdx]
		avoidVotes := ballot.Map[avoidIdx]
		confidence := 0.0
		if total := buyVotes + avoidVotes; total > 0 {
			confidence = buyVotes / total
		}
		outOfSample := 0.0
		if i >= splitIdx {
			outOfSample = 1.0
		}

		bar, ok := barByDate[ts.Date]
		if !ok || bar.Close <= 0 {
			continue
		}
		closePrice := bar.Close
		signals = append(signals, models.Signal{
			Symbol:           voo_context_target_symbol,
			Date:             ts.Date,
			Open:             bar.Open,
			High:             bar.High,
			Low:              bar.Low,
			Close:            closePrice,
			Volume:           bar.Volume,
			BuyLimit:         closePrice,
			Entry:            1,
			OrderType:        "limit",
			Direction:        "LONG",
			Regime:           "All Regimes",
			TakeProfit:       closePrice * (1.0 + tpPct),
			StopLoss:         closePrice * (1.0 - slPct),
			HoldDaysOverride: holdDays,
			AssetClass:       "equity",
			StrategyID:       "voo_context_forest",
			Priority:         0,
			Metadata: map[string]float64{
				"confidence":    confidence,
				"votes_buy":     buyVotes,
				"votes_avoid":   avoidVotes,
				"out_of_sample": outOfSample,
			},
		})
	}

	return signals, nil
}

func init() {
	NewVOOContextForestStrategy()
}
