package analytics

import (
	"math"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// CalculatePerformanceMetrics computes standard quantitative risk and return metrics
// from trade history and daily equity time series.
func CalculatePerformanceMetrics(
	initialCapital float64,
	trades []models.Trade,
	equityPoints []models.DailyEquityPoint,
) models.PerformanceReport {
	report := models.PerformanceReport{
		InitialCapital: initialCapital,
		TotalTrades:    len(trades),
	}

	if len(equityPoints) == 0 {
		return report
	}

	report.StartDate = equityPoints[0].Date
	report.EndDate = equityPoints[len(equityPoints)-1].Date
	report.TotalTradingDays = len(equityPoints)
	report.TotalCalendarYears = float64(report.TotalTradingDays) / 252.0

	finalEquity := equityPoints[len(equityPoints)-1].TotalEquity
	report.FinalEquity = finalEquity
	report.NetProfit = finalEquity - initialCapital
	if initialCapital > 0 {
		report.TotalReturnPct = (finalEquity - initialCapital) / initialCapital
	}

	// 1. Trade-level statistics
	var totalProfit, totalLoss float64
	var totalWinPct, totalLossPct float64
	var totalHoldDays int
	var totalCommission float64

	for _, t := range trades {
		totalCommission += t.CommissionPaid
		totalHoldDays += t.HoldDays

		if t.NetPnL > 0 {
			report.WinningTrades++
			totalProfit += t.NetPnL
			totalWinPct += t.ReturnPct
		} else {
			report.LosingTrades++
			totalLoss += math.Abs(t.NetPnL)
			totalLossPct += math.Abs(t.ReturnPct)
		}
	}

	report.TotalCommissionPaid = totalCommission

	if report.TotalTrades > 0 {
		report.WinRate = float64(report.WinningTrades) / float64(report.TotalTrades)
		report.AvgHoldingDays = float64(totalHoldDays) / float64(report.TotalTrades)
	}

	if report.WinningTrades > 0 {
		report.AvgWinAmount = totalProfit / float64(report.WinningTrades)
	}
	if report.LosingTrades > 0 {
		report.AvgLossAmount = totalLoss / float64(report.LosingTrades)
	}

	if totalLoss > 0 {
		report.ProfitFactor = totalProfit / totalLoss
	} else if totalProfit > 0 {
		report.ProfitFactor = 999.0 // Infinite profit factor
	}

	if report.AvgLossAmount > 0 {
		report.PayoffRatio = report.AvgWinAmount / report.AvgLossAmount
	}

	// 2. Daily returns & Peak-to-Trough Drawdown calculation
	var dailyReturns []float64
	peak := initialCapital
	peakDate := equityPoints[0].Date
	maxDrawdown := 0.0
	currentDrawdownDays := 0
	maxDrawdownDays := 0

	for i := 1; i < len(equityPoints); i++ {
		prev := equityPoints[i-1].TotalEquity
		curr := equityPoints[i].TotalEquity
		currDate := equityPoints[i].Date

		ret := 0.0
		if prev > 0 {
			ret = (curr - prev) / prev
		}
		dailyReturns = append(dailyReturns, ret)

		// Track running peak
		if curr >= peak {
			peak = curr
			peakDate = currDate
			currentDrawdownDays = 0
		} else if peak > 0 {
			dd := (peak - curr) / peak
			currentDrawdownDays++

			if dd > maxDrawdown {
				maxDrawdown = dd
				report.MaxDrawdownDollars = peak - curr
				report.MaxDrawdownPeakEquity = peak
				report.MaxDrawdownTroughEquity = curr
				report.MaxDrawdownPeakDate = peakDate
				report.MaxDrawdownTroughDate = currDate
			}

			if currentDrawdownDays > maxDrawdownDays {
				maxDrawdownDays = currentDrawdownDays
			}
		}
	}

	report.MaxDrawdownPct = maxDrawdown
	report.MaxDrawdownDuration = maxDrawdownDays

	// 3. Annualized metrics (Sharpe, Sortino, CAGR)
	years := report.TotalCalendarYears
	if years >= 0.5 && report.FinalEquity > 0 && report.InitialCapital > 0 {
		report.CAGR = math.Pow(report.FinalEquity/report.InitialCapital, 1.0/years) - 1.0
	} else if years > 0 {
		report.CAGR = report.TotalReturnPct / years
	}

	if len(dailyReturns) > 1 {
		// Mean
		var sumRet float64
		for _, r := range dailyReturns {
			sumRet += r
		}
		meanDailyRet := sumRet / float64(len(dailyReturns))

		// Std Dev
		var sumSqDiff float64
		var sumDownsideSqDiff float64
		for _, r := range dailyReturns {
			diff := r - meanDailyRet
			sumSqDiff += diff * diff

			if r < 0 {
				sumDownsideSqDiff += r * r
			}
		}

		stdDevDaily := math.Sqrt(sumSqDiff / float64(len(dailyReturns)-1))
		downsideStdDev := math.Sqrt(sumDownsideSqDiff / float64(len(dailyReturns)))

		// Annualized Sharpe Ratio (assuming 0% risk-free rate)
		if stdDevDaily > 0 {
			report.SharpeRatio = (meanDailyRet / stdDevDaily) * math.Sqrt(252.0)
		}

		// Annualized Sortino Ratio
		if downsideStdDev > 0 {
			report.SortinoRatio = (meanDailyRet / downsideStdDev) * math.Sqrt(252.0)
		}
	}

	return report
}
