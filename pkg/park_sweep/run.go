package park_sweep

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/markov_strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/options"
	"github.com/darianmavgo/backtestgosqlite/pkg/refdb"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	"github.com/darianmavgo/backtestgosqlite/pkg/streak_strategy"
	"github.com/jmoiron/sqlx"
)

// markovMu keeps one Markov calc database consistent. The prediction table is
// built once; each strategy clears only its signal rows before generating.
var markovMu sync.Mutex

// Run simulates every pending strategy with the park symbol. Failed and
// running rows from an earlier process are claimed again. A failure during
// this process stays failed until the next Run.
func Run(sweepPath, marketPath string, concurrency int) error {
	sweep, err := Open(sweepPath)
	if err != nil {
		return err
	}
	defer sweep.Close()
	cfg, err := loadConfig(sweep)
	if err != nil {
		return err
	}
	var holdEquity float64
	if err := sweep.Get(&holdEquity, `SELECT final_equity FROM park_asset WHERE symbol = ?`, cfg.ParkSymbol); err != nil {
		return fmt.Errorf("park_asset %s: %w (run seed first)", cfg.ParkSymbol, err)
	}
	if _, err := sweep.Exec(`UPDATE strategy_run SET status = 'pending', error = '' WHERE status IN ('running', 'failed')`); err != nil {
		return err
	}
	if concurrency < 1 {
		concurrency = runtime.NumCPU()
	}

	var wg sync.WaitGroup
	errCh := make(chan error, concurrency)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			market, err := storage.OpenSQLite(marketPath)
			if err != nil {
				errCh <- err
				return
			}
			defer market.Close()
			for {
				id, err := claimNext(sweep)
				if err != nil {
					errCh <- err
					return
				}
				if id == "" {
					return
				}
				if err := runOne(sweep, market, marketPath, id, cfg, holdEquity); err != nil {
					fmt.Printf("%s failed: %v\n", id, err)
					_, _ = sweep.Exec(`
						UPDATE strategy_run
						SET status = 'failed', error = ?, finished_at = ?
						WHERE strategy_id = ?`, err.Error(), time.Now().UTC().Format(time.RFC3339), id)
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func claimNext(db *sqlx.DB) (string, error) {
	tx, err := db.Beginx()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var id string
	err = tx.Get(&id, `
		SELECT r.strategy_id
		FROM strategy_run r
		JOIN sweep_strategy s ON s.strategy_id = r.strategy_id
		WHERE r.status = 'pending'
		ORDER BY CASE s.kind WHEN 'streak' THEN 0 ELSE 1 END,
		         r.strategy_id
		LIMIT 1`)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	res, err := tx.Exec(`
		UPDATE strategy_run SET status = 'running', error = '', started_at = ?
		WHERE strategy_id = ? AND status = 'pending'`,
		time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return "", err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return "", err
	}
	if n != 1 {
		return "", nil
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

func runOne(sweep, market *sqlx.DB, marketPath, id string, cfg Config, holdEquity float64) error {
	var row Strategy
	if err := sweep.Get(&row, `SELECT * FROM sweep_strategy WHERE strategy_id = ?`, id); err != nil {
		return err
	}
	alloc := row.AllocationPct
	if cfg.AllocationPct.Valid {
		alloc = cfg.AllocationPct.Float64
	}
	syms := []string{row.SignalSymbol, row.TradeSymbol, cfg.ParkSymbol}
	bars, dates, err := storage.FetchBars(market, "backtest_start", uniqueUpper(syms), cfg.StartDate, cfg.EndDate)
	if err != nil {
		return err
	}
	if len(bars[cfg.ParkSymbol]) == 0 {
		return fmt.Errorf("no %s bars", cfg.ParkSymbol)
	}
	if len(bars[row.TradeSymbol]) == 0 {
		return fmt.Errorf("no %s bars", row.TradeSymbol)
	}
	sigs, strat, err := signalsFor(row, bars, marketPath)
	if err != nil {
		return err
	}
	inWindow := map[string]bool{}
	for _, d := range dates {
		inWindow[d] = true
	}
	kept := sigs[:0]
	for _, sig := range sigs {
		if !inWindow[sig.Date] {
			continue
		}
		sig.StrategyID = id
		sig.Priority = 0
		kept = append(kept, sig)
	}
	simCfg := strat.DefaultConfig()
	simCfg.AllocationPct = alloc
	simCfg.PositionSizing = "fixed_pct"
	sim := simulator.NewSharedAccountSimulator([]simulator.StrategyPriorityEntry{{
		Strategy: strat, Priority: 0, Config: simCfg,
	}}, cfg.Capital)
	sim.SetDefaultAsset(cfg.ParkSymbol, options.DividendsFromAdjClose(bars[cfg.ParkSymbol]))
	report, _, trades, _ := sim.Run(kept, bars, dates)
	parked := sim.DefaultAssetResult()
	var sleeve float64
	for _, t := range trades {
		sleeve += t.NetPnL
	}
	contrib := report.FinalEquity - report.InitialCapital - sleeve
	_, err = sweep.Exec(`
		UPDATE strategy_run SET
			status = 'done', error = '',
			final_equity = ?, cagr = ?, max_drawdown_pct = ?, sharpe = ?,
			total_trades = ?, winning_trades = ?, losing_trades = ?, win_rate = ?,
			idle_days = ?, avg_park_weight = ?, dividends = ?,
			sleeve_net = ?, park_contribution = ?, edge_vs_googl = ?,
			finished_at = ?
		WHERE strategy_id = ?`,
		report.FinalEquity, report.CAGR, report.MaxDrawdownPct, report.SharpeRatio,
		report.TotalTrades, report.WinningTrades, report.LosingTrades, report.WinRate,
		report.IdleDays, parked.AvgWeight, parked.Dividends,
		sleeve, contrib, report.FinalEquity-holdEquity,
		time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return err
	}
	fmt.Printf("%s %s equity %.2f cagr %.2f%% trades %d edge %.2f\n",
		row.Kind, id, report.FinalEquity, report.CAGR*100, report.TotalTrades, report.FinalEquity-holdEquity)
	return nil
}

func signalsFor(row Strategy, bars map[string][]models.Bar, marketPath string) ([]models.Signal, strategy.Strategy, error) {
	switch row.Kind {
	case "streak":
		s := &streak_strategy.Strategy{Row: refdb.StreakStrategy{
			ID: row.StrategyID, Name: row.Name,
			SignalSymbol: row.SignalSymbol, TradeSymbol: row.TradeSymbol,
			Direction: row.Direction, SignalDays: row.SignalDays, HoldDays: row.HoldDays,
			TakeProfitPct: row.TakeProfitPct, StopLossPct: row.StopLossPct, Regime: row.Regime,
			AllocationPct: row.AllocationPct, CashYield: row.CashYield, SlippagePct: row.SlippagePct,
			NextDayLimit: row.NextDayLimit,
		}}
		return s.GenerateSignals(bars), s, nil
	case "markov":
		s := &markov_strategy.Strategy{Row: refdb.MarkovStrategy{
			ID: row.StrategyID, Name: row.Name,
			SignalSymbol: row.SignalSymbol, TradeSymbol: row.TradeSymbol,
			Direction: row.Direction, TargetState: row.TargetState, HoldDays: row.HoldDays,
			TakeProfitPct: row.TakeProfitPct, StopLossPct: row.StopLossPct,
			AllocationPct: row.AllocationPct, CashYield: row.CashYield, SlippagePct: row.SlippagePct,
			NextDayLimit: row.NextDayLimit,
		}}
		sigs, err := markovSignals(s, bars, marketPath)
		return sigs, s, err
	default:
		return nil, nil, fmt.Errorf("unknown kind %q", row.Kind)
	}
}

func markovSignals(s *markov_strategy.Strategy, bars map[string][]models.Bar, marketPath string) ([]models.Signal, error) {
	markovMu.Lock()
	defer markovMu.Unlock()
	calc := filepath.Join(os.TempDir(), "park_sweep_markov_calc.db")
	table := "markov_model_signals"
	if strings.Contains(s.ID(), "hmm") {
		calc = filepath.Join(os.TempDir(), "park_sweep_markov_hmm_calc.db")
		table = "markov_hmm_signals"
	}
	clearTable(calc, table)
	s.SetDatabases(marketPath, calc)
	sigs := s.GenerateSignals(bars)
	if err := requireTable(calc, "markov_model_predictions"); err != nil {
		return nil, err
	}
	return sigs, nil
}

func clearTable(path, table string) {
	db, err := sqlx.Open("sqlite", path)
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.Exec("DELETE FROM " + table)
}

func requireTable(path, table string) error {
	db, err := sqlx.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	var n int
	if err := db.Get(&n, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("markov pipeline did not build %s", table)
	}
	return nil
}

func uniqueUpper(syms []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range syms {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
