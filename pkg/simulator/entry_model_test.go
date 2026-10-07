package simulator

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

func bars(sym string, rows ...[4]float64) []models.Bar { // open, high, low, close per consecutive day
	out := make([]models.Bar, len(rows))
	for i, r := range rows {
		out[i] = models.Bar{Symbol: sym, Date: "2026-09-" + string(rune('0'+(21+i)/10)) + string(rune('0'+(21+i)%10)),
			Open: r[0], High: r[1], Low: r[2], Close: r[3]}
	}
	return out
}

func liveCfg() strategy.StrategyConfig {
	return strategy.StrategyConfig{NextDayLimitEntry: true, TakeProfitPct: 0.05, StopLossPct: 0.92}
}

func apply(cfg strategy.StrategyConfig, sig models.Signal, b []models.Bar) []models.Signal {
	return ApplyLiveEntryModel([]models.Signal{sig}, map[string][]models.Bar{"MARA": b}, func(models.Signal) strategy.StrategyConfig { return cfg })
}

func TestLiveEntryFillsAtLimitWhenNextBarTradesThrough(t *testing.T) {
	b := bars("MARA", [4]float64{20, 21, 19.5, 20}, [4]float64{20.5, 21, 19.8, 20.2}) // D=09-21, D+1=09-22
	got := apply(liveCfg(), models.Signal{Symbol: "MARA", Date: b[0].Date, Close: 20, BuyLimit: 20}, b)
	if len(got) != 1 {
		t.Fatalf("signals = %d, want 1", len(got))
	}
	s := got[0]
	if s.Date != b[1].Date || s.BuyLimit != 20 || s.Close != 20 {
		t.Fatalf("got date=%s limit=%v close=%v", s.Date, s.BuyLimit, s.Close)
	}
	if s.TakeProfit != 21 || s.StopLoss < 18.39 || s.StopLoss > 18.41 {
		t.Fatalf("TP/SL anchored to the limit: tp=%v sl=%v", s.TakeProfit, s.StopLoss)
	}
}

func TestLiveEntryFillsAtOpenWhenOpenBelowLimit(t *testing.T) {
	b := bars("MARA", [4]float64{20, 21, 19.5, 20}, [4]float64{19.4, 20, 19, 19.9})
	got := apply(liveCfg(), models.Signal{Symbol: "MARA", Date: b[0].Date, Close: 20, BuyLimit: 20}, b)
	if len(got) != 1 || got[0].BuyLimit != 19.4 {
		t.Fatalf("want fill at the 19.4 open, got %+v", got)
	}
	if got[0].TakeProfit != 21 { // still 5% over the 20 limit, not over the 19.4 fill
		t.Fatalf("take-profit must stay anchored to the limit, got %v", got[0].TakeProfit)
	}
}

func TestLiveEntryDroppedWhenNextBarNeverReachesLimit(t *testing.T) {
	b := bars("MARA", [4]float64{20, 21, 19.5, 20}, [4]float64{21, 22, 20.5, 21.5}) // gaps up, low above the limit
	if got := apply(liveCfg(), models.Signal{Symbol: "MARA", Date: b[0].Date, Close: 20, BuyLimit: 20}, b); len(got) != 0 {
		t.Fatalf("a gap-up day must not fill, got %+v", got)
	}
}

func TestLiveEntryNoNextBarDropsSignal(t *testing.T) {
	b := bars("MARA", [4]float64{20, 21, 19.5, 20})
	if got := apply(liveCfg(), models.Signal{Symbol: "MARA", Date: b[0].Date, Close: 20, BuyLimit: 20}, b); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestLiveEntryStopLessStrategyGetsCrisisStop(t *testing.T) {
	cfg := liveCfg()
	cfg.StopLossPct = 0
	b := bars("MARA", [4]float64{20, 21, 19.5, 20}, [4]float64{20, 21, 19.5, 20})
	got := apply(cfg, models.Signal{Symbol: "MARA", Date: b[0].Date, Close: 20, BuyLimit: 20}, b)
	if len(got) != 1 || got[0].StopLoss < 15.99 || got[0].StopLoss > 16.01 {
		t.Fatalf("want crisis stop 16.00, got %+v", got)
	}
}

func TestLegacyEntryUntouchedWhenFlagOff(t *testing.T) {
	cfg := liveCfg()
	cfg.NextDayLimitEntry = false
	b := bars("MARA", [4]float64{20, 21, 19.5, 20}, [4]float64{21, 22, 20.5, 21.5})
	in := models.Signal{Symbol: "MARA", Date: b[0].Date, Close: 20, BuyLimit: 20}
	got := apply(cfg, in, b)
	if len(got) != 1 || got[0].Date != in.Date || got[0].BuyLimit != 20 {
		t.Fatalf("flag off must pass through, got %+v", got)
	}
}

func openCfg() strategy.StrategyConfig {
	return strategy.StrategyConfig{NextDayOpenEntry: true, TakeProfitPct: 0.05, StopLossPct: 0.92}
}

func TestOpenEntryBuysAtNextOpenAndMeasuresTakeProfitFromTheFill(t *testing.T) {
	// D closes at 20 but D+1 opens at 22: a market order fills at 22, a limit at 20 would not fill.
	b := bars("MARA", [4]float64{20, 21, 19.5, 20}, [4]float64{22, 23, 21.5, 22.5})
	got := apply(openCfg(), models.Signal{Symbol: "MARA", Date: b[0].Date, Close: 20, BuyLimit: 20, OrderType: "limit"}, b)
	if len(got) != 1 {
		t.Fatalf("signals = %d, want 1 (a gap up must still fill)", len(got))
	}
	s := got[0]
	if s.Date != b[1].Date || s.Close != 22 || s.BuyLimit != 22 || s.OrderType != "market" {
		t.Fatalf("got date=%s close=%v limit=%v type=%s, want the next day at the 22 open as a market order", s.Date, s.Close, s.BuyLimit, s.OrderType)
	}
	if s.TakeProfit < 23.09 || s.TakeProfit > 23.11 { // 22 * 1.05
		t.Fatalf("take-profit must be 5%% over the 22 fill, got %v", s.TakeProfit)
	}
	if s.StopLoss < 20.23 || s.StopLoss > 20.25 { // 22 * 0.92
		t.Fatalf("stop must come from the fill, got %v", s.StopLoss)
	}
}

func TestOpenEntryKeepsASignalStopAndAbsoluteTarget(t *testing.T) {
	b := bars("MARA", [4]float64{20, 21, 19.5, 20}, [4]float64{20.5, 21, 19.8, 20.2})
	got := apply(openCfg(), models.Signal{Symbol: "MARA", Date: b[0].Date, Close: 20, StopLoss: 18, TakeProfit: 30}, b)
	if len(got) != 1 || got[0].StopLoss != 18 || got[0].TakeProfit != 30 {
		t.Fatalf("a signal's own stop and target must stand, got %+v", got)
	}
}

func TestOpenEntryWinsOverLimitEntryAndDropsWithoutANextBar(t *testing.T) {
	cfg := openCfg()
	cfg.NextDayLimitEntry = true
	b := bars("MARA", [4]float64{20, 21, 19.5, 20}, [4]float64{22, 23, 21.5, 22.5})
	if got := apply(cfg, models.Signal{Symbol: "MARA", Date: b[0].Date, Close: 20, BuyLimit: 20}, b); len(got) != 1 || got[0].Close != 22 {
		t.Fatalf("open entry must win over the limit model, got %+v", got)
	}
	if got := apply(cfg, models.Signal{Symbol: "MARA", Date: b[1].Date, Close: 22.5}, b); len(got) != 0 {
		t.Fatalf("no next session, got %+v", got)
	}
}
