package strategy

import (
	"math"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

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
