package study

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// VOOUp3ETFStudy buys every ETF in the reference DB's "sweep" universe the day
// VOO closes up 3 days in a row, with ONE fixed exit rule (3-day hold, +5% TP,
// -10% SL), and writes a per-ETF comparison to the results DB. There is no
// parameter grid: every ETF gets the same trade, so results are comparable.
type VOOUp3ETFStudy struct {
	marketDBPath  string
	resultsDBPath string
}

func init() {
	Register(&VOOUp3ETFStudy{})
}

func (s *VOOUp3ETFStudy) ID() string { return "voo_up3_etf" }

func (s *VOOUp3ETFStudy) Name() string {
	return "VOO 3-Up-Day ETF Comparison (3d hold / +5% TP / -10% SL)"
}

func (s *VOOUp3ETFStudy) Description() string {
	return "Signal: VOO closes up 3 days in a row. Buy each ETF in the reference DB sweep " +
		"universe at that close; exit on +5% TP, -10% SL or after 3 days. Fixed parameters, " +
		"bounded worker pool. Compare ETFs via the etf_compare view in the results DB."
}

func (s *VOOUp3ETFStudy) SetDatabases(marketDBPath, resultsDBPath string) {
	s.marketDBPath = marketDBPath
	s.resultsDBPath = resultsDBPath
}

const (
	vooUp3GainDays   = 3
	vooUp3Hold       = 3
	vooUp3TP         = 0.05
	vooUp3SL         = 0.10
	vooUp3MinBars    = 250
	vooUp3Capital    = 100000.0
	vooUp3Alloc      = 0.65
	vooUp3Yield      = 0.045
	vooUp3MaxWorkers = 8
)

// vooUp3Row is one ETF's outcome. Percent fields are in percent units.
type vooUp3Row struct {
	Symbol       string
	Status       string // ok | insufficient_bars | no_signals | no_trades | error
	Bars         int
	FirstBar     string
	SignalDays   int
	Trades       int
	Wins         int
	WinRate      float64
	AvgReturn    float64
	MedianReturn float64
	BestTrade    float64
	WorstTrade   float64
	TPExits      int
	SLExits      int
	TimeExits    int
	AvgHoldDays  float64
	TotalReturn  float64
	CAGR         float64
	MaxDD        float64
	Sharpe       float64
	NetProfit    float64
	TradeList    []models.Trade
}

func (s *VOOUp3ETFStudy) Run() error {
	log.Printf("Running study: %s", s.Name())

	ref, err := refdb.OpenExisting(refdb.DefaultPath)
	if err != nil {
		return fmt.Errorf("open reference db: %w", err)
	}
	if ref == nil {
		return fmt.Errorf("reference db %s missing or empty (no %q universe)", refdb.DefaultPath, refdb.ListSweep)
	}
	symbols, err := refdb.Universe(ref, refdb.ListSweep)
	ref.Close()
	if err != nil {
		return fmt.Errorf("read %q universe: %w", refdb.ListSweep, err)
	}
	if len(symbols) == 0 {
		return fmt.Errorf("reference db has no %q universe", refdb.ListSweep)
	}

	mdb, err := storage.OpenSQLite(s.marketDBPath)
	if err != nil {
		return fmt.Errorf("open market db: %w", err)
	}
	defer mdb.Close()

	vooMap, _, err := storage.FetchBars(mdb, "backtest_start", []string{"VOO"}, storage.DefaultStartDate, "")
	if err != nil {
		return fmt.Errorf("load VOO: %w", err)
	}
	voo := vooMap["VOO"]
	upDates := strategy.VOOUpStreakDates(voo, vooUp3GainDays)
	if len(upDates) == 0 {
		return fmt.Errorf("no VOO %d-up-day signals since %s", vooUp3GainDays, storage.DefaultStartDate)
	}
	log.Printf("VOO %d-up-close dates: %d (%s → %s)", vooUp3GainDays, len(upDates), upDates[0], upDates[len(upDates)-1])

	workers := runtime.NumCPU()
	if workers > vooUp3MaxWorkers {
		workers = vooUp3MaxWorkers
	}
	log.Printf("Comparing %d ETFs with %d workers (hold %dd, TP +%.0f%%, SL -%.0f%%)",
		len(symbols), workers, vooUp3Hold, vooUp3TP*100, vooUp3SL*100)

	jobs := make(chan string)
	results := make(chan vooUp3Row)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sym := range jobs {
				results <- evalVOOUp3Symbol(mdb, voo, sym)
			}
		}()
	}
	go func() {
		for _, sym := range symbols {
			jobs <- sym
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	rows := make([]vooUp3Row, 0, len(symbols))
	for r := range results {
		rows = append(rows, r)
		if len(rows)%100 == 0 {
			log.Printf("  ...%d/%d ETFs", len(rows), len(symbols))
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Symbol < rows[j].Symbol })

	if err := persistVOOUp3(s.resultsDBPath, upDates, rows); err != nil {
		return err
	}

	var ok []vooUp3Row
	for _, r := range rows {
		if r.Status == "ok" {
			ok = append(ok, r)
		}
	}
	sort.Slice(ok, func(i, j int) bool { return ok[i].AvgReturn > ok[j].AvgReturn })
	n := 20
	if n > len(ok) {
		n = len(ok)
	}
	fmt.Printf("\nVOO %d-UP → ETF  (hold %dd, +%.0f%% TP, -%.0f%% SL)  %d signal days, %d/%d ETFs traded\n",
		vooUp3GainDays, vooUp3Hold, vooUp3TP*100, vooUp3SL*100, len(upDates), len(ok), len(rows))
	fmt.Printf("%-4s %-8s %7s %8s %8s %8s %8s\n", "#", "ETF", "Trades", "WR", "AvgRet", "CAGR", "MaxDD")
	for i, r := range ok[:n] {
		fmt.Printf("%-4d %-8s %7d %7.1f%% %7.2f%% %7.2f%% %7.2f%%\n", i+1, r.Symbol, r.Trades, r.WinRate, r.AvgReturn, r.CAGR, r.MaxDD)
	}
	fmt.Printf("Full comparison: sqlite3 %s 'SELECT * FROM etf_compare'\n", s.resultsDBPath)
	return nil
}

func evalVOOUp3Symbol(db *sqlx.DB, voo []models.Bar, sym string) vooUp3Row {
	row := vooUp3Row{Symbol: sym}
	barMap, dates, err := storage.FetchBars(db, "backtest_start", []string{sym}, storage.DefaultStartDate, "")
	if err != nil {
		row.Status = "error"
		return row
	}
	bars := barMap[sym]
	row.Bars = len(bars)
	if len(bars) > 0 {
		row.FirstBar = bars[0].Date
	}
	if len(bars) < vooUp3MinBars {
		row.Status = "insufficient_bars"
		return row
	}
	sigs := strategy.VOOUpStreakSignals(sym, voo, bars, vooUp3GainDays, vooUp3TP, vooUp3SL, vooUp3Hold)
	row.SignalDays = len(sigs)
	if len(sigs) == 0 {
		row.Status = "no_signals"
		return row
	}
	cfg := strategy.StrategyConfig{
		AllocationPct:      vooUp3Alloc,
		PositionCap:        1,
		HoldingWindow:      vooUp3Hold,
		TakeProfitPct:      vooUp3TP,
		TargetPct:          1.0 + vooUp3TP,
		StopLossPct:        1.0 - vooUp3SL,
		CashYieldAnnual:    vooUp3Yield,
		SlippagePct:        0.0005,
		CommissionPerShare: 0.0001,
	}
	sim := simulator.NewPortfolioSimulator(cfg, vooUp3Capital)
	report, trades, _ := sim.Run(sigs, map[string][]models.Bar{sym: bars}, dates)
	if len(trades) == 0 {
		row.Status = "no_trades"
		return row
	}

	row.Status = "ok"
	row.TradeList = trades
	row.Trades = len(trades)
	rets := make([]float64, 0, len(trades))
	var sum float64
	row.BestTrade, row.WorstTrade = trades[0].ReturnPct*100, trades[0].ReturnPct*100
	for _, t := range trades {
		r := t.ReturnPct * 100
		rets = append(rets, r)
		sum += r
		if r > 0 {
			row.Wins++
		}
		if r > row.BestTrade {
			row.BestTrade = r
		}
		if r < row.WorstTrade {
			row.WorstTrade = r
		}
		switch t.ExitReason {
		case models.ExitReasonProfitTarget:
			row.TPExits++
		case models.ExitReasonStopLoss:
			row.SLExits++
		default:
			row.TimeExits++
		}
		row.AvgHoldDays += float64(t.HoldDays)
	}
	n := float64(len(trades))
	row.AvgHoldDays /= n
	row.WinRate = float64(row.Wins) / n * 100
	row.AvgReturn = sum / n
	sort.Float64s(rets)
	if m := len(rets); m%2 == 1 {
		row.MedianReturn = rets[m/2]
	} else {
		row.MedianReturn = (rets[m/2-1] + rets[m/2]) / 2
	}
	row.TotalReturn = report.TotalReturnPct * 100
	row.CAGR = report.CAGR * 100
	row.MaxDD = report.MaxDrawdownPct * 100
	row.Sharpe = report.SharpeRatio
	row.NetProfit = report.NetProfit
	return row
}

const vooUp3Schema = `
DROP VIEW  IF EXISTS etf_compare;
DROP TABLE IF EXISTS run_params;
DROP TABLE IF EXISTS voo_signals;
DROP TABLE IF EXISTS etf_results;
DROP TABLE IF EXISTS etf_trades;
CREATE TABLE run_params (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE voo_signals (date TEXT PRIMARY KEY);
CREATE TABLE etf_results (
	symbol            TEXT PRIMARY KEY,
	status            TEXT,     -- ok | insufficient_bars | no_signals | no_trades | error
	bars              INTEGER,
	first_bar         TEXT,
	signal_days       INTEGER,  -- VOO 3-up dates the ETF had a bar on
	trades            INTEGER,  -- signals actually taken (one position at a time)
	wins              INTEGER,
	win_rate_pct      REAL,
	avg_return_pct    REAL,     -- mean per-trade return
	median_return_pct REAL,
	best_trade_pct    REAL,
	worst_trade_pct   REAL,
	tp_exits          INTEGER,
	sl_exits          INTEGER,
	time_exits        INTEGER,
	avg_hold_days     REAL,
	total_return_pct  REAL,     -- whole-account return, 65% allocation
	cagr_pct          REAL,
	max_drawdown_pct  REAL,
	sharpe            REAL,
	net_profit        REAL
);
CREATE TABLE etf_trades (
	symbol      TEXT,
	entry_date  TEXT,
	exit_date   TEXT,
	entry_price REAL,
	exit_price  REAL,
	return_pct  REAL,
	exit_reason TEXT,
	hold_days   INTEGER
);
CREATE INDEX idx_etf_trades_symbol ON etf_trades (symbol, entry_date);
CREATE VIEW etf_compare AS
	SELECT
		RANK() OVER (ORDER BY avg_return_pct DESC) AS rank_avg_return,
		RANK() OVER (ORDER BY cagr_pct DESC)       AS rank_cagr,
		symbol, trades, win_rate_pct, avg_return_pct, median_return_pct,
		best_trade_pct, worst_trade_pct, tp_exits, sl_exits, time_exits,
		avg_hold_days, total_return_pct, cagr_pct, max_drawdown_pct, sharpe, net_profit
	FROM etf_results
	WHERE status = 'ok'
	ORDER BY avg_return_pct DESC;
`

func persistVOOUp3(path string, upDates []string, rows []vooUp3Row) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	db, err := storage.OpenSQLite(path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(vooUp3Schema); err != nil {
		return err
	}
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	fail := func(err error) error { _ = tx.Rollback(); return err }

	params := [][2]string{
		{"signal", fmt.Sprintf("VOO closes up %d consecutive days", vooUp3GainDays)},
		{"hold_days", fmt.Sprint(vooUp3Hold)},
		{"take_profit_pct", fmt.Sprint(vooUp3TP * 100)},
		{"stop_loss_pct", fmt.Sprint(vooUp3SL * 100)},
		{"allocation_pct", fmt.Sprint(vooUp3Alloc * 100)},
		{"start_date", storage.DefaultStartDate},
		{"universe", refdb.ListSweep + " (" + refdb.DefaultPath + ")"},
	}
	for _, p := range params {
		if _, err := tx.Exec(`INSERT INTO run_params VALUES (?, ?)`, p[0], p[1]); err != nil {
			return fail(err)
		}
	}
	for _, d := range upDates {
		if _, err := tx.Exec(`INSERT INTO voo_signals VALUES (?)`, d); err != nil {
			return fail(err)
		}
	}
	for _, r := range rows {
		if _, err := tx.Exec(`INSERT INTO etf_results VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.Symbol, r.Status, r.Bars, r.FirstBar, r.SignalDays, r.Trades, r.Wins, r.WinRate,
			r.AvgReturn, r.MedianReturn, r.BestTrade, r.WorstTrade, r.TPExits, r.SLExits, r.TimeExits,
			r.AvgHoldDays, r.TotalReturn, r.CAGR, r.MaxDD, r.Sharpe, r.NetProfit); err != nil {
			return fail(err)
		}
		for _, t := range r.TradeList {
			if _, err := tx.Exec(`INSERT INTO etf_trades VALUES (?,?,?,?,?,?,?,?)`,
				r.Symbol, t.EntryDate, t.ExitDate, t.EntryPrice, t.ExitPrice,
				t.ReturnPct*100, string(t.ExitReason), t.HoldDays); err != nil {
				return fail(err)
			}
		}
	}
	return tx.Commit()
}
