package strategy

import (
	"fmt"
	"sort"
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// DeclineStreak represents a continuous sequence of trading days where each close is lower than the previous close.
type DeclineStreak struct {
	Symbol       string  `json:"symbol"`
	StreakDays   int     `json:"streak_days"`    // Number of consecutive days close < prev close
	StartDate    string  `json:"start_date"`     // Date of the peak before decline started
	EndDate      string  `json:"end_date"`       // Date of the trough/last consecutive down close
	StartClose   float64 `json:"start_close"`    // Close price on peak date
	EndClose     float64 `json:"end_close"`      // Close price on trough date
	TotalDropPct float64 `json:"total_drop_pct"` // Total percentage drop from peak to trough
	EndIdx       int     `json:"end_idx"`        // Index of the end bar
}

// MillwharfWeeklyCandidate holds an eligible signal candidate before weekly ranking.
type MillwharfWeeklyCandidate struct {
	Symbol       string
	Bar          models.Bar
	StreakDays   int
	PeakClose    float64
	TotalDropPct float64
	High6d       float64
	TakeProfit   float64
	WeekKey      string
}

// MillwharfStrategy implements the Millwharf Weekly Consistent Decline Reversal strategy.
// Strategy Rules:
// 1. Every week, scan the stock/ETF universe for symbols in a consistent decline (each close < previous close).
// 2. Filter for declines lasting at least 5 consecutive trading days (Streak >= 5).
// 3. Select the symbol with the longest consistent decline in that week and start a position.
// 4. Take Profit: Min(High of the last 6 days, Entry Close * 1.20).
// 5. Stop Loss: None (holds through drawdowns).
// 6. Time Exit: Exits unconditionally at market open after holding for 4 trading days.
type MillwharfStrategy struct {
	MinStreak        int     // Minimum consecutive declining closes required (default: 5)
	TakeProfitLookback int   // Lookback window for highest high (default: 6 days)
	MaxProfitCap     float64 // Maximum take-profit cap multiplier (default: 1.20 for +20%)
	HoldingWindow    int     // Number of holding days before market open exit (default: 4)
}

func init() {
	Register(&MillwharfStrategy{
		MinStreak:          5,
		TakeProfitLookback: 6,
		MaxProfitCap:       1.20,
		HoldingWindow:      4,
	})
}

func (s *MillwharfStrategy) ID() string {
	return "millwharf"
}

func (s *MillwharfStrategy) Name() string {
	return "Millwharf Weekly Consistent Decline"
}

func (s *MillwharfStrategy) Description() string {
	return "Every week enters the stock with the longest consistent decline (>=5 days). Take profit at last 6-day high or +20% (whichever is lower). No stop loss; exits at market open after 4-day hold."
}

func (s *MillwharfStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 "millwharf",
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          1.20,   // Up to +20% take profit
		StopLossPct:        0.0001, // Effectively no stop loss
		HoldingWindow:      4,      // 4-day holding window
		ExitAtMarketOpen:   true,   // Exit at market open on time-up
		PositionCap:        5,      // Max 5 concurrent positions
		AllocationPct:      0.20,   // 20% equity per position
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *MillwharfStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

// getISOWeekKey converts a YYYY-MM-DD date string into a sortable ISO week key (e.g. "2026-W08").
func getISOWeekKey(dateStr string) string {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		if len(dateStr) >= 10 {
			t, err = time.Parse("2006-01-02", dateStr[:10])
		}
		if err != nil {
			return dateStr
		}
	}
	year, week := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// GenerateSignals evaluates historical bars and selects the longest decline candidate each week.
func (s *MillwharfStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	minStreak := s.MinStreak
	if minStreak <= 0 {
		minStreak = 5
	}
	tpLookback := s.TakeProfitLookback
	if tpLookback <= 0 {
		tpLookback = 6
	}
	maxCap := s.MaxProfitCap
	if maxCap <= 0 {
		maxCap = 1.20
	}

	// 1. Gather all qualifying candidates (streak >= minStreak) across all symbols
	candidatesByWeek := make(map[string][]MillwharfWeeklyCandidate)

	for sym, bars := range barsBySymbol {
		if len(bars) < minStreak+1 {
			continue
		}

		streak := 0
		peakClose := bars[0].Close

		for i := 1; i < len(bars); i++ {
			if bars[i].Close < bars[i-1].Close {
				if streak == 0 {
					peakClose = bars[i-1].Close
				}
				streak++

				if streak >= minStreak {
					// Calculate highest high over the last tpLookback trading days (days i-(tpLookback-1) to i)
					startLookback := i - (tpLookback - 1)
					if startLookback < 0 {
						startLookback = 0
					}
					high6d := bars[i].High
					for k := startLookback; k <= i; k++ {
						if bars[k].High > high6d {
							high6d = bars[k].High
						}
					}

					// Take profit is the lower of high6d or +20% gain (bars[i].Close * maxCap)
					capGainPrice := bars[i].Close * maxCap
					tpPrice := high6d
					if capGainPrice < tpPrice {
						tpPrice = capGainPrice
					}

					dropPct := 0.0
					if peakClose > 0 {
						dropPct = (bars[i].Close - peakClose) / peakClose * 100.0
					}

					weekKey := getISOWeekKey(bars[i].Date)
					candidatesByWeek[weekKey] = append(candidatesByWeek[weekKey], MillwharfWeeklyCandidate{
						Symbol:       sym,
						Bar:          bars[i],
						StreakDays:   streak,
						PeakClose:    peakClose,
						TotalDropPct: dropPct,
						High6d:       high6d,
						TakeProfit:   tpPrice,
						WeekKey:      weekKey,
					})
				}
			} else {
				streak = 0
				peakClose = bars[i].Close
			}
		}
	}

	// 2. For each week, pick the stock with the longest millwharf decline
	var signals []models.Signal

	for _, cands := range candidatesByWeek {
		if len(cands) == 0 {
			continue
		}

		// Sort candidates in this week:
		// - StreakDays DESC (longest decline first)
		// - TotalDropPct ASC (largest drop percentage as tie-breaker)
		// - Date ASC
		sort.Slice(cands, func(i, j int) bool {
			if cands[i].StreakDays != cands[j].StreakDays {
				return cands[i].StreakDays > cands[j].StreakDays
			}
			if cands[i].TotalDropPct != cands[j].TotalDropPct {
				return cands[i].TotalDropPct < cands[j].TotalDropPct
			}
			return cands[i].Bar.Date < cands[j].Bar.Date
		})

		best := cands[0]
		signals = append(signals, models.Signal{
			Idx:        best.Bar.Idx,
			Symbol:     best.Symbol,
			Date:       best.Bar.Date,
			Open:       best.Bar.Open,
			High:       best.Bar.High,
			Low:        best.Bar.Low,
			Close:      best.Bar.Close,
			Volume:     best.Bar.Volume,
			BuyLimit:   best.Bar.Close,
			OrderType:  "market",
			TakeProfit: best.TakeProfit,
			StopLoss:   0.0001, // No stop loss
			Entry:      1,
			Metadata: map[string]float64{
				"decline_streak": float64(best.StreakDays),
				"high_6d":        best.High6d,
				"take_profit":    best.TakeProfit,
				"total_drop_pct": best.TotalDropPct,
				"target_pct":     (best.TakeProfit - best.Bar.Close) / best.Bar.Close,
			},
		})
	}

	// 3. Sort final signals chronologically
	sort.Slice(signals, func(i, j int) bool {
		if signals[i].Date == signals[j].Date {
			return signals[i].Symbol < signals[j].Symbol
		}
		return signals[i].Date < signals[j].Date
	})

	return signals
}

// FindLongestDeclines searches through all symbols and returns all distinct consecutive decline streaks
// that occurred on or after startDate (formatted as YYYY-MM-DD). If startDate is empty, all history is searched.
// The returned slice is sorted in descending order by streak length, then by total drop percentage.
func FindLongestDeclines(barsBySymbol map[string][]models.Bar, startDate string, minStreak int) []DeclineStreak {
	if minStreak <= 0 {
		minStreak = 1
	}

	var allStreaks []DeclineStreak

	for sym, bars := range barsBySymbol {
		if len(bars) < 2 {
			continue
		}

		currentStreak := 0
		var peakDate string
		var peakClose float64

		for i := 1; i < len(bars); i++ {
			if bars[i].Close < bars[i-1].Close {
				if currentStreak == 0 {
					peakDate = bars[i-1].Date
					peakClose = bars[i-1].Close
				}
				currentStreak++
			} else {
				if currentStreak >= minStreak {
					endDate := bars[i-1].Date
					endClose := bars[i-1].Close

					// Check date filter: include if the streak ends on or after startDate
					if startDate == "" || endDate >= startDate {
						dropPct := 0.0
						if peakClose > 0 {
							dropPct = (endClose - peakClose) / peakClose * 100.0
						}
						allStreaks = append(allStreaks, DeclineStreak{
							Symbol:       sym,
							StreakDays:   currentStreak,
							StartDate:    peakDate,
							EndDate:      endDate,
							StartClose:   peakClose,
							EndClose:     endClose,
							TotalDropPct: dropPct,
							EndIdx:       bars[i-1].Idx,
						})
					}
				}
				currentStreak = 0
			}
		}

		// Handle streak still ongoing at the end of the series
		if currentStreak >= minStreak {
			n := len(bars)
			endDate := bars[n-1].Date
			endClose := bars[n-1].Close
			if startDate == "" || endDate >= startDate {
				dropPct := 0.0
				if peakClose > 0 {
					dropPct = (endClose - peakClose) / peakClose * 100.0
				}
				allStreaks = append(allStreaks, DeclineStreak{
					Symbol:       sym,
					StreakDays:   currentStreak,
					StartDate:    peakDate,
					EndDate:      endDate,
					StartClose:   peakClose,
					EndClose:     endClose,
					TotalDropPct: dropPct,
					EndIdx:       bars[n-1].Idx,
				})
			}
		}
	}

	// Sort by StreakDays DESC, then TotalDropPct ASC (largest drop first)
	sort.Slice(allStreaks, func(i, j int) bool {
		if allStreaks[i].StreakDays != allStreaks[j].StreakDays {
			return allStreaks[i].StreakDays > allStreaks[j].StreakDays
		}
		if allStreaks[i].TotalDropPct != allStreaks[j].TotalDropPct {
			return allStreaks[i].TotalDropPct < allStreaks[j].TotalDropPct
		}
		return allStreaks[i].EndDate > allStreaks[j].EndDate
	})

	return allStreaks
}
