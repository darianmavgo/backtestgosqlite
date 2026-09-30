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

func (s *BiggestWinnerStrategy) ID() string   { return "biggest-winner" }
func (s *BiggestWinnerStrategy) Name() string { return "Biggest Winner Backtest" }
func (s *BiggestWinnerStrategy) Description() string {
	return "Buys the top performing asset of the previous calendar year and holds it for one year."
}

func (s *BiggestWinnerStrategy) DefaultConfig() StrategyConfig {
	return annualHoldConfig(s.ID(), s.Name(), s.Description())
}

func (s *BiggestWinnerStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *BiggestWinnerStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	// Not used for pure Go strategies without intermediate calculation tables
}

func (s *BiggestWinnerStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	return annualWinnerSignals(barsBySymbol, annualLong)
}

// annualSide is how the prior year's biggest winner is traded the next year.
type annualSide int

const (
	annualLong annualSide = iota
	annualShort
	annualInverse
)

func annualHoldConfig(id, name, desc string) StrategyConfig {
	return StrategyConfig{
		ID:            id,
		Name:          name,
		Description:   desc,
		AllocationPct: 1.0,
		PositionCap:   1,
		HoldingWindow: 252,   // Safety cap. The year-end exit signal is the real close.
		TargetPct:     999.0, // Never exit via profit target
		StopLossPct:   0.0,   // 0 multiplier disables the stop loss
		SlippagePct:   0.0005,
	}
}

// annualWinnerSignals ranks every symbol by its calendar-year return
// (last close vs first open) and, on the first session of the next year,
// trades that winner until the last session of the hold year.
//
// annualInverse buys InverseETF(winner) on that same session, and emits
// nothing for a year whose winner has no matched inverse (or whose inverse
// did not trade that day). The account stays in cash for the skipped year.
func annualWinnerSignals(barsBySymbol map[string][]models.Bar, side annualSide) []models.Signal {
	var signals []models.Signal

	returnsByYear := make(map[string]map[string]float64)
	firstBarOfYear := make(map[string]map[string]models.Bar)

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
			if len(yrBars) == 0 {
				continue
			}
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

	var years []string
	for yr := range firstBarOfYear {
		years = append(years, yr)
	}
	sort.Strings(years)

	for _, yrStr := range years {
		yrInt, err := strconv.Atoi(yrStr)
		if err != nil {
			continue
		}
		prevReturns := returnsByYear[strconv.Itoa(yrInt-1)]
		if len(prevReturns) == 0 {
			continue
		}

		var bestSym string
		bestRet := -999999.0
		for sym, ret := range prevReturns {
			if ret > bestRet {
				if _, hasBar := firstBarOfYear[yrStr][sym]; hasBar {
					bestRet = ret
					bestSym = sym
				}
			}
		}
		if bestSym == "" {
			continue
		}

		winnerEntry := firstBarOfYear[yrStr][bestSym]
		tradeSym := bestSym
		direction := ""
		switch side {
		case annualShort:
			direction = "SHORT"
		case annualInverse:
			inv, ok := InverseETF(bestSym)
			if !ok {
				continue
			}
			bar, ok := findBarOnDate(barsBySymbol[inv], winnerEntry.Date)
			if !ok {
				continue
			}
			tradeSym = inv
			winnerEntry = bar
		}

		signals = append(signals, signalFromBar(winnerEntry, 1, direction))

		var exitBar models.Bar
		var maxDate string
		for _, b := range barsBySymbol[tradeSym] {
			if len(b.Date) >= 4 && b.Date[:4] == yrStr && b.Date > maxDate {
				maxDate = b.Date
				exitBar = b
			}
		}
		if exitBar.Date != "" && exitBar.Date != winnerEntry.Date {
			signals = append(signals, signalFromBar(exitBar, -1, direction))
		}
	}

	sort.Slice(signals, func(i, j int) bool {
		if signals[i].Date == signals[j].Date {
			return signals[i].Entry < signals[j].Entry // exit before a same-day entry
		}
		return signals[i].Date < signals[j].Date
	})
	return signals
}

func signalFromBar(b models.Bar, entry int, direction string) models.Signal {
	return models.Signal{
		Idx:       b.Idx,
		Symbol:    b.Symbol,
		Date:      b.Date,
		Open:      b.Open,
		High:      b.High,
		Low:       b.Low,
		Close:     b.Close,
		Volume:    b.Volume,
		BuyLimit:  b.Close,
		OrderType: "market",
		Entry:     entry,
		Direction: direction,
	}
}

func findBarOnDate(bars []models.Bar, date string) (models.Bar, bool) {
	for _, b := range bars {
		if b.Date == date {
			return b, true
		}
		if b.Date > date {
			break
		}
	}
	return models.Bar{}, false
}
