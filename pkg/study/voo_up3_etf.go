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
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// VOOUp3ETFStudy finds VOO 3-consecutive-up-close dates, then for every ETF in
// the market DB sweeps take-profit / stop-loss / hold and ranks by CAGR.
// The winning (symbol, TP, SL, hold) is written to data/voo_up3_winner.csv so
// the voo-up3 strategy trades it.
type VOOUp3ETFStudy struct {
	marketDBPath  string
	resultsDBPath string
}

func init() {
	Register(&VOOUp3ETFStudy{})
}

func (s *VOOUp3ETFStudy) ID() string { return "voo_up3_etf" }

func (s *VOOUp3ETFStudy) Name() string {
	return "VOO 3-Up-Day ETF Sweep (TP / SL / Hold → best CAGR)"
}

func (s *VOOUp3ETFStudy) Description() string {
	return "Signal: VOO closes up 3 days in a row. For each ETF, buy that close and sweep " +
		"take-profit, stop-loss, and hold-days. Rank by CAGR (min 15 trades, 65% allocation). " +
		"Writes the winner to data/voo_up3_winner.csv for the voo-up3 strategy."
}

func (s *VOOUp3ETFStudy) SetDatabases(marketDBPath, resultsDBPath string) {
	s.marketDBPath = marketDBPath
	s.resultsDBPath = resultsDBPath
}

const (
	vooUp3GainDays  = 3
	vooUp3MinBars   = 400
	vooUp3MinTrades = 15
	vooUp3Capital   = 100000.0
	vooUp3Alloc     = 0.65
	vooUp3Yield     = 0.045
	vooUp3WinnerOut = "data/voo_up3_winner.csv"
)

var (
	vooUp3TPs   = []float64{0.03, 0.05, 0.08, 0.12}
	vooUp3SLs   = []float64{0.02, 0.04, 0.06, 0.08}
	vooUp3Holds = []int{2, 5, 8, 12}
)

type vooUp3Row struct {
	Symbol     string
	TP, SL     float64
	Hold       int
	CAGR       float64
	WinRate    float64
	Trades     int
	MaxDD      float64
	MaxDDDays  int
	NetProfit  float64
	Sharpe     float64
	SignalDays int
}

func (s *VOOUp3ETFStudy) Run() error {
	log.Printf("Running study: %s", s.Name())
	if err := os.MkdirAll(filepath.Dir(s.resultsDBPath), 0755); err != nil {
		return err
	}

	mdb, err := storage.OpenSQLite(s.marketDBPath)
	if err != nil {
		return fmt.Errorf("open market db: %w", err)
	}
	defer mdb.Close()

	vooMap, _, err := storage.FetchBars(mdb, "backtest_start", []string{"VOO"}, "", "")
	if err != nil {
		return fmt.Errorf("load VOO: %w", err)
	}
	voo := vooMap["VOO"]
	if len(voo) < vooUp3GainDays+10 {
		return fmt.Errorf("not enough VOO bars (%d)", len(voo))
	}
	upDates := strategy.VOOUpStreakDates(voo, vooUp3GainDays)
	if len(upDates) == 0 {
		return fmt.Errorf("no VOO %d-up-day signals", vooUp3GainDays)
	}
	log.Printf("VOO %d-up-close dates: %d  (%s → %s)", vooUp3GainDays, len(upDates), upDates[0], upDates[len(upDates)-1])

	symbols, err := vooUp3Symbols(mdb)
	if err != nil {
		return err
	}
	log.Printf("Sweeping %d ETFs × %d TP × %d SL × %d hold = %d configs",
		len(symbols), len(vooUp3TPs), len(vooUp3SLs), len(vooUp3Holds),
		len(symbols)*len(vooUp3TPs)*len(vooUp3SLs)*len(vooUp3Holds))

	workers := runtime.NumCPU()
	jobs := make(chan string, len(symbols))
	for _, sym := range symbols {
		jobs <- sym
	}
	close(jobs)

	var mu sync.Mutex
	var best []vooUp3Row
	var done, skipped int
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sym := range jobs {
				row, ok := sweepSymbol(mdb, voo, upDates, sym)
				mu.Lock()
				done++
				if ok {
					best = append(best, row)
				} else {
					skipped++
				}
				if done%100 == 0 {
					log.Printf("  ...%d/%d symbols (%d with a usable config, %d skipped)", done, len(symbols), len(best), skipped)
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	sort.Slice(best, func(i, j int) bool {
		if best[i].CAGR != best[j].CAGR {
			return best[i].CAGR > best[j].CAGR
		}
		return best[i].Sharpe > best[j].Sharpe
	})

	if err := persistVOOUp3(s.resultsDBPath, upDates, best); err != nil {
		return err
	}
	if err := writeVOOUp3Winner(vooUp3WinnerOut, best); err != nil {
		return err
	}

	n := 20
	if n > len(best) {
		n = len(best)
	}
	fmt.Printf("\n========================================================================================================================\n")
	fmt.Printf("VOO %d-UP-DAY ETF SWEEP — best TP/SL/hold per ETF, ranked by CAGR\n", vooUp3GainDays)
	fmt.Printf("   Signal dates: %d   Symbols with a usable config: %d   Skipped: %d\n", len(upDates), len(best), skipped)
	fmt.Printf("========================================================================================================================\n")
	fmt.Printf("%-4s %-8s %-22s %8s %7s %7s %8s %7s\n", "#", "ETF", "TP / SL / Hold", "CAGR", "WR", "Trades", "MaxDD", "Sharpe")
	for i, r := range best[:n] {
		fmt.Printf("%-4d %-8s +%2.0f%% / -%2.0f%% / %2dd   %7.2f%% %6.1f%% %7d %7.2f%% %7.2f\n",
			i+1, r.Symbol, r.TP*100, r.SL*100, r.Hold, r.CAGR*100, r.WinRate*100, r.Trades, r.MaxDD*100, r.Sharpe)
	}
	if len(best) > 0 {
		w := best[0]
		fmt.Printf("\nWinner: %s  +%.0f%% TP / -%.0f%% SL / %d-day hold   CAGR %.2f%%  WR %.1f%%  %d trades\n",
			w.Symbol, w.TP*100, w.SL*100, w.Hold, w.CAGR*100, w.WinRate*100, w.Trades)
		fmt.Printf("Wrote %s — `./bin/backtest -strategy voo-up3` now trades this combo.\n", vooUp3WinnerOut)
	}
	fmt.Printf("Full ranking: %s\n", s.resultsDBPath)
	return nil
}

func vooUp3Symbols(db *sqlx.DB) ([]string, error) {
	var symbols []string
	err := db.Select(&symbols, `
		SELECT symbol FROM (
			SELECT symbol, COUNT(*) AS n
			FROM backtest_start
			WHERE length(Date) = 10 AND (timeframe = '1d' OR timeframe IS NULL OR timeframe = '')
			GROUP BY symbol
			HAVING n >= ?
		) ORDER BY symbol
	`, vooUp3MinBars)
	return symbols, err
}

func sweepSymbol(db *sqlx.DB, voo []models.Bar, upDates []string, sym string) (vooUp3Row, bool) {
	barMap, dates, err := storage.FetchBars(db, "backtest_start", []string{sym}, "", "")
	if err != nil {
		return vooUp3Row{}, false
	}
	bars := barMap[sym]
	if len(bars) < vooUp3MinBars {
		return vooUp3Row{}, false
	}

	var best vooUp3Row
	found := false
	for _, tp := range vooUp3TPs {
		for _, sl := range vooUp3SLs {
			for _, hold := range vooUp3Holds {
				sigs := strategy.VOOUpStreakSignals(sym, voo, bars, vooUp3GainDays, tp, sl, hold)
				if len(sigs) < vooUp3MinTrades {
					continue
				}
				cfg := strategy.StrategyConfig{
					AllocationPct:      vooUp3Alloc,
					PositionCap:        1,
					HoldingWindow:      hold,
					TakeProfitPct:      tp,
					TargetPct:          1.0 + tp,
					StopLossPct:        1.0 - sl,
					CashYieldAnnual:    vooUp3Yield,
					SlippagePct:        0.0005,
					CommissionPerShare: 0.0001,
				}
				sim := simulator.NewPortfolioSimulator(cfg, vooUp3Capital)
				report, _, _ := sim.Run(sigs, map[string][]models.Bar{sym: bars}, dates)
				if report.TotalTrades < vooUp3MinTrades {
					continue
				}
				if !found || report.CAGR > best.CAGR {
					best = vooUp3Row{
						Symbol:     sym,
						TP:         tp,
						SL:         sl,
						Hold:       hold,
						CAGR:       report.CAGR,
						WinRate:    report.WinRate,
						Trades:     report.TotalTrades,
						MaxDD:      report.MaxDrawdownPct,
						MaxDDDays:  report.MaxDrawdownDuration,
						NetProfit:  report.NetProfit,
						Sharpe:     report.SharpeRatio,
						SignalDays: len(sigs),
					}
					found = true
				}
			}
		}
	}
	return best, found
}

func persistVOOUp3(path string, upDates []string, rows []vooUp3Row) error {
	db, err := storage.OpenSQLite(path)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS voo_up3_signals (date TEXT PRIMARY KEY);
		DELETE FROM voo_up3_signals;
		CREATE TABLE IF NOT EXISTS voo_up3_results (
			rank INTEGER,
			symbol TEXT,
			take_profit_pct REAL,
			stop_loss_pct REAL,
			hold_days INTEGER,
			cagr REAL,
			win_rate REAL,
			total_trades INTEGER,
			max_drawdown_pct REAL,
			max_drawdown_days INTEGER,
			net_profit REAL,
			sharpe REAL,
			signal_days INTEGER
		);
		DELETE FROM voo_up3_results;
	`); err != nil {
		return err
	}
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	sigStmt, err := tx.Preparex(`INSERT INTO voo_up3_signals (date) VALUES (?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, d := range upDates {
		if _, err := sigStmt.Exec(d); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	_ = sigStmt.Close()
	rowStmt, err := tx.Preparex(`INSERT INTO voo_up3_results
		(rank, symbol, take_profit_pct, stop_loss_pct, hold_days, cagr, win_rate, total_trades,
		 max_drawdown_pct, max_drawdown_days, net_profit, sharpe, signal_days)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for i, r := range rows {
		if _, err := rowStmt.Exec(i+1, r.Symbol, r.TP, r.SL, r.Hold, r.CAGR, r.WinRate, r.Trades,
			r.MaxDD, r.MaxDDDays, r.NetProfit, r.Sharpe, r.SignalDays); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	_ = rowStmt.Close()
	return tx.Commit()
}

func writeVOOUp3Winner(path string, rows []vooUp3Row) error {
	if len(rows) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	w := rows[0]
	body := fmt.Sprintf("symbol,tp,sl,hold,cagr,win_rate,trades,max_dd,sharpe\n%s,%.4f,%.4f,%d,%.6f,%.6f,%d,%.6f,%.6f\n",
		w.Symbol, w.TP, w.SL, w.Hold, w.CAGR, w.WinRate, w.Trades, w.MaxDD, w.Sharpe)
	return os.WriteFile(path, []byte(body), 0644)
}
