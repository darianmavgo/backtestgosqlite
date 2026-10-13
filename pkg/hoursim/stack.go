package hoursim

import (
	"math"

	"backtestgo/pkg/sim"
	"github.com/darianmavgo/backtestgosqlite/pkg/analytics"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

// Member is one strategy of a shared-account stack. Priority 0 is the primary: it sizes against
// the whole account and may sell a subordinate's position for cash or for its symbol. Subordinates
// (Priority above 0) trade the cash that is left and never push each other out.
type Member struct {
	ID       string
	Priority int
	Config   strategy.StrategyConfig
	Signals  []models.Signal
}

// StackInput is a shared-account run: every member's signals over one set of bars and one cash
// balance, whose starting size is Capital.
type StackInput struct {
	Members   []Member
	Daily     map[string][]models.Bar
	Hourly    map[string][]models.Bar
	Dates     []string
	Capital   float64
	Benchmark map[string]models.Bar
}

// StackOutput is the combined run and each member's own report.
type StackOutput struct {
	Output
	PerStrategy map[string]models.PerformanceReport
	// Preempted counts the positions the primary sold to make room.
	Preempted int
}

// RunStack plays the members' signals over hourly bars in one shared account.
func RunStack(in StackInput) (StackOutput, error) {
	sleeves := make([]sim.Sleeve, 0, len(in.Members))
	var all []models.Signal
	for _, m := range in.Members {
		if err := Supported(m.Config); err != nil {
			return StackOutput{}, err
		}
		sleeves = append(sleeves, sim.Sleeve{ID: m.ID, Priority: m.Priority, Cfg: Config(m.Config, in.Capital), Sigs: Signals(m.Signals)})
		all = append(all, m.Signals...)
	}
	first := strategy.StrategyConfig{}
	if len(in.Members) > 0 {
		first = in.Members[0].Config
	}
	bars, daily := barsFor(Input{Daily: in.Daily, Hourly: in.Hourly}, all)
	res := sim.RunPortfolio(sleeves, bars, in.Dates)
	out := finish(Input{Config: first, Benchmark: in.Benchmark}, res, daily)

	// Commission is per sleeve; finish used the primary's rate for every trade.
	rate := map[string]float64{}
	for _, m := range in.Members {
		rate[m.ID] = m.Config.CommissionPerShare
	}
	for i, t := range res.Trades {
		out.Trades[i].CommissionPaid = 2 * float64(t.Shares) * rate[t.Strategy]
	}
	out.Report = analytics.CalculatePerformanceMetricsWithBenchmark(res.Initial, out.Trades, out.Curve, in.Benchmark)

	so := StackOutput{Output: out, PerStrategy: map[string]models.PerformanceReport{}}
	for _, t := range res.Trades {
		if t.Reason == "preempted" {
			so.Preempted++
		}
	}
	for _, m := range in.Members {
		so.PerStrategy[m.ID] = sleeveReport(m.ID, res.Initial, out.Trades, out.Curve, in.Benchmark)
	}
	return so, nil
}

// sleeveReport rebuilds one member's report from its own trades: its equity is the starting
// capital plus its cumulative net profit, marked on the day each trade exited.
func sleeveReport(id string, capital float64, all []models.Trade, curve []models.DailyEquityPoint, bm map[string]models.Bar) models.PerformanceReport {
	var trades []models.Trade
	var net float64
	byDate := map[string]float64{}
	for _, t := range all {
		if t.StrategyID == id {
			trades = append(trades, t)
			net += t.NetPnL
			byDate[t.ExitDate] += t.NetPnL
		}
	}
	var sc []models.DailyEquityPoint
	cum, peak := 0.0, capital
	for _, pt := range curve {
		cum += byDate[pt.Date]
		eq := math.Max(0, capital+cum)
		peak = math.Max(peak, eq)
		dd := 0.0
		if peak > 0 {
			dd = (peak - eq) / peak
		}
		sc = append(sc, models.DailyEquityPoint{Date: pt.Date, TotalEquity: eq, DrawdownPct: dd})
	}
	rep := analytics.CalculatePerformanceMetricsWithBenchmark(capital, trades, sc, bm)
	rep.NetProfit = net
	rep.FinalEquity = capital + net
	if capital > 0 {
		rep.TotalReturnPct = net / capital
	}
	if years := rep.TotalCalendarYears; years >= 0.5 && rep.FinalEquity > 0 && rep.InitialCapital > 0 {
		rep.CAGR = math.Pow(rep.FinalEquity/rep.InitialCapital, 1/years) - 1
	}
	return rep
}
