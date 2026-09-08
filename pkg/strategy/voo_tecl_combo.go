package strategy

import (
	"sort"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

// VOOTECLCombo implements the All-Weather Dual Combo strategy:
//
//   - LONG TECL when VOO closes down 3 consecutive days
//     (+5% take-profit, 0% stop-loss, 8-day max hold, 65% allocation)
//
//   - SHORT SPXU when VOO closes up 3 consecutive days AND VOO is below its SMA200
//     (+6% take-profit, -5% stop-loss, 2-day max hold, 65% allocation)
//
// Priority rule: if both signals fire on the same day, LONG takes priority.
// Regime filtering (VOO < SMA200) is performed inside GenerateSignals — the
// engine receives only pre-filtered signals and does not know about regimes.
//
// T-bill yield on idle cash is configured via DefaultConfig().CashYieldAnnual
// and applied by the PortfolioSimulator.
type VOOTECLCombo struct{}

// NewVOOTECLCombo constructs and auto-registers the strategy.
func NewVOOTECLCombo() *VOOTECLCombo {
	s := &VOOTECLCombo{}
	Register(s)
	return s
}

func (s *VOOTECLCombo) ID() string { return "voo-tecl-combo" }

func (s *VOOTECLCombo) Name() string { return "VOO→TECL All-Weather Combo" }

func (s *VOOTECLCombo) Description() string {
	return "Long TECL on 3-consecutive VOO down-closes (+5% TP / 8d hold) " +
		"combined with Short SPXU on 3-consecutive VOO up-closes in bear markets " +
		"(VOO < SMA200, +6% TP / -5% SL / 2d hold). Long takes priority on same-day conflicts. " +
		"65% allocation per trade. 4.5% T-bill yield on idle cash."
}

func (s *VOOTECLCombo) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

// DefaultConfig returns the canonical VOO-TECL combo parameters.
func (s *VOOTECLCombo) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		Benchmark:          "VOO",
		AllocationPct:      0.65,
		TakeProfitPct:      0.05,  // Long leg default; short overrides per-signal
		StopLossPct:        0.00,  // Long leg: no stop-loss
		HoldingWindow:      8,     // Long leg default; short overrides per-signal (2)
		PositionCap:        1,     // One open position at a time
		CashYieldAnnual:    0.045,
		SlippagePct:        0.0,
		CommissionPerShare: 0.0,
	}
}

// GenerateSignals detects entry signals for both legs of the combo.
//
// Required keys in barsBySymbol:
//   - "VOO"  — signal bars; Bar.SMA200 populated when regime filtering is needed
//   - "TECL" — long trade bars
//   - "SPXU" — short trade bars
//
// SMA200 is read from Bar.SMA200 (set by the storage layer). If SMA200 is 0,
// regime gate is treated as open ("All Regimes").
//
// Returns []models.Signal sorted chronologically. Each signal has:
//   - Symbol: "TECL" (LONG) or "SPXU" (SHORT)
//   - Direction: "LONG" or "SHORT"
//   - TakeProfit: absolute target price for the leg
//   - StopLoss: absolute stop price (0 for the long leg — no stop)
//   - HoldDaysOverride: 8 (long) or 2 (short)
//   - Regime: observability label
func (s *VOOTECLCombo) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	vooBars := barsBySymbol["VOO"]
	teclBars := barsBySymbol["TECL"]
	spxuBars := barsBySymbol["SPXU"]

	if len(vooBars) < 4 || len(teclBars) == 0 || len(spxuBars) == 0 {
		return nil
	}

	// Index TECL and SPXU bars by date for O(1) price lookup.
	teclByDate := make(map[string]models.Bar, len(teclBars))
	for _, b := range teclBars {
		teclByDate[b.Date] = b
	}
	spxuByDate := make(map[string]models.Bar, len(spxuBars))
	for _, b := range spxuBars {
		spxuByDate[b.Date] = b
	}

	// Long-leg parameters
	const longConsecutiveDays = 3
	const longHoldDays = 8
	const longTPPct = 0.05 // +5%

	// Short-leg parameters
	const shortConsecutiveDays = 3
	const shortHoldDays = 2
	const shortTPPct = 0.06 // +6%
	const shortSLPct = 0.05 // -5%

	var signals []models.Signal

	for i := longConsecutiveDays; i < len(vooBars); i++ {
		date := vooBars[i].Date
		vooClose := vooBars[i].Close

		// Detect long signal: 3 consecutive VOO down-closes.
		wantLong := consecutiveDrops(vooBars, i, longConsecutiveDays)

		// Detect short signal: 3 consecutive VOO up-closes + VOO < SMA200.
		wantShort := false
		if consecutiveRallies(vooBars, i, shortConsecutiveDays) {
			sma200 := vooBars[i].SMA200
			// If SMA200 == 0, data not available → treat as no gate (allow short).
			if sma200 <= 0 || vooClose < sma200 {
				wantShort = true
			}
		}

		// Priority rule: LONG beats SHORT if both fire on the same day.
		if wantLong {
			teclBar, ok := teclByDate[date]
			if !ok || teclBar.Close <= 0 {
				continue
			}
			entryPrice := teclBar.Close
			signals = append(signals, models.Signal{
				Symbol:           "TECL",
				Date:             date,
				Open:             teclBar.Open,
				High:             teclBar.High,
				Low:              teclBar.Low,
				Close:            entryPrice,
				Volume:           teclBar.Volume,
				Entry:            1,
				Direction:        "LONG",
				Regime:           "All Regimes",
				HoldDaysOverride: longHoldDays,
				TakeProfit:       entryPrice * (1.0 + longTPPct),
				// No StopLoss for the long leg by design.
			})
		} else if wantShort {
			spxuBar, ok := spxuByDate[date]
			if !ok || spxuBar.Close <= 0 {
				continue
			}
			entryPrice := spxuBar.Close
			regimeLabel := "All Regimes"
			if vooBars[i].SMA200 > 0 {
				regimeLabel = "VOO<SMA200"
			}
			signals = append(signals, models.Signal{
				Symbol:           "SPXU",
				Date:             date,
				Open:             spxuBar.Open,
				High:             spxuBar.High,
				Low:              spxuBar.Low,
				Close:            entryPrice,
				Volume:           spxuBar.Volume,
				Entry:            1,
				Direction:        "SHORT",
				Regime:           regimeLabel,
				HoldDaysOverride: shortHoldDays,
				TakeProfit:       entryPrice * (1.0 + shortTPPct),
				StopLoss:         entryPrice * (1.0 - shortSLPct),
			})
		}
	}

	sort.Slice(signals, func(i, j int) bool {
		return signals[i].Date < signals[j].Date
	})
	return signals
}

// consecutiveDrops returns true if bars[idx] through bars[idx-n] each closed
// lower than the preceding bar (n consecutive down-closes ending at idx).
func consecutiveDrops(bars []models.Bar, idx, n int) bool {
	if idx < n {
		return false
	}
	for s := 0; s < n; s++ {
		if bars[idx-s].Close >= bars[idx-s-1].Close {
			return false
		}
	}
	return true
}

// consecutiveRallies returns true if bars[idx] through bars[idx-n] each closed
// higher than the preceding bar (n consecutive up-closes ending at idx).
func consecutiveRallies(bars []models.Bar, idx, n int) bool {
	if idx < n {
		return false
	}
	for s := 0; s < n; s++ {
		if bars[idx-s].Close <= bars[idx-s-1].Close {
			return false
		}
	}
	return true
}

func init() {
	Register(NewVOOTECLCombo())
}
