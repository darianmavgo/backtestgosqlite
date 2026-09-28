package strategy

import (
	"sort"
	"strconv"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

type BiggestWinnerStrategy struct{}

func init() {
	Register(&BiggestWinnerStrategy{})
}

func (s *BiggestWinnerStrategy) ID() string          { return "biggest-winner" }
func (s *BiggestWinnerStrategy) Name() string        { return "Biggest Winner Backtest" }
func (s *BiggestWinnerStrategy) Description() string { return "Buys the top performing asset of the previous calendar year and holds it for one year." }

func (s *BiggestWinnerStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:            s.ID(),
		Name:          s.Name(),
		Description:   s.Description(),
		AllocationPct: 1.0,
		PositionCap:   1,
		HoldingWindow: 252,    // Roughly one trading year
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

func (s *BiggestWinnerStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	var signals []models.Signal

	returnsByYear := make(map[string]map[string]float64)
	firstBarOfYear := make(map[string]map[string]models.Bar)

	// Group bars by year and calculate annual return per symbol
	for sym, bars := range barsBySymbol {
		if len(bars) == 0 {
			continue
		}

		barsByYr := make(map[string][]models.Bar)
		for _, b := range bars {
			if len(b.Date) >= 4 {
				yr := b.Date[:4]
				barsByYr[yr] = append(barsByYr[yr], b)
			}
		}

		for yr, yrBars := range barsByYr {
			if len(yrBars) > 0 {
				first := yrBars[0]
				last := yrBars[len(yrBars)-1]
				ret := (last.Close - first.Open) / first.Open

				if returnsByYear[yr] == nil {
					returnsByYear[yr] = make(map[string]float64)
				}
				returnsByYear[yr][sym] = ret

				if firstBarOfYear[yr] == nil {
					firstBarOfYear[yr] = make(map[string]models.Bar)
				}
				firstBarOfYear[yr][sym] = first
			}
		}
	}

	// Collect and sort years chronologically
	var years []string
	for yr := range firstBarOfYear {
		years = append(years, yr)
	}
	sort.Strings(years)

	// For each year Y, find the biggest winner in year Y-1 and enter on the first day of year Y
	for _, yrStr := range years {
		yrInt, err := strconv.Atoi(yrStr)
		if err != nil {
			continue
		}

		prevYrStr := strconv.Itoa(yrInt - 1)
		prevReturns, ok := returnsByYear[prevYrStr]
		if !ok || len(prevReturns) == 0 {
			continue // No data for the previous year
		}

		var bestSym string
		bestRet := -999999.0

		for sym, ret := range prevReturns {
			if ret > bestRet {
				// Verify the symbol is still trading in the current year
				if _, hasBar := firstBarOfYear[yrStr][sym]; hasBar {
					bestRet = ret
					bestSym = sym
				}
			}
		}

		if bestSym != "" {
			entryBar := firstBarOfYear[yrStr][bestSym]
			signals = append(signals, models.Signal{
				Idx:       entryBar.Idx,
				Symbol:    bestSym,
				Date:      entryBar.Date,
				Open:      entryBar.Open,
				High:      entryBar.High,
				Low:       entryBar.Low,
				Close:     entryBar.Close,
				Volume:    entryBar.Volume,
				BuyLimit:  entryBar.Close,
				OrderType: "market",
				Entry:     1,
			})

			// We need an exit signal at the very end of the year to free up PositionCap
			// Find the last available bar for this symbol in this year
			var exitBar models.Bar
			var maxDate string
			for _, b := range barsBySymbol[bestSym] {
				if len(b.Date) >= 4 && b.Date[:4] == yrStr {
					if b.Date > maxDate {
						maxDate = b.Date
						exitBar = b
					}
				}
			}

			// If we found a valid last bar of the year, append a sell signal
			if exitBar.Date != "" {
				signals = append(signals, models.Signal{
					Idx:       exitBar.Idx,
					Symbol:    bestSym,
					Date:      exitBar.Date,
					Open:      exitBar.Open,
					High:      exitBar.High,
					Low:       exitBar.Low,
					Close:     exitBar.Close,
					Volume:    exitBar.Volume,
					BuyLimit:  exitBar.Close,
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
