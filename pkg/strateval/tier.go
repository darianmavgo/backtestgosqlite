package strateval

import "fmt"

// Gates are hard thresholds for tier-A eligibility (OOS-focused).
type Gates struct {
	MinOOSTrades      int
	MinOOSSharpe      float64
	MinOOSAvgTradePct float64 // decimal, e.g. 0 means > 0
	MaxOOSDDMultiple  float64 // OOS max DD may be at most this × IS max DD
}

// DefaultGates returns the v1 promote-loop defaults.
func DefaultGates() Gates {
	return Gates{
		MinOOSTrades:      30,
		MinOOSSharpe:      0,
		MinOOSAvgTradePct: 0,
		MaxOOSDDMultiple:  1.5,
	}
}

// Metrics is the slice of a PerformanceReport we persist and gate on.
type Metrics struct {
	StartDate     string
	EndDate       string
	CAGR          float64
	Sharpe        float64
	MaxDD         float64
	Trades        int
	WinRate       float64
	AvgTradePct   float64
	TotalReturnPct float64
}

// AssignTier classifies a strategy. inAllowlist marks decaying live names as D
// when they fail gates (otherwise they would just be C).
func AssignTier(is, oos Metrics, g Gates, inAllowlist bool) (tier string, reasons []string) {
	var fails []string
	if oos.Trades < g.MinOOSTrades {
		fails = append(fails, fmt.Sprintf("oos_trades=%d < %d", oos.Trades, g.MinOOSTrades))
	}
	if oos.Sharpe <= g.MinOOSSharpe {
		fails = append(fails, fmt.Sprintf("oos_sharpe=%.2f <= %.2f", oos.Sharpe, g.MinOOSSharpe))
	}
	if oos.AvgTradePct <= g.MinOOSAvgTradePct {
		fails = append(fails, fmt.Sprintf("oos_avg_trade=%.4f <= %.4f", oos.AvgTradePct, g.MinOOSAvgTradePct))
	}
	if is.MaxDD > 0 && oos.MaxDD > is.MaxDD*g.MaxOOSDDMultiple {
		fails = append(fails, fmt.Sprintf("oos_dd=%.2f%% > %.1fx is_dd=%.2f%%", oos.MaxDD*100, g.MaxOOSDDMultiple, is.MaxDD*100))
	}
	if len(fails) == 0 {
		return "A", []string{"passes OOS hard gates"}
	}
	// Marginal: positive OOS sharpe but soft fails
	if oos.Sharpe > 0 && oos.Trades >= g.MinOOSTrades/2 {
		if inAllowlist {
			return "B", append([]string{"marginal but on allowlist"}, fails...)
		}
		return "B", append([]string{"marginal"}, fails...)
	}
	if inAllowlist {
		return "D", append([]string{"on allowlist but failing OOS gates — demote candidate"}, fails...)
	}
	return "C", append([]string{"research only"}, fails...)
}
