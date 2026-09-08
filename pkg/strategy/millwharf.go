package strategy

import (
	"fmt"
	"sort"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
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
	// Delegate signal generation directly to the canonical SQLite pipeline
	if sqlStrat, exists := Get("millwharf-sql"); exists {
		return sqlStrat.GenerateSignals(barsBySymbol)
	}
	pipe := NewSQLPipelineStrategy("millwharf-pipeline", s.Name(), s.Description(), "sql/strategies/millwharf", "data/market_history.db", s.DefaultConfig())
	return pipe.GenerateSignals(barsBySymbol)
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
