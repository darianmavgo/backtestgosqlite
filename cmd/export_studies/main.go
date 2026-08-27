package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/darianmavgo/backtestgosqlite/internal/dipsim"
	"github.com/darianmavgo/backtestgosqlite/internal/signals"
	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	dbPath := "data/sp500_etfs_study.db"
	db, err := storage.OpenSQLite(dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	fmt.Println("🚀 Exporting all study calculations into SQLite tables and generating adjacent HTML reports...")

	// 1. Create and populate additional calculation tables in sp500_etfs_study.db
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS tecl_allocation_matrix (
			allocation_pct REAL PRIMARY KEY,
			net_profit REAL,
			ending_capital REAL,
			total_return_pct REAL,
			cagr_pct REAL,
			max_mtm_drawdown_pct REAL,
			max_closed_drawdown_pct REAL,
			calmar_ratio REAL,
			total_trades INTEGER,
			win_rate_pct REAL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS compare_3x_etfs_matrix (
			symbol TEXT PRIMARY KEY,
			name TEXT,
			net_profit REAL,
			ending_capital REAL,
			total_return_pct REAL,
			cagr_pct REAL,
			max_mtm_drawdown_pct REAL,
			calmar_ratio REAL,
			win_rate_pct REAL,
			total_trades INTEGER,
			avg_hold_days REAL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS inverse_3x_reversion_matrix (
			symbol TEXT,
			regime_filter TEXT,
			net_profit REAL,
			ending_capital REAL,
			total_return_pct REAL,
			cagr_pct REAL,
			max_mtm_drawdown_pct REAL,
			calmar_ratio REAL,
			win_rate_pct REAL,
			total_trades INTEGER,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (symbol, regime_filter)
		);

		CREATE TABLE IF NOT EXISTS all_weather_combo_trades (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			trade_num INTEGER,
			direction TEXT,
			asset TEXT,
			signal_date TEXT,
			entry_date TEXT,
			entry_price REAL,
			exit_date TEXT,
			exit_price REAL,
			hold_days INTEGER,
			exit_reason TEXT,
			return_pct REAL,
			net_pnl REAL,
			is_win INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS all_weather_combo_summary (
			strategy_name TEXT PRIMARY KEY,
			initial_capital REAL,
			ending_capital REAL,
			net_profit REAL,
			total_return_pct REAL,
			cagr_pct REAL,
			max_mtm_drawdown_pct REAL,
			calmar_ratio REAL,
			win_rate_pct REAL,
			total_trades INTEGER,
			long_trades INTEGER,
			short_trades INTEGER,
			long_win_rate_pct REAL,
			short_win_rate_pct REAL,
			profit_factor REAL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)

	// Load core data bars
	vooBars, _ := storage.FetchDipBars(db, "VOO")
	sigBars, _ := storage.FetchSignalBars(db, "VOO")
	dropSignals := signals.DetectConsecutiveDrops(vooBars, 3)
	rallySignals := signals.DetectConsecutiveRallies(vooBars, 3)
	bearRallySignals := signals.FilterByRegime(rallySignals, sigBars, "VOO < SMA200")

	// 2. Populate TECL Allocation Matrix
	teclBars, _ := storage.FetchDipBars(db, "TECL")
	allocTiers := []float64{0.30, 0.40, 0.50, 0.60, 0.65, 0.75, 0.80, 0.85, 0.90, 1.00}
	tx1, _ := db.Beginx()
	stmt1, _ := tx1.Preparex(`
		INSERT OR REPLACE INTO tecl_allocation_matrix (
			allocation_pct, net_profit, ending_capital, total_return_pct, cagr_pct,
			max_mtm_drawdown_pct, max_closed_drawdown_pct, calmar_ratio, total_trades, win_rate_pct
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	for _, a := range allocTiers {
		cfg := dipsim.DipSimConfig{
			SignalSymbol: "VOO", SignalDays: 3, SignalDirection: "drop",
			TradeSymbol: "TECL", AllocationPct: a, TakeProfitPct: 0.05,
			MaxHoldDays: 8, InitialCapital: 100000.0, CashYieldAnnual: 0.045,
		}
		res := dipsim.Run(cfg, dropSignals, teclBars)
		_, _ = stmt1.Exec(a, res.NetProfit, res.EndingCap, res.TotalReturn, res.CAGR, res.MTMMaxDD, res.ClosedMaxDD, res.CalmarRatio, res.TotalTrades, res.WinRate)
	}
	stmt1.Close()
	_ = tx1.Commit()
	fmt.Println("✅ Populated tecl_allocation_matrix in sp500_etfs_study.db")

	// 3. Populate 6 3x Leveraged ETFs Head-to-Head Matrix
	etfMeta := []struct {
		Sym  string
		Name string
	}{
		{"TECL", "Direxion Daily Technology Bull 3X"},
		{"TQQQ", "ProShares UltraPro QQQ (3x Nasdaq 100)"},
		{"FAS", "Direxion Daily Financial Bull 3X"},
		{"UPRO", "ProShares UltraPro S&P 500"},
		{"SOXL", "Direxion Daily Semiconductor Bull 3X"},
		{"UDOW", "ProShares UltraPro Dow30"},
	}
	tx2, _ := db.Beginx()
	stmt2, _ := tx2.Preparex(`
		INSERT OR REPLACE INTO compare_3x_etfs_matrix (
			symbol, name, net_profit, ending_capital, total_return_pct, cagr_pct,
			max_mtm_drawdown_pct, calmar_ratio, win_rate_pct, total_trades, avg_hold_days
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	for _, em := range etfMeta {
		bars, err := storage.FetchDipBars(db, em.Sym)
		if err == nil && len(bars) > 0 {
			cfg := dipsim.DipSimConfig{
				SignalSymbol: "VOO", SignalDays: 3, SignalDirection: "drop",
				TradeSymbol: em.Sym, AllocationPct: 0.65, TakeProfitPct: 0.05,
				MaxHoldDays: 8, InitialCapital: 100000.0, CashYieldAnnual: 0.045,
			}
			res := dipsim.Run(cfg, dropSignals, bars)
			_, _ = stmt2.Exec(em.Sym, em.Name, res.NetProfit, res.EndingCap, res.TotalReturn, res.CAGR, res.MTMMaxDD, res.CalmarRatio, res.WinRate, res.TotalTrades, res.AvgHoldDays)
		}
	}
	stmt2.Close()
	_ = tx2.Commit()
	fmt.Println("✅ Populated compare_3x_etfs_matrix in sp500_etfs_study.db")

	// 4. Populate Inverse 3x Reversion Matrix
	inverseMeta := []string{"SPXU", "SQQQ", "SOXS"}
	tx3, _ := db.Beginx()
	stmt3, _ := tx3.Preparex(`
		INSERT OR REPLACE INTO inverse_3x_reversion_matrix (
			symbol, regime_filter, net_profit, ending_capital, total_return_pct, cagr_pct,
			max_mtm_drawdown_pct, calmar_ratio, win_rate_pct, total_trades
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	for _, sym := range inverseMeta {
		bars, err := storage.FetchDipBars(db, sym)
		if err == nil && len(bars) > 0 {
			// All regimes
			cfgAll := dipsim.DipSimConfig{
				SignalSymbol: "VOO", SignalDays: 3, SignalDirection: "rally",
				TradeSymbol: sym, AllocationPct: 0.65, TakeProfitPct: 0.05,
				MaxHoldDays: 8, InitialCapital: 100000.0, CashYieldAnnual: 0.045,
			}
			resAll := dipsim.Run(cfgAll, rallySignals, bars)
			_, _ = stmt3.Exec(sym, "All Regimes", resAll.NetProfit, resAll.EndingCap, resAll.TotalReturn, resAll.CAGR, resAll.MTMMaxDD, resAll.CalmarRatio, resAll.WinRate, resAll.TotalTrades)

			// Bear regime
			cfgBear := dipsim.DipSimConfig{
				SignalSymbol: "VOO", SignalDays: 3, SignalDirection: "rally",
				TradeSymbol: sym, AllocationPct: 0.65, TakeProfitPct: 0.06,
				StopLossPct: 0.05, MaxHoldDays: 2, InitialCapital: 100000.0, CashYieldAnnual: 0.045,
			}
			resBear := dipsim.Run(cfgBear, bearRallySignals, bars)
			_, _ = stmt3.Exec(sym, "VOO < SMA200", resBear.NetProfit, resBear.EndingCap, resBear.TotalReturn, resBear.CAGR, resBear.MTMMaxDD, resBear.CalmarRatio, resBear.WinRate, resBear.TotalTrades)
		}
	}
	stmt3.Close()
	_ = tx3.Commit()
	fmt.Println("✅ Populated inverse_3x_reversion_matrix in sp500_etfs_study.db")

	// 5. Populate All-Weather Combo (TECL Long Dips + SPXU Short Bear Rallies)
	spxuBars, _ := storage.FetchDipBars(db, "SPXU")
	comboCfg := dipsim.ComboConfig{
		LongConfig: dipsim.DipSimConfig{
			SignalSymbol: "VOO", SignalDays: 3, SignalDirection: "drop",
			TradeSymbol: "TECL", AllocationPct: 0.65, TakeProfitPct: 0.05,
			MaxHoldDays: 8, InitialCapital: 100000.0, CashYieldAnnual: 0.045,
		},
		ShortConfig: dipsim.DipSimConfig{
			SignalSymbol: "VOO", SignalDays: 3, SignalDirection: "rally",
			TradeSymbol: "SPXU", AllocationPct: 0.65, TakeProfitPct: 0.06,
			StopLossPct: 0.05, MaxHoldDays: 2, RegimeFilter: "VOO < SMA200",
			InitialCapital: 100000.0, CashYieldAnnual: 0.045,
		},
		InitialCapital: 100000.0, CashYieldAnnual: 0.045,
	}
	comboRes := dipsim.RunCombo(comboCfg, dropSignals, bearRallySignals, vooBars, teclBars, spxuBars)

	_, _ = db.Exec("DELETE FROM all_weather_combo_trades;")
	tx4, _ := db.Beginx()
	stmt4, _ := tx4.Preparex(`
		INSERT INTO all_weather_combo_trades (
			trade_num, direction, asset, signal_date, entry_date, entry_price,
			exit_date, exit_price, hold_days, exit_reason, return_pct, net_pnl, is_win
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	for _, t := range comboRes.Trades {
		winInt := 0
		if t.IsWin {
			winInt = 1
		}
		_, _ = stmt4.Exec(t.TradeNum, t.Direction, t.Asset, t.SignalDate, t.EntryDate, t.EntryPrice, t.ExitDate, t.ExitPrice, t.HoldDays, t.ExitReason, t.ReturnPct, t.NetPnL, winInt)
	}
	stmt4.Close()
	_ = tx4.Commit()

	longWinRate := 0.0
	if comboRes.LongTrades > 0 {
		longWinRate = float64(comboRes.LongWins) / float64(comboRes.LongTrades) * 100.0
	}
	shortWinRate := 0.0
	if comboRes.ShortTrades > 0 {
		shortWinRate = float64(comboRes.ShortWins) / float64(comboRes.ShortTrades) * 100.0
	}

	_, _ = db.Exec(`
		INSERT OR REPLACE INTO all_weather_combo_summary (
			strategy_name, initial_capital, ending_capital, net_profit, total_return_pct,
			cagr_pct, max_mtm_drawdown_pct, calmar_ratio, win_rate_pct, total_trades,
			long_trades, short_trades, long_win_rate_pct, short_win_rate_pct, profit_factor
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, "All-Weather TECL (Long Dips) + SPXU (Short Bear Rallies)", comboCfg.InitialCapital, comboRes.EndingCap, comboRes.NetProfit, comboRes.TotalReturn,
		comboRes.CAGR, comboRes.MTMMaxDD, comboRes.CalmarRatio, comboRes.WinRate, comboRes.TotalTrades, comboRes.LongTrades, comboRes.ShortTrades, longWinRate, shortWinRate, comboRes.ProfitFactor)

	fmt.Println("✅ Populated all_weather_combo_trades & summary in sp500_etfs_study.db")

	// 6. Copy HTML Reports adjacent to SQLite databases in data/
	reportFiles, _ := filepath.Glob("reports/*.html")
	for _, rFile := range reportFiles {
		baseName := filepath.Base(rFile)
		dest := filepath.Join("data", baseName)
		copyFile(rFile, dest)
	}
	fmt.Printf("✅ Copied %d HTML study reports directly into data/ adjacent to .db files\n", len(reportFiles))

	// 7. Generate Master Study Catalog: data/index.html
	generateMasterCatalog("data/index.html")
	generateMasterCatalog("reports/index.html")
	fmt.Println("✅ Generated Master Study Catalog: data/index.html & reports/index.html")
}

func copyFile(src, dst string) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer out.Close()

	_, _ = io.Copy(out, in)
}

func generateMasterCatalog(outputPath string) {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Quantitative Backtest Studies & SQLite Database Catalog</title>
    <style>
        :root {
            --bg: #090d16;
            --card: #111827;
            --card-hover: #172033;
            --border: #1e293b;
            --text: #f8fafc;
            --text-dim: #94a3b8;
            --accent: #38bdf8;
            --pos: #10b981;
            --neg: #f43f5e;
            --amber: #f59e0b;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; }
        body { background: var(--bg); color: var(--text); padding: 32px 20px; line-height: 1.6; }
        .container { max-width: 1320px; margin: 0 auto; }
        .header { margin-bottom: 28px; padding-bottom: 16px; border-bottom: 1px solid var(--border); }
        .title { font-size: 28px; font-weight: 800; color: #fff; }
        .subtitle { color: var(--text-dim); font-size: 15px; margin-top: 6px; }
        
        .section-title { font-size: 20px; font-weight: 700; color: var(--accent); margin: 32px 0 16px 0; }
        .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(380px, 1fr)); gap: 18px; }
        
        .card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 22px; transition: transform 0.2s, border-color 0.2s; display: flex; flex-direction: column; justify-content: space-between; }
        .card:hover { transform: translateY(-2px); border-color: var(--accent); background: var(--card-hover); }
        
        .card-header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 12px; }
        .card-title { font-size: 17px; font-weight: 700; color: #fff; }
        .badge { padding: 3px 8px; border-radius: 6px; font-size: 11px; font-weight: 700; text-transform: uppercase; }
        .badge-sql { background: rgba(56, 189, 248, 0.15); color: var(--accent); border: 1px solid rgba(56, 189, 248, 0.3); }
        .badge-win { background: rgba(16, 185, 129, 0.15); color: var(--pos); border: 1px solid rgba(16, 185, 129, 0.3); }
        
        .card-desc { font-size: 13.5px; color: var(--text-dim); margin-bottom: 16px; flex-grow: 1; }
        .card-meta { background: #0c1220; border-radius: 8px; padding: 10px 12px; margin-bottom: 16px; font-size: 12.5px; border: 1px solid rgba(255,255,255,0.05); }
        .meta-row { display: flex; justify-content: space-between; margin-bottom: 4px; }
        .meta-row:last-child { margin-bottom: 0; }
        .meta-label { color: var(--text-dim); }
        .meta-val { color: #fff; font-weight: 600; }
        
        .card-actions { display: flex; gap: 10px; }
        .btn { display: inline-block; padding: 8px 14px; border-radius: 8px; font-size: 13px; font-weight: 600; text-decoration: none; text-align: center; cursor: pointer; transition: all 0.2s; }
        .btn-primary { background: var(--accent); color: #090d16; }
        .btn-primary:hover { background: #7dd3fc; }
        .btn-secondary { background: #1e293b; color: var(--text); border: 1px solid var(--border); }
        .btn-secondary:hover { background: #334155; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1 class="title">🏛️ Quantitative Backtest Studies & SQLite Catalog</h1>
            <p class="subtitle">Complete inventory of research studies, underlying SQLite calculations, relational schema tables, and interactive Chart.js reports.</p>
        </div>

        <h2 class="section-title">📊 Primary Leveraged ETF Mean-Reversion Studies</h2>
        <div class="grid">
            <!-- Study 1: Dual All-Weather Combo -->
            <div class="card">
                <div>
                    <div class="card-header">
                        <div class="card-title">👑 All-Weather Dual Combo (TECL + SPXU)</div>
                        <span class="badge badge-win">40.7% CAGR</span>
                    </div>
                    <div class="card-desc">
                        Combines Long TECL on 3-day dips (+5% TP / 8d hold) with Short SPXU on 3-day rallies during bear markets (+6% TP / -5% SL / 2d hold).
                    </div>
                    <div class="card-meta">
                        <div class="meta-row"><span class="meta-label">SQLite DB:</span><span class="meta-val">data/sp500_etfs_study.db</span></div>
                        <div class="meta-row"><span class="meta-label">SQL Tables:</span><span class="meta-val">all_weather_combo_trades, summary</span></div>
                        <div class="meta-row"><span class="meta-label">Key Metrics:</span><span class="meta-val" style="color: #10b981;">+$452k Net PnL | 13.36% Max DD | Calmar 3.05</span></div>
                    </div>
                </div>
                <div class="card-actions">
                    <a href="tecl_spxu_optimized_combo.html" class="btn btn-primary" target="_blank">Open Interactive Report</a>
                    <a href="lib_combo_tecl_spxu.html" class="btn btn-secondary" target="_blank">Modular Report</a>
                </div>
            </div>

            <!-- Study 2: TECL Across Sizing Tiers -->
            <div class="card">
                <div>
                    <div class="card-header">
                        <div class="card-title">🚀 TECL Sizing Matrix (30% to 100%)</div>
                        <span class="badge badge-sql">30% - 100%</span>
                    </div>
                    <div class="card-desc">
                        Full allocation sensitivity study showing portfolio growth, mark-to-market drawdowns, and Calmar ratios from 30% up to 100% dynamic sizing.
                    </div>
                    <div class="card-meta">
                        <div class="meta-row"><span class="meta-label">SQLite DB:</span><span class="meta-val">data/sp500_etfs_study.db</span></div>
                        <div class="meta-row"><span class="meta-label">SQL Tables:</span><span class="meta-val">tecl_allocation_matrix</span></div>
                        <div class="meta-row"><span class="meta-label">Key Metrics:</span><span class="meta-val" style="color: #10b981;">50% Alloc = 10.27% DD / 25.8% CAGR</span></div>
                    </div>
                </div>
                <div class="card-actions">
                    <a href="tecl_allocations.html" class="btn btn-primary" target="_blank">Open Allocation Matrix</a>
                    <a href="lib_tecl_dip.html" class="btn btn-secondary" target="_blank">Single Run Report</a>
                </div>
            </div>

            <!-- Study 3: 6 3x Leveraged ETFs Comparison -->
            <div class="card">
                <div>
                    <div class="card-header">
                        <div class="card-title">⚔️ 6 3x Leveraged ETFs Head-to-Head</div>
                        <span class="badge badge-sql">6 Assets</span>
                    </div>
                    <div class="card-desc">
                        Head-to-head comparison of TECL, TQQQ, FAS, UPRO, SOXL, and UDOW trading the VOO 3-day dip strategy with 4.5% Treasury yield.
                    </div>
                    <div class="card-meta">
                        <div class="meta-row"><span class="meta-label">SQLite DB:</span><span class="meta-val">data/sp500_etfs_study.db</span></div>
                        <div class="meta-row"><span class="meta-label">SQL Tables:</span><span class="meta-val">compare_3x_etfs_matrix</span></div>
                        <div class="meta-row"><span class="meta-label">Rank #1:</span><span class="meta-val" style="color: #10b981;">TECL (+315.9% Return / Calmar 2.47)</span></div>
                    </div>
                </div>
                <div class="card-actions">
                    <a href="compare_3x_etfs.html" class="btn btn-primary" target="_blank">Open Head-to-Head Report</a>
                </div>
            </div>

            <!-- Study 4: Bear Strategy Grid Search -->
            <div class="card">
                <div>
                    <div class="card-header">
                        <div class="card-title">🐻 Bear Market Grid Search Optimizer</div>
                        <span class="badge badge-sql">23,760 Runs</span>
                    </div>
                    <div class="card-desc">
                        Multi-dimensional parallel parameter search across SPXU, SQQQ, and SOXS testing streak lengths, holding periods, profit targets, and stop losses.
                    </div>
                    <div class="card-meta">
                        <div class="meta-row"><span class="meta-label">SQLite DB:</span><span class="meta-val">data/sp500_etfs_study.db</span></div>
                        <div class="meta-row"><span class="meta-label">SQL Tables:</span><span class="meta-val">bear_optimization_grid_results</span></div>
                        <div class="meta-row"><span class="meta-label">Rank #1 Setup:</span><span class="meta-val" style="color: #10b981;">SPXU 3d/2d/+6%/-5% (Calmar 3.06 / 3.25% DD)</span></div>
                    </div>
                </div>
                <div class="card-actions">
                    <a href="lib_gridsearch_bear.html" class="btn btn-primary" target="_blank">Open Grid Search Report</a>
                    <a href="inverse_3x_study.html" class="btn btn-secondary" target="_blank">Inverse Study</a>
                </div>
            </div>

            <!-- Study 5: Dividend & Parking Sweeps -->
            <div class="card">
                <div>
                    <div class="card-header">
                        <div class="card-title">💵 Idle Reserve Sweeps (AGNC / GOOGL)</div>
                        <span class="badge badge-sql">Dividends</span>
                    </div>
                    <div class="card-desc">
                        Backtests incorporating ex-dividend accounting: parking 100% idle capital into monthly dividend payer AGNC (+$85k cash collected) or GOOGL.
                    </div>
                    <div class="card-meta">
                        <div class="meta-row"><span class="meta-label">SQLite DB:</span><span class="meta-val">data/sp500_etfs_study.db</span></div>
                        <div class="meta-row"><span class="meta-label">SQL Tables:</span><span class="meta-val">corporate_dividends, backtest_start</span></div>
                        <div class="meta-row"><span class="meta-label">AGNC Dividends:</span><span class="meta-val" style="color: #10b981;">+$85,675.40 Cash Collected</span></div>
                    </div>
                </div>
                <div class="card-actions">
                    <a href="rank6_agnc_sweep_vs_voo.html" class="btn btn-primary" target="_blank">AGNC Dividend Sweep</a>
                    <a href="rank6_googl_sweep_vs_voo.html" class="btn btn-secondary" target="_blank">GOOGL Sweep</a>
                </div>
            </div>

            <!-- Study 6: VOO / QQQ Dip Baseline -->
            <div class="card">
                <div>
                    <div class="card-header">
                        <div class="card-title">📉 VOO & QQQ Streak Reversion Baseline</div>
                        <span class="badge badge-sql">Streaks</span>
                    </div>
                    <div class="card-desc">
                        Initial empirical frequency analysis of multi-day consecutive drops and mean-reversion snapbacks across S&P 500 and Nasdaq 100.
                    </div>
                    <div class="card-meta">
                        <div class="meta-row"><span class="meta-label">SQLite DB:</span><span class="meta-val">data/sp500_etfs_study.db</span></div>
                        <div class="meta-row"><span class="meta-label">SQL Tables:</span><span class="meta-val">study_trades, study_win_rates, voo_upro_trades</span></div>
                    </div>
                </div>
                <div class="card-actions">
                    <a href="voo_streaks.html" class="btn btn-primary" target="_blank">VOO Streaks</a>
                    <a href="qqq_streaks.html" class="btn btn-secondary" target="_blank">QQQ Streaks</a>
                    <a href="rank6_vs_voo.html" class="btn btn-secondary" target="_blank">Rank 6 Baseline</a>
                </div>
            </div>
        </div>

        <h2 class="section-title">🏛️ Multi-Strategy Screening & Institutional Analytics</h2>
        <div class="grid">
            <div class="card">
                <div>
                    <div class="card-header">
                        <div class="card-title">🔬 Whitings Creek Multi-Strategy Study</div>
                        <span class="badge badge-sql">Equities</span>
                    </div>
                    <div class="card-desc">
                        Multi-strategy screening engine across Donchian Breakouts, Bollinger Capitulation, RSI2 Mean-Reversion, and MACD Crossovers.
                    </div>
                    <div class="card-meta">
                        <div class="meta-row"><span class="meta-label">SQLite DB:</span><span class="meta-val">data/wc_master_backtest.db, data/live_scan.db</span></div>
                        <div class="meta-row"><span class="meta-label">SQL Tables:</span><span class="meta-val">wc_summary, wc_backtest_details, entry</span></div>
                    </div>
                </div>
                <div class="card-actions">
                    <a href="comparison_report.html" class="btn btn-primary" target="_blank">Multi-Strategy Comparison</a>
                    <a href="backtest_report.html" class="btn btn-secondary" target="_blank">Detail Report</a>
                </div>
            </div>

            <div class="card">
                <div>
                    <div class="card-header">
                        <div class="card-title">💼 IBKR Portfolio Analytics</div>
                        <span class="badge badge-sql">Live Ledger</span>
                    </div>
                    <div class="card-desc">
                        Execution ledger tracking live trades, cash balances, equity progression, Sharpe, Sortino, and drawdown analytics.
                    </div>
                    <div class="card-meta">
                        <div class="meta-row"><span class="meta-label">SQLite DB:</span><span class="meta-val">data/ibkr_history.db</span></div>
                        <div class="meta-row"><span class="meta-label">SQL Tables:</span><span class="meta-val">backtest_start</span></div>
                    </div>
                </div>
                <div class="card-actions">
                    <a href="ibkr_portfolio_report.html" class="btn btn-primary" target="_blank">IBKR Portfolio Report</a>
                </div>
            </div>
        </div>
    </div>
</body>
</html>`

	_ = os.WriteFile(outputPath, []byte(html), 0644)
}
