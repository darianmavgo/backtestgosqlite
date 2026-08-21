package strategy

import (
	"math"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// NOTE [Architectural Boundary]:
// As documented in docs/SQLvsGOModels.md, rolling-window calculations and signal screening
// natively belong in SQLite SQL pipelines (sql/strategies/) for maximum throughput, inspectability,
// and zero-allocation performance. The pure Go indicator functions below are retained as
// in-memory references and testing utilities.

// CalcSMA calculates the Simple Moving Average for a given period.
func CalcSMA(bars []models.Bar, period int) []float64 {
	sma := make([]float64, len(bars))
	if len(bars) < period {
		return sma
	}

	sum := 0.0
	for i := 0; i < period; i++ {
		sum += bars[i].Close
	}
	sma[period-1] = sum / float64(period)

	for i := period; i < len(bars); i++ {
		sum += bars[i].Close - bars[i-period].Close
		sma[i] = sum / float64(period)
	}
	return sma
}

// CalcEMA calculates the Exponential Moving Average for a given period.
func CalcEMA(bars []models.Bar, period int) []float64 {
	ema := make([]float64, len(bars))
	if len(bars) < period {
		return ema
	}

	sum := 0.0
	for i := 0; i < period; i++ {
		sum += bars[i].Close
	}
	ema[period-1] = sum / float64(period)

	multiplier := 2.0 / float64(period+1)
	for i := period; i < len(bars); i++ {
		ema[i] = (bars[i].Close-ema[i-1])*multiplier + ema[i-1]
	}
	return ema
}

// CalcRSI calculates the Relative Strength Index (Wilder's Smoothing) for a given period.
func CalcRSI(bars []models.Bar, period int) []float64 {
	rsi := make([]float64, len(bars))
	if len(bars) <= period {
		return rsi
	}

	gains := make([]float64, len(bars))
	losses := make([]float64, len(bars))
	for i := 1; i < len(bars); i++ {
		diff := bars[i].Close - bars[i-1].Close
		if diff > 0 {
			gains[i] = diff
		} else {
			losses[i] = -diff
		}
	}

	avgGain := 0.0
	avgLoss := 0.0
	for i := 1; i <= period; i++ {
		avgGain += gains[i]
		avgLoss += losses[i]
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	if avgLoss == 0 {
		rsi[period] = 100
	} else {
		rs := avgGain / avgLoss
		rsi[period] = 100 - (100 / (1 + rs))
	}

	for i := period + 1; i < len(bars); i++ {
		avgGain = (avgGain*float64(period-1) + gains[i]) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + losses[i]) / float64(period)

		if avgLoss == 0 {
			rsi[i] = 100
		} else {
			rs := avgGain / avgLoss
			rsi[i] = 100 - (100 / (1 + rs))
		}
	}
	return rsi
}

// BollingerBands contains the lower, middle, and upper bands.
type BollingerBands struct {
	Lower  []float64
	Middle []float64
	Upper  []float64
}

// CalcBollinger calculates Bollinger Bands for a given period and standard deviation multiplier.
func CalcBollinger(bars []models.Bar, period int, stdDevMultiplier float64) BollingerBands {
	bb := BollingerBands{
		Lower:  make([]float64, len(bars)),
		Middle: make([]float64, len(bars)),
		Upper:  make([]float64, len(bars)),
	}

	if len(bars) < period {
		return bb
	}

	for i := period - 1; i < len(bars); i++ {
		sum := 0.0
		for j := 0; j < period; j++ {
			sum += bars[i-j].Close
		}
		mean := sum / float64(period)
		bb.Middle[i] = mean

		sumSq := 0.0
		for j := 0; j < period; j++ {
			diff := bars[i-j].Close - mean
			sumSq += diff * diff
		}
		stdDev := math.Sqrt(sumSq / float64(period))
		bb.Lower[i] = mean - stdDevMultiplier*stdDev
		bb.Upper[i] = mean + stdDevMultiplier*stdDev
	}
	return bb
}

// MACD contains the MACD line, signal line, and histogram.
type MACD struct {
	MACD      []float64
	Signal    []float64
	Histogram []float64
}

// CalcMACD calculates Moving Average Convergence Divergence.
func CalcMACD(bars []models.Bar, fastPeriod, slowPeriod, signalPeriod int) MACD {
	n := len(bars)
	macdResult := MACD{
		MACD:      make([]float64, n),
		Signal:    make([]float64, n),
		Histogram: make([]float64, n),
	}
	if n < slowPeriod+signalPeriod {
		return macdResult
	}

	fastEMA := CalcEMA(bars, fastPeriod)
	slowEMA := CalcEMA(bars, slowPeriod)

	for i := slowPeriod - 1; i < n; i++ {
		macdResult.MACD[i] = fastEMA[i] - slowEMA[i]
	}

	// Calculate signal line as EMA of MACD line starting at slowPeriod-1
	startIdx := slowPeriod - 1
	validMACD := macdResult.MACD[startIdx:]
	if len(validMACD) >= signalPeriod {
		sum := 0.0
		for i := 0; i < signalPeriod; i++ {
			sum += validMACD[i]
		}
		sigEMA := sum / float64(signalPeriod)
		macdResult.Signal[startIdx+signalPeriod-1] = sigEMA
		macdResult.Histogram[startIdx+signalPeriod-1] = macdResult.MACD[startIdx+signalPeriod-1] - sigEMA

		mult := 2.0 / float64(signalPeriod+1)
		for i := signalPeriod; i < len(validMACD); i++ {
			sigEMA = (validMACD[i]-sigEMA)*mult + sigEMA
			actualIdx := startIdx + i
			macdResult.Signal[actualIdx] = sigEMA
			macdResult.Histogram[actualIdx] = macdResult.MACD[actualIdx] - sigEMA
		}
	}

	return macdResult
}

// DonchianChannel contains the upper, middle, and lower bands of Donchian channels.
type DonchianChannel struct {
	Upper  []float64
	Middle []float64
	Lower  []float64
}

// CalcDonchian calculates Donchian Channels over a given lookback period.
func CalcDonchian(bars []models.Bar, period int) DonchianChannel {
	n := len(bars)
	dc := DonchianChannel{
		Upper:  make([]float64, n),
		Middle: make([]float64, n),
		Lower:  make([]float64, n),
	}
	if n < period {
		return dc
	}

	for i := period - 1; i < n; i++ {
		maxHigh := bars[i].High
		minLow := bars[i].Low
		for j := 0; j < period; j++ {
			if bars[i-j].High > maxHigh {
				maxHigh = bars[i-j].High
			}
			if bars[i-j].Low < minLow {
				minLow = bars[i-j].Low
			}
		}
		dc.Upper[i] = maxHigh
		dc.Lower[i] = minLow
		dc.Middle[i] = (maxHigh + minLow) / 2.0
	}
	return dc
}

// CalcATR calculates Average True Range for volatility estimation.
func CalcATR(bars []models.Bar, period int) []float64 {
	atr := make([]float64, len(bars))
	if len(bars) <= period {
		return atr
	}

	tr := make([]float64, len(bars))
	tr[0] = bars[0].High - bars[0].Low
	for i := 1; i < len(bars); i++ {
		hl := bars[i].High - bars[i].Low
		hc := math.Abs(bars[i].High - bars[i-1].Close)
		lc := math.Abs(bars[i].Low - bars[i-1].Close)
		tr[i] = math.Max(hl, math.Max(hc, lc))
	}

	sumTR := 0.0
	for i := 0; i < period; i++ {
		sumTR += tr[i]
	}
	atr[period-1] = sumTR / float64(period)

	for i := period; i < len(bars); i++ {
		atr[i] = (atr[i-1]*float64(period-1) + tr[i]) / float64(period)
	}
	return atr
}
