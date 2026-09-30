package strategy

import (
	"fmt"
	"sort"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

type BiggestWinnerStrategy struct{}

func init() {
	Register(&BiggestWinnerStrategy{})
}

func (s *BiggestWinnerStrategy) ID() string          { return "biggest-winner" }
func (s *BiggestWinnerStrategy) Name() string        { return "Biggest Winner Backtest" }
func (s *BiggestWinnerStrategy) Description() string { return "Buys the top performing asset of the previous week and holds it for one week. Enters on Monday, exits on Friday." }

func (s *BiggestWinnerStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:            s.ID(),
		Name:          s.Name(),
		Description:   s.Description(),
		AllocationPct: 1.0,
		PositionCap:   1,
		HoldingWindow: 5,      // Roughly one trading week
		TargetPct:     999.0,  // Never exit via profit target
		StopLossPct:   0.0,    // 0 multiplier disables the stop loss
		SlippagePct:   0.0005,
	}
}

func (s *BiggestWinnerStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *BiggestWinnerStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	// Not used for pure Go strategies without intermediate calculation tables
}

func getISOWeek(dateStr string) string {
	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return ""
	}
	y, w := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", y, w)
}

func (s *BiggestWinnerStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var signals []models.Signal

	returnsByWeek := make(map[string]map[string]float64)
	firstBarOfWeek := make(map[string]map[string]models.Bar)

	// Group bars by week and calculate weekly return per symbol
	for sym, bars := range barsBySymbol {
		if len(bars) == 0 {
			continue
		}

		barsByWk := make(map[string][]models.Bar)
		for _, b := range bars {
			wk := getISOWeek(b.Date)
			if wk != "" {
				barsByWk[wk] = append(barsByWk[wk], b)
			}
		}

		for wk, wkBars := range barsByWk {
			if len(wkBars) > 0 {
				first := wkBars[0]
				last := wkBars[len(wkBars)-1]
				ret := (last.Close - first.Open) / first.Open

				if returnsByWeek[wk] == nil {
					returnsByWeek[wk] = make(map[string]float64)
				}
				returnsByWeek[wk][sym] = ret

				if firstBarOfWeek[wk] == nil {
					firstBarOfWeek[wk] = make(map[string]models.Bar)
				}
				firstBarOfWeek[wk][sym] = first
			}
		}
	}

	// Collect and sort weeks chronologically
	var weeks []string
	for wk := range firstBarOfWeek {
		weeks = append(weeks, wk)
	}
	sort.Strings(weeks)

	// For each week W, find the biggest winner in week W-1 and enter on Monday of week W, exit on Friday of week W
	for _, wkStr := range weeks {
		// Get any bar's date from this week to calculate the previous week
		var refDate string
		for _, b := range firstBarOfWeek[wkStr] {
			refDate = b.Date
			break
		}
		if refDate == "" {
			continue
		}

		t, err := time.Parse("2006-01-02", refDate)
		if err != nil {
			continue
		}

		prev := t.AddDate(0, 0, -7)
		y0, w0 := prev.ISOWeek()
		prevWkStr := fmt.Sprintf("%04d-W%02d", y0, w0)

		prevReturns, ok := returnsByWeek[prevWkStr]
		if !ok || len(prevReturns) == 0 {
			continue // No data for the previous week
		}

		var bestSym string
		bestRet := -999999.0

		for sym, ret := range prevReturns {
			if ret > bestRet {
				// Verify the symbol is still trading in the current week
				if _, hasBar := firstBarOfWeek[wkStr][sym]; hasBar {
					bestRet = ret
					bestSym = sym
				}
			}
		}

		if bestSym != "" {
			// Find Monday and Friday bars for bestSym in wkStr
			var mondayBar, fridayBar models.Bar
			hasMonday, hasFriday := false, false

			for _, b := range barsBySymbol[bestSym] {
				if getISOWeek(b.Date) == wkStr {
					bt, _ := time.Parse("2006-01-02", b.Date)
					if bt.Weekday() == time.Monday {
						mondayBar = b
						hasMonday = true
					} else if bt.Weekday() == time.Friday {
						fridayBar = b
						hasFriday = true
					}
				}
			}

			if hasMonday {
				signals = append(signals, models.Signal{
					Idx:       mondayBar.Idx,
					Symbol:    bestSym,
					Date:      mondayBar.Date,
					Open:      mondayBar.Open,
					High:      mondayBar.High,
					Low:       mondayBar.Low,
					Close:     mondayBar.Close,
					Volume:    mondayBar.Volume,
					BuyLimit:  mondayBar.Close,
					OrderType: "market",
					Entry:     1,
				})
			}

			if hasFriday {
				signals = append(signals, models.Signal{
					Idx:       fridayBar.Idx,
					Symbol:    bestSym,
					Date:      fridayBar.Date,
					Open:      fridayBar.Open,
					High:      fridayBar.High,
					Low:       fridayBar.Low,
					Close:     fridayBar.Close,
					Volume:    fridayBar.Volume,
					BuyLimit:  fridayBar.Close,
					OrderType: "market",
					Entry:     -1, // -1 denotes Exit
				})
			}
		}
	}

	// Ensure chronological order
	sort.Slice(signals, func(i, j int) bool {
		return signals[i].Date < signals[j].Date
	})

	return signals
}
