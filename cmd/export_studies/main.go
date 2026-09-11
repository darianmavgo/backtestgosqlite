package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/simulator"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	dbPath := "data/sp500_etfs_study.db"
	db, err := storage.OpenSQLite(dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	fmt.Println("🚀 Exporting all study calculations into SQLite tables...")

	// Create/refresh output tables.
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS tecl_allocation_matrix (
			allocation_pct REAL PRIMARY KEY,
			net_profit REAL,
			ending_capital REAL,
			total_return_pct REAL,
			cagr_pct REAL,
			max_mtm_drawdown_pct REAL,
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
			profit_factor REAL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)

	// ------------------------------------------------------------------
	// Load core bar data.
	// ------------------------------------------------------------------
	vooMap, _, _ := storage.FetchBars(db, "backtest_start", []string{"VOO"}, "", "")
	teclMap, _, _ := storage.FetchBars(db, "backtest_start", []string{"TECL"}, "", "")
	spxuMap, _, _ := storage.FetchBars(db, "backtest_start", []string{"SPXU"}, "", "")
	
	vooBars := vooMap["VOO"]
	teclBars := teclMap["TECL"]
	spxuBars := spxuMap["SPXU"]

	// Build shared sorted dates across all VOO bars.
	sortedDates := sortedDatesFrom(vooBars, teclBars, spxuBars)

	const initialCapital = 100000.0

	// Helper: run a single-leg simulation.
	runLeg := func(tradeSym string, tradeBars []models.Bar, sigDays int, dir string, regimeFilter string, tp, sl float64, hold int) models.PerformanceReport {
		barsBySymbol := map[string][]models.Bar{"VOO": vooBars, tradeSym: tradeBars}
		cfg := strategy.StrategyConfig{
			AllocationPct: 0.65, TakeProfitPct: tp, StopLossPct: sl,
			HoldingWindow: hold, PositionCap: 1, CashYieldAnnual: 0.045,
		}
		sigs := buildSignals(vooBars, tradeBars, sigDays, dir, regimeFilter, tp, sl, hold, tradeSym)
		sim := simulator.NewPortfolioSimulator(cfg, initialCapital)
		report, _, _ := sim.Run(sigs, barsBySymbol, sortedDates)
		return report
	}

	// ------------------------------------------------------------------
	// 2. TECL Allocation Matrix.
	// ------------------------------------------------------------------
	allocTiers := []float64{0.30, 0.40, 0.50, 0.60, 0.65, 0.75, 0.80, 0.85, 0.90, 1.00}
	tx1, _ := db.Beginx()
	stmt1, _ := tx1.Preparex(`
		INSERT OR REPLACE INTO tecl_allocation_matrix (
			allocation_pct, net_profit, ending_capital, total_return_pct, cagr_pct,
			max_mtm_drawdown_pct, calmar_ratio, total_trades, win_rate_pct
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	for _, a := range allocTiers {
		sigs := buildSignals(vooBars, teclBars, 3, "drop", "", 0.05, 0.0, 8, "TECL")
		cfg := strategy.StrategyConfig{AllocationPct: a, TakeProfitPct: 0.05, HoldingWindow: 8, PositionCap: 1, CashYieldAnnual: 0.045}
		sim := simulator.NewPortfolioSimulator(cfg, initialCapital)
		barsBySymbol := map[string][]models.Bar{"VOO": vooBars, "TECL": teclBars}
		res, _, _ := sim.Run(sigs, barsBySymbol, sortedDates)
		_, _ = stmt1.Exec(a, res.NetProfit, res.FinalEquity, res.TotalReturnPct, res.CAGR, res.MaxDrawdownPct, res.CalmarRatio, res.TotalTrades, res.WinRate*100)
	}
	stmt1.Close()
	_ = tx1.Commit()
	fmt.Println("✅ Populated tecl_allocation_matrix")

	// ------------------------------------------------------------------
	// 3. 6 3x Leveraged ETFs Head-to-Head.
	// ------------------------------------------------------------------
	etfMeta := []struct{ Sym, Name string }{
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
		barMap, _, err := storage.FetchBars(db, "backtest_start", []string{em.Sym}, "", "")
		bars := barMap[em.Sym]
		if err != nil || len(bars) == 0 {
			continue
		}
		res := runLeg(em.Sym, bars, 3, "drop", "", 0.05, 0.0, 8)
		_, _ = stmt2.Exec(em.Sym, em.Name, res.NetProfit, res.FinalEquity, res.TotalReturnPct, res.CAGR, res.MaxDrawdownPct, res.CalmarRatio, res.WinRate*100, res.TotalTrades, res.AvgHoldingDays)
	}
	stmt2.Close()
	_ = tx2.Commit()
	fmt.Println("✅ Populated compare_3x_etfs_matrix")

	// ------------------------------------------------------------------
	// 4. Inverse 3x Reversion Matrix.
	// ------------------------------------------------------------------
	inverseMeta := []string{"SPXU", "SQQQ", "SOXS"}
	tx3, _ := db.Beginx()
	stmt3, _ := tx3.Preparex(`
		INSERT OR REPLACE INTO inverse_3x_reversion_matrix (
			symbol, regime_filter, net_profit, ending_capital, total_return_pct, cagr_pct,
			max_mtm_drawdown_pct, calmar_ratio, win_rate_pct, total_trades
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	for _, sym := range inverseMeta {
		barMap, _, err := storage.FetchBars(db, "backtest_start", []string{sym}, "", "")
		bars := barMap[sym]
		if err != nil || len(bars) == 0 {
			continue
		}
		// All regimes
		resAll := runLeg(sym, bars, 3, "rally", "", 0.05, 0.0, 8)
		_, _ = stmt3.Exec(sym, "All Regimes", resAll.NetProfit, resAll.FinalEquity, resAll.TotalReturnPct, resAll.CAGR, resAll.MaxDrawdownPct, resAll.CalmarRatio, resAll.WinRate*100, resAll.TotalTrades)
		// Bear regime (VOO < SMA200)
		resBear := runLeg(sym, bars, 3, "rally", "VOO<SMA200", 0.06, 0.05, 2)
		_, _ = stmt3.Exec(sym, "VOO<SMA200", resBear.NetProfit, resBear.FinalEquity, resBear.TotalReturnPct, resBear.CAGR, resBear.MaxDrawdownPct, resBear.CalmarRatio, resBear.WinRate*100, resBear.TotalTrades)
	}
	stmt3.Close()
	_ = tx3.Commit()
	fmt.Println("✅ Populated inverse_3x_reversion_matrix")

	// ------------------------------------------------------------------
	// 5. All-Weather Combo via unified VOOTECLSPXUCombo Strategy.
	// ------------------------------------------------------------------
	comboStrat := strategy.NewVOOTECLSPXUCombo()
	comboCfg := comboStrat.DefaultConfig()
	barsBySymbol := map[string][]models.Bar{"VOO": vooBars, "TECL": teclBars, "SPXU": spxuBars}
	comboSigs := comboStrat.GenerateSignals(barsBySymbol)
	comboSim := simulator.NewPortfolioSimulator(comboCfg, initialCapital)
	comboReport, comboTrades, _ := comboSim.Run(comboSigs, barsBySymbol, sortedDates)

	_, _ = db.Exec("DELETE FROM all_weather_combo_trades;")
	tx4, _ := db.Beginx()
	stmt4, _ := tx4.Preparex(`
		INSERT INTO all_weather_combo_trades (
			trade_num, direction, asset, signal_date, entry_date, entry_price,
			exit_date, exit_price, hold_days, exit_reason, return_pct, net_pnl, is_win
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	longTrades, shortTrades, longWins, shortWins := 0, 0, 0, 0
	for i, t := range comboTrades {
		isWin := t.NetPnL > 0
		winInt := 0
		if isWin {
			winInt = 1
		}
		dir := "LONG"
		if t.Symbol == "SPXU" {
			dir = "SHORT"
			shortTrades++
			if isWin {
				shortWins++
			}
		} else {
			longTrades++
			if isWin {
				longWins++
			}
		}
		retPct := (t.ExitPrice - t.EntryPrice) / t.EntryPrice * 100.0
		netPnL := t.NetPnL
		_, _ = stmt4.Exec(i+1, dir, t.Symbol, t.EntryDate, t.EntryDate, t.EntryPrice, t.ExitDate, t.ExitPrice, t.HoldDays, t.ExitReason, retPct, netPnL, winInt)
	}
	stmt4.Close()
	_ = tx4.Commit()

	longWinRate := 0.0
	if longTrades > 0 {
		longWinRate = float64(longWins) / float64(longTrades) * 100.0
	}
	shortWinRate := 0.0
	if shortTrades > 0 {
		shortWinRate = float64(shortWins) / float64(shortTrades) * 100.0
	}
	_ = longWinRate
	_ = shortWinRate

	_, _ = db.Exec(`
		INSERT OR REPLACE INTO all_weather_combo_summary (
			strategy_name, initial_capital, ending_capital, net_profit, total_return_pct,
			cagr_pct, max_mtm_drawdown_pct, calmar_ratio, win_rate_pct, total_trades,
			long_trades, short_trades, profit_factor
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, "All-Weather TECL (Long Dips) + SPXU (Short Bear Rallies)",
		initialCapital, comboReport.FinalEquity, comboReport.NetProfit, comboReport.TotalReturnPct,
		comboReport.CAGR, comboReport.MaxDrawdownPct, comboReport.CalmarRatio, comboReport.WinRate*100, comboReport.TotalTrades,
		longTrades, shortTrades, comboReport.ProfitFactor)

	fmt.Println("✅ Populated all_weather_combo_trades & summary")

	// ------------------------------------------------------------------
	// 6. Copy HTML reports adjacent to SQLite databases.
	// ------------------------------------------------------------------
	reportFiles, _ := filepath.Glob("reports/*.html")
	for _, rFile := range reportFiles {
		baseName := filepath.Base(rFile)
		dest := filepath.Join("data", baseName)
		copyFile(rFile, dest)
	}
	fmt.Printf("✅ Copied %d HTML study reports into data/\n", len(reportFiles))

	// 7. Master catalog.
	generateMasterCatalog("data/index.html")
	generateMasterCatalog("reports/index.html")
	fmt.Println("✅ Generated Master Study Catalog: data/index.html & reports/index.html")
}

// buildSignals generates []models.Signal for a single dip/rally leg.
func buildSignals(signalBars, tradeBars []models.Bar, consecutiveDays int, direction, regimeFilter string, tpPct, slPct float64, holdDays int, tradeSym string) []models.Signal {
	tradeByDate := make(map[string]models.Bar, len(tradeBars))
	for _, b := range tradeBars {
		tradeByDate[b.Date] = b
	}
	isLong := direction != "rally"
	var signals []models.Signal
	for i := consecutiveDays; i < len(signalBars); i++ {
		voo := signalBars[i]
		var detected bool
		if isLong {
			detected = true
			for s := 0; s < consecutiveDays; s++ {
				if signalBars[i-s].Close >= signalBars[i-s-1].Close {
					detected = false; break
				}
			}
		} else {
			detected = true
			for s := 0; s < consecutiveDays; s++ {
				if signalBars[i-s].Close <= signalBars[i-s-1].Close {
					detected = false; break
				}
			}
		}
		if !detected {
			continue
		}
		switch regimeFilter {
		case "VOO<SMA200":
			if voo.SMA200 > 0 && voo.Close >= voo.SMA200 {
				continue
			}
		case "VOO<SMA50":
			if voo.SMA50 > 0 && voo.Close >= voo.SMA50 {
				continue
			}
		case "VOO>=SMA200":
			if voo.SMA200 > 0 && voo.Close < voo.SMA200 {
				continue
			}
		}
		tb, ok := tradeByDate[voo.Date]
		if !ok || tb.Close <= 0 {
			continue
		}
		ep := tb.Close
		dir := "LONG"
		if !isLong {
			dir = "SHORT"
		}
		sig := models.Signal{
			Symbol: tradeSym, Date: voo.Date, Open: tb.Open, High: tb.High, Low: tb.Low,
			Close: ep, Volume: tb.Volume, Entry: 1, Direction: dir, HoldDaysOverride: holdDays,
		}
		if tpPct > 0 {
			sig.TakeProfit = ep * (1.0 + tpPct)
		}
		if slPct > 0 {
			sig.StopLoss = ep * (1.0 - slPct)
		}
		signals = append(signals, sig)
	}
	return signals
}

// sortedDatesFrom builds a sorted union of all dates from the provided bar slices.
func sortedDatesFrom(barSlices ...[]models.Bar) []string {
	dateSet := make(map[string]struct{})
	for _, bars := range barSlices {
		for _, b := range bars {
			dateSet[b.Date] = struct{}{}
		}
	}
	dates := make([]string, 0, len(dateSet))
	for d := range dateSet {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	return dates
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
    <title>Quantitative Backtest Studies &amp; SQLite Database Catalog</title>
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
            <h1 class="title">🏛️ Quantitative Backtest Studies &amp; SQLite Catalog</h1>
            <p class="subtitle">Complete inventory of research studies, underlying SQLite calculations, relational schema tables, and interactive Chart.js reports.</p>
        </div>

        <h2 class="section-title">📊 Primary Leveraged ETF Mean-Reversion Studies</h2>
        <div class="grid">
            <div class="card">
                <div>
                    <div class="card-header">
                        <div class="card-title">👑 All-Weather Dual Combo (TECL + SPXU)</div>
                        <span class="badge badge-win">40.7% CAGR</span>
                    </div>
                    <div class="card-desc">Combines Long TECL on 3-day dips (+5% TP / 8d hold) with Short SPXU on 3-day rallies during bear markets (+6% TP / -5% SL / 2d hold).</div>
                    <div class="card-meta">
                        <div class="meta-row"><span class="meta-label">SQLite DB:</span><span class="meta-val">data/sp500_etfs_study.db</span></div>
                        <div class="meta-row"><span class="meta-label">SQL Tables:</span><span class="meta-val">all_weather_combo_trades, summary</span></div>
                        <div class="meta-row"><span class="meta-label">Key Metrics:</span><span class="meta-val" style="color: #10b981;">+$452k Net PnL | 13.36% Max DD | Calmar 3.05</span></div>
                    </div>
                </div>
                <div class="card-actions">
                    <a href="tecl_spxu_optimized_combo.html" class="btn btn-primary" target="_blank">Open Interactive Report</a>
                </div>
            </div>

            <div class="card">
                <div>
                    <div class="card-header">
                        <div class="card-title">🚀 TECL Sizing Matrix (30% to 100%)</div>
                        <span class="badge badge-sql">30% - 100%</span>
                    </div>
                    <div class="card-desc">Full allocation sensitivity study showing portfolio growth, mark-to-market drawdowns, and Calmar ratios from 30% up to 100% dynamic sizing.</div>
                    <div class="card-meta">
                        <div class="meta-row"><span class="meta-label">SQLite DB:</span><span class="meta-val">data/sp500_etfs_study.db</span></div>
                        <div class="meta-row"><span class="meta-label">SQL Tables:</span><span class="meta-val">tecl_allocation_matrix</span></div>
                        <div class="meta-row"><span class="meta-label">Key Metrics:</span><span class="meta-val" style="color: #10b981;">50% Alloc = 10.27% DD / 25.8% CAGR</span></div>
                    </div>
                </div>
                <div class="card-actions">
                    <a href="tecl_allocations.html" class="btn btn-primary" target="_blank">Open Allocation Matrix</a>
                </div>
            </div>

            <div class="card">
                <div>
                    <div class="card-header">
                        <div class="card-title">⚔️ 6 3x Leveraged ETFs Head-to-Head</div>
                        <span class="badge badge-sql">6 Assets</span>
                    </div>
                    <div class="card-desc">Head-to-head comparison of TECL, TQQQ, FAS, UPRO, SOXL, and UDOW trading the VOO 3-day dip strategy with 4.5% Treasury yield.</div>
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

            <div class="card">
                <div>
                    <div class="card-header">
                        <div class="card-title">🐻 Bear Market Grid Search Optimizer</div>
                        <span class="badge badge-sql">Grid Search</span>
                    </div>
                    <div class="card-desc">Multi-dimensional parallel parameter search across SPXU, SQQQ, and SOXS testing streak lengths, holding periods, profit targets, and stop losses.</div>
                    <div class="card-meta">
                        <div class="meta-row"><span class="meta-label">SQLite DB:</span><span class="meta-val">data/sp500_etfs_study.db</span></div>
                        <div class="meta-row"><span class="meta-label">Rank #1 Setup:</span><span class="meta-val" style="color: #10b981;">SPXU 3d/2d/+6%/-5% (Calmar 3.06 / 3.25% DD)</span></div>
                    </div>
                </div>
                <div class="card-actions">
                    <a href="lib_gridsearch_bear.html" class="btn btn-primary" target="_blank">Open Grid Search Report</a>
                </div>
            </div>
        </div>
    </div>
</body>
</html>`

	_ = os.WriteFile(outputPath, []byte(html), 0644)
}
