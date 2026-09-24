package strategy

import (
	"fmt"
	"math"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/ryanbressler/CloudForest"
)

// decisionTreeSample mirrors the feature set originally reverse-engineered for MARA
// (pkg/study/mara_decision_tree.go), generalized to any symbol's bars.
type decisionTreeSample struct {
	Date            string
	Close           float64
	NextReturn      float64
	IsGain5         bool
	IsDrop5         bool
	Return1d        float64
	Return3d        float64
	Return5d        float64
	Return10d       float64
	RSI14           float64
	PriceVsSMA20    float64
	PriceVsSMA50    float64
	PriceVsSMA200   float64
	SMA20Vs50       float64
	VolRatio20      float64
	RangeVsATR14    float64
	CloseNearHigh   float64
	ConsecutiveDown int
}

// computeDecisionTreeSamples computes the same 13-feature set used for the original
// MARA/MU CloudForest studies, generalized to any symbol's daily bars.
func computeDecisionTreeSamples(bars []models.Bar) []decisionTreeSample {
	var samples []decisionTreeSample

	for i := 200; i < len(bars)-1; i++ {
		curr := bars[i]
		next := bars[i+1]

		nextReturn := (next.Close - curr.Close) / curr.Close * 100.0
		isGain5 := nextReturn >= 5.0
		isDrop5 := nextReturn <= -5.0

		ret1d := (curr.Close - bars[i-1].Close) / bars[i-1].Close * 100.0
		ret3d := (curr.Close - bars[i-3].Close) / bars[i-3].Close * 100.0
		ret5d := (curr.Close - bars[i-5].Close) / bars[i-5].Close * 100.0
		ret10d := (curr.Close - bars[i-10].Close) / bars[i-10].Close * 100.0

		var sum20, sum50, sum200 float64
		for j := 0; j < 20; j++ {
			sum20 += bars[i-j].Close
		}
		for j := 0; j < 50; j++ {
			sum50 += bars[i-j].Close
		}
		for j := 0; j < 200; j++ {
			sum200 += bars[i-j].Close
		}
		sma20 := sum20 / 20.0
		sma50 := sum50 / 50.0
		sma200 := sum200 / 200.0

		pVsSma20 := (curr.Close - sma20) / sma20 * 100.0
		pVsSma50 := (curr.Close - sma50) / sma50 * 100.0
		pVsSma200 := (curr.Close - sma200) / sma200 * 100.0
		sma20Vs50 := (sma20 - sma50) / sma50 * 100.0

		var gains, losses float64
		for j := i - 13; j <= i; j++ {
			chg := bars[j].Close - bars[j-1].Close
			if chg > 0 {
				gains += chg
			} else {
				losses -= chg
			}
		}
		avgGain := gains / 14.0
		avgLoss := losses / 14.0
		rsi := 50.0
		if avgLoss > 0 {
			rs := avgGain / avgLoss
			rsi = 100.0 - (100.0 / (1.0 + rs))
		} else if avgGain > 0 {
			rsi = 100.0
		}

		var volSum float64
		for j := 0; j < 20; j++ {
			volSum += float64(bars[i-j].Volume)
		}
		volRatio := 1.0
		if volSum > 0 {
			volRatio = float64(curr.Volume) / (volSum / 20.0)
		}

		var trSum float64
		for j := i - 13; j <= i; j++ {
			tr := math.Max(bars[j].High-bars[j].Low, math.Max(math.Abs(bars[j].High-bars[j-1].Close), math.Abs(bars[j].Low-bars[j-1].Close)))
			trSum += tr
		}
		atr14 := trSum / 14.0
		currRange := curr.High - curr.Low
		rangeRatio := 1.0
		if atr14 > 0 {
			rangeRatio = currRange / atr14
		}

		closeNearHigh := 0.5
		if currRange > 0 {
			closeNearHigh = (curr.Close - curr.Low) / currRange
		}

		consecDown := 0
		for k := i; k > i-10; k-- {
			if bars[k].Close < bars[k-1].Close {
				consecDown++
			} else {
				break
			}
		}

		samples = append(samples, decisionTreeSample{
			Date:            curr.Date,
			Close:           curr.Close,
			NextReturn:      nextReturn,
			IsGain5:         isGain5,
			IsDrop5:         isDrop5,
			Return1d:        ret1d,
			Return3d:        ret3d,
			Return5d:        ret5d,
			Return10d:       ret10d,
			RSI14:           rsi,
			PriceVsSMA20:    pVsSma20,
			PriceVsSMA50:    pVsSma50,
			PriceVsSMA200:   pVsSma200,
			SMA20Vs50:       sma20Vs50,
			VolRatio20:      volRatio,
			RangeVsATR14:    rangeRatio,
			CloseNearHigh:   closeNearHigh,
			ConsecutiveDown: consecDown,
		})
	}

	return samples
}

// MinDecisionTreeExtremeCases is the minimum number of +-5% single-day moves
// required before a decision tree is even attempted — below this, there's not
// enough signal in the data to learn a meaningful rule (common for low-volatility
// bond/treasury ETFs).
const MinDecisionTreeExtremeCases = 20

// FitDecisionTreeBuyDates fits a depth-3 CloudForest decision tree (the same
// "Precision Re-test" style model architecture originally reverse-engineered for
// MARA in pkg/study/mara_decision_tree.go) against a symbol's own daily bars, and
// returns the set of historical dates the tree predicts BUY on. The tree is fit
// fresh from bars every call — this is a reverse-engineering/in-sample exercise
// consistent with how the original MARA/MU studies operated, not a walk-forward
// train/test pipeline.
//
// Feature extraction (computeDecisionTreeSamples) is pure Go, rescanning up to
// 200 trailing bars per row; FitDecisionTreeBuyDatesSQL below gets identical
// samples from SQL window functions instead (sql/strategies/decision_tree_features)
// for the many auto-fit dt_<symbol> strategies in etf_decision_tree.go. Both feed
// the same fitBuyDatesFromSamples, since CloudForest's recursive tree-growing
// itself is exactly the stateful control flow SQL can't express.
func FitDecisionTreeBuyDates(bars []models.Bar) (map[string]bool, error) {
	return fitBuyDatesFromSamples(computeDecisionTreeSamples(bars))
}

// fitBuyDatesFromSamples grows the tree from an already-computed feature set
// and returns the historical dates it predicts BUY on.
func fitBuyDatesFromSamples(samples []decisionTreeSample) (map[string]bool, error) {
	if len(samples) < 250 {
		return nil, fmt.Errorf("insufficient samples (%d, need >= 250)", len(samples))
	}

	nCases := len(samples)

	fRet1d := &CloudForest.DenseNumFeature{Name: "Return1d", Missing: make([]bool, nCases), NumData: make([]float64, nCases)}
	fRet3d := &CloudForest.DenseNumFeature{Name: "Return3d", Missing: make([]bool, nCases), NumData: make([]float64, nCases)}
	fRet5d := &CloudForest.DenseNumFeature{Name: "Return5d", Missing: make([]bool, nCases), NumData: make([]float64, nCases)}
	fRet10d := &CloudForest.DenseNumFeature{Name: "Return10d", Missing: make([]bool, nCases), NumData: make([]float64, nCases)}
	fRSI := &CloudForest.DenseNumFeature{Name: "RSI14", Missing: make([]bool, nCases), NumData: make([]float64, nCases)}
	fPvs20 := &CloudForest.DenseNumFeature{Name: "PriceVsSMA20", Missing: make([]bool, nCases), NumData: make([]float64, nCases)}
	fPvs50 := &CloudForest.DenseNumFeature{Name: "PriceVsSMA50", Missing: make([]bool, nCases), NumData: make([]float64, nCases)}
	fPvs200 := &CloudForest.DenseNumFeature{Name: "PriceVsSMA200", Missing: make([]bool, nCases), NumData: make([]float64, nCases)}
	fSma2050 := &CloudForest.DenseNumFeature{Name: "SMA20Vs50", Missing: make([]bool, nCases), NumData: make([]float64, nCases)}
	fVolRatio := &CloudForest.DenseNumFeature{Name: "VolRatio20", Missing: make([]bool, nCases), NumData: make([]float64, nCases)}
	fRangeATR := &CloudForest.DenseNumFeature{Name: "RangeVsATR14", Missing: make([]bool, nCases), NumData: make([]float64, nCases)}
	fCloseNearHigh := &CloudForest.DenseNumFeature{Name: "CloseNearHigh", Missing: make([]bool, nCases), NumData: make([]float64, nCases)}
	fConsecDown := &CloudForest.DenseNumFeature{Name: "ConsecutiveDown", Missing: make([]bool, nCases), NumData: make([]float64, nCases)}

	targetF := &CloudForest.DenseCatFeature{
		Name:    "Target",
		Missing: make([]bool, nCases),
		CatData: make([]int, nCases),
		CatMap: &CloudForest.CatMap{
			Map:  map[string]int{"AVOID": 0, "BUY": 1},
			Back: []string{"AVOID", "BUY"},
		},
	}

	var extremeCases []int
	for i, s := range samples {
		fRet1d.NumData[i] = s.Return1d
		fRet3d.NumData[i] = s.Return3d
		fRet5d.NumData[i] = s.Return5d
		fRet10d.NumData[i] = s.Return10d
		fRSI.NumData[i] = s.RSI14
		fPvs20.NumData[i] = s.PriceVsSMA20
		fPvs50.NumData[i] = s.PriceVsSMA50
		fPvs200.NumData[i] = s.PriceVsSMA200
		fSma2050.NumData[i] = s.SMA20Vs50
		fVolRatio.NumData[i] = s.VolRatio20
		fRangeATR.NumData[i] = s.RangeVsATR14
		fCloseNearHigh.NumData[i] = s.CloseNearHigh
		fConsecDown.NumData[i] = float64(s.ConsecutiveDown)

		if s.IsGain5 {
			targetF.CatData[i] = 1
			extremeCases = append(extremeCases, i)
		} else if s.IsDrop5 {
			targetF.CatData[i] = 0
			extremeCases = append(extremeCases, i)
		} else {
			targetF.CatData[i] = 0
		}
	}

	if len(extremeCases) < MinDecisionTreeExtremeCases {
		return nil, fmt.Errorf("too few +-5%% extreme-move days to fit a tree (%d, need >= %d)", len(extremeCases), MinDecisionTreeExtremeCases)
	}

	fm := &CloudForest.FeatureMatrix{
		Data: []CloudForest.Feature{
			fRet1d, fRet3d, fRet5d, fRet10d, fRSI, fPvs20, fPvs50, fPvs200, fSma2050, fVolRatio, fRangeATR, fCloseNearHigh, fConsecDown, targetF,
		},
		Map: map[string]int{
			"Return1d": 0, "Return3d": 1, "Return5d": 2, "Return10d": 3, "RSI14": 4, "PriceVsSMA20": 5, "PriceVsSMA50": 6, "PriceVsSMA200": 7,
			"SMA20Vs50": 8, "VolRatio20": 9, "RangeVsATR14": 10, "CloseNearHigh": 11, "ConsecutiveDown": 12, "Target": 13,
		},
	}

	candidateIndices := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	weights := map[string]float64{"AVOID": 1.5, "BUY": 1.0}
	wrfTarget := CloudForest.NewWRFTarget(targetF, weights)
	allocs := CloudForest.NewBestSplitAllocs(nCases, wrfTarget)

	tree := CloudForest.NewTree()
	tree.Target = "Target"
	tree.Grow(fm, wrfTarget, extremeCases, candidateIndices, nil, len(candidateIndices), 15, 3, false, false, false, false, false, nil, nil, allocs)

	bb := CloudForest.NewCatBallotBox(nCases)
	tree.Vote(fm, bb)

	buyDates := make(map[string]bool)
	for i, s := range samples {
		if bb.Tally(i) == "BUY" {
			buyDates[s.Date] = true
		}
	}

	return buyDates, nil
}

// decisionTreeFeaturesPipelineDir is the shared SQL pipeline directory every
// dt_<symbol> ETFDecisionTreeStrategy instance points at (see etf_decision_tree.go).
const decisionTreeFeaturesPipelineDir = "sql/strategies/decision_tree_features"

// dtfRow scans one row of decision_tree_features_slice (see
// sql/strategies/decision_tree_features/01_schema.sql); is_gain5/is_drop5 are
// stored as SQLite INTEGER 0/1 and converted to bool when building samples.
type dtfRow struct {
	Date            string  `db:"date"`
	Close           float64 `db:"close"`
	NextReturn      float64 `db:"next_return"`
	IsGain5         int     `db:"is_gain5"`
	IsDrop5         int     `db:"is_drop5"`
	Return1d        float64 `db:"return_1d"`
	Return3d        float64 `db:"return_3d"`
	Return5d        float64 `db:"return_5d"`
	Return10d       float64 `db:"return_10d"`
	RSI14           float64 `db:"rsi14"`
	PriceVsSMA20    float64 `db:"price_vs_sma20"`
	PriceVsSMA50    float64 `db:"price_vs_sma50"`
	PriceVsSMA200   float64 `db:"price_vs_sma200"`
	SMA20Vs50       float64 `db:"sma20_vs_50"`
	VolRatio20      float64 `db:"vol_ratio20"`
	RangeVsATR14    float64 `db:"range_vs_atr14"`
	CloseNearHigh   float64 `db:"close_near_high"`
	ConsecutiveDown int     `db:"consecutive_down"`
}

// computeDecisionTreeSamplesSQL runs the shared decision_tree_features SQL
// pipeline for symbol (window-function equivalent of computeDecisionTreeSamples)
// and scans its output back into the same []decisionTreeSample shape, so
// fitBuyDatesFromSamples can't tell which source produced them. Locks
// sqlPipelineMu like SQLPipelineStrategy.GenerateSignals, since both share the
// same calc DB connection pattern and aren't safe to run concurrently against
// the same calc DB.
func computeDecisionTreeSamplesSQL(marketDBPath, calcDBPath, pipelineDir, symbol string) ([]decisionTreeSample, error) {
	sqlPipelineMu.Lock()
	defer sqlPipelineMu.Unlock()

	db, err := openAttachedCalcDB(marketDBPath, calcDBPath)
	if err != nil {
		return nil, fmt.Errorf("decision tree features pipeline: %w", err)
	}
	defer db.Close()

	cfg := StrategyConfig{Benchmark: symbol}
	for _, fileName := range []string{"01_schema.sql", "02_calc_features.sql"} {
		content, err := readPipelineFile(pipelineDir, fileName)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", fileName, err)
		}
		sqlText := substitutePlaceholders(string(content), "decision_tree_features", fileName, cfg)
		for _, q := range strings.Split(sqlText, ";") {
			trimmed := strings.TrimSpace(q)
			if trimmed == "" {
				continue
			}
			if _, err := db.Exec(trimmed); err != nil {
				return nil, fmt.Errorf("exec %s: %w (query: %.80s...)", fileName, err, trimmed)
			}
		}
	}

	var rows []dtfRow
	if err := db.Select(&rows, "SELECT * FROM decision_tree_features_slice ORDER BY date"); err != nil {
		return nil, fmt.Errorf("select decision_tree_features_slice: %w", err)
	}

	samples := make([]decisionTreeSample, len(rows))
	for i, r := range rows {
		samples[i] = decisionTreeSample{
			Date:            r.Date,
			Close:           r.Close,
			NextReturn:      r.NextReturn,
			IsGain5:         r.IsGain5 != 0,
			IsDrop5:         r.IsDrop5 != 0,
			Return1d:        r.Return1d,
			Return3d:        r.Return3d,
			Return5d:        r.Return5d,
			Return10d:       r.Return10d,
			RSI14:           r.RSI14,
			PriceVsSMA20:    r.PriceVsSMA20,
			PriceVsSMA50:    r.PriceVsSMA50,
			PriceVsSMA200:   r.PriceVsSMA200,
			SMA20Vs50:       r.SMA20Vs50,
			VolRatio20:      r.VolRatio20,
			RangeVsATR14:    r.RangeVsATR14,
			CloseNearHigh:   r.CloseNearHigh,
			ConsecutiveDown: r.ConsecutiveDown,
		}
	}
	return samples, nil
}

// FitDecisionTreeBuyDatesSQL is FitDecisionTreeBuyDates with feature extraction
// computed by the shared SQL pipeline (sql/strategies/decision_tree_features)
// instead of Go loops. pipelineDir lets callers pass an already-resolved path
// (see ETFDecisionTreeStrategy.GenerateSignals); empty uses the repo-root-relative
// default.
func FitDecisionTreeBuyDatesSQL(marketDBPath, calcDBPath, pipelineDir, symbol string) (map[string]bool, error) {
	if pipelineDir == "" {
		pipelineDir = decisionTreeFeaturesPipelineDir
	}
	samples, err := computeDecisionTreeSamplesSQL(marketDBPath, calcDBPath, pipelineDir, symbol)
	if err != nil {
		return nil, err
	}
	return fitBuyDatesFromSamples(samples)
}

// DecisionTreeSignals fits a fresh decision tree against bars and converts its BUY
// predictions into entry signals with the given exit parameters. Returns an error
// (nil signals) if the symbol doesn't have enough history or enough extreme moves
// to fit a meaningful tree.
func DecisionTreeSignals(symbol string, bars []models.Bar, tpPct, slPct float64, holdDays int) ([]models.Signal, error) {
	buyDates, err := FitDecisionTreeBuyDates(bars)
	if err != nil {
		return nil, err
	}
	if len(buyDates) == 0 {
		return nil, fmt.Errorf("tree produced zero BUY predictions")
	}
	return BuildDecisionTreeSignals(symbol, bars, buyDates, tpPct, slPct, holdDays), nil
}

// DecisionTreeSignalsSQL is DecisionTreeSignals with feature extraction run
// through the shared SQL pipeline (see FitDecisionTreeBuyDatesSQL) instead of
// Go loops. bars is still required to build the resulting signals' OHLC —
// only feature/label extraction moves to SQL, not signal assembly.
func DecisionTreeSignalsSQL(symbol, marketDBPath, calcDBPath, pipelineDir string, bars []models.Bar, tpPct, slPct float64, holdDays int) ([]models.Signal, error) {
	buyDates, err := FitDecisionTreeBuyDatesSQL(marketDBPath, calcDBPath, pipelineDir, symbol)
	if err != nil {
		return nil, err
	}
	if len(buyDates) == 0 {
		return nil, fmt.Errorf("tree produced zero BUY predictions")
	}
	return BuildDecisionTreeSignals(symbol, bars, buyDates, tpPct, slPct, holdDays), nil
}

// BuildDecisionTreeSignals converts an already-fit tree's BUY dates (see
// FitDecisionTreeBuyDates) into entry signals with the given exit parameters.
// Splitting this from DecisionTreeSignals lets a caller fit the tree once and
// cheaply sweep many TP/SL/hold combinations against the same BUY dates.
func BuildDecisionTreeSignals(symbol string, bars []models.Bar, buyDates map[string]bool, tpPct, slPct float64, holdDays int) []models.Signal {
	barByDate := make(map[string]models.Bar, len(bars))
	for _, b := range bars {
		barByDate[b.Date] = b
	}

	var signals []models.Signal
	for date := range buyDates {
		bar, ok := barByDate[date]
		if !ok || bar.Close <= 0 {
			continue
		}
		closePrice := bar.Close
		var takeProfit, stopLoss float64
		if tpPct > 0 {
			takeProfit = closePrice * (1.0 + tpPct)
		}
		if slPct > 0 {
			stopLoss = closePrice * (1.0 - slPct)
		}
		signals = append(signals, models.Signal{
			Symbol:           symbol,
			Date:             date,
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
			TakeProfit:       takeProfit,
			StopLoss:         stopLoss,
			HoldDaysOverride: holdDays,
			AssetClass:       "equity",
			StrategyID:       "decision_tree",
			Priority:         0,
		})
	}

	return signals
}
