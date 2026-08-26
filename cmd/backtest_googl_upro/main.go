package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
)

type BarData struct {
	Date     string  `db:"Date"`
	Open     float64 `db:"open"`
	High     float64 `db:"high"`
	Low      float64 `db:"low"`
	Close    float64 `db:"close"`
	AdjClose float64 `db:"Adj Close"`
	Volume   int64   `db:"volume"`
}

type DividendEvent struct {
	Symbol string  `db:"symbol"`
	ExDate string  `db:"ex_date"`
	Amount float64 `db:"amount"`
}

type DailyPoint struct {
	Date           string  `json:"date"`
	TotalEquity    float64 `json:"total_equity"`
	Drawdown       float64 `json:"drawdown"`
	VooEquity      float64 `json:"voo_equity"`
	VooDD          float64 `json:"voo_dd"`
	GooglEquity    float64 `json:"googl_equity"`
	GooglDD        float64 `json:"googl_dd"`
	GooglShares    int     `json:"googl_shares"`
	UproShares     int     `json:"upro_shares"`
	Cash           float64 `json:"cash"`
	InUproPosition bool    `json:"in_upro_position"`
	IsTradeEntry   bool    `json:"is_trade_entry"`
	IsTradeExit    bool    `json:"is_trade_exit"`
	TradePnl       float64 `json:"trade_pnl"`
	TradeReturnPct float64 `json:"trade_return_pct"`
}

type ExecutedTrade struct {
	TradeNum   int
	EntryDate  string
	ExitDate   string
	UproEntry  float64
	UproExit   float64
	HoldDays   int
	ExitReason string
	ReturnPct  float64
	NetPnL     float64
	IsWin      bool
}

func main() {
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	initialCapital := flag.Float64("capital", 100000.0, "Starting cash ($)")
	allocRatio := flag.Float64("alloc", 0.65, "Dynamic allocation ratio to UPRO on dip signal (0.65 = 65%)")
	htmlOutput := flag.String("html", "reports/rank6_googl_sweep_vs_voo.html", "Path to export interactive HTML comparison chart")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	var vooBars, uproBars, googlBars []BarData
	_ = db.Select(&vooBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = 'VOO' ORDER BY substr(Date, 1, 10) ASC;`)
	_ = db.Select(&uproBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = 'UPRO' ORDER BY substr(Date, 1, 10) ASC;`)
	_ = db.Select(&googlBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = 'GOOGL' ORDER BY substr(Date, 1, 10) ASC;`)

	var divList []DividendEvent
	_ = db.Select(&divList, `SELECT symbol, ex_date, amount FROM corporate_dividends WHERE symbol = 'GOOGL' ORDER BY ex_date ASC;`)
	googlDivMap := make(map[string]float64)
	for _, d := range divList {
		googlDivMap[d.ExDate] = d.Amount
	}

	uproMap := make(map[string]BarData)
	for _, b := range uproBars {
		uproMap[b.Date] = b
	}
	googlMap := make(map[string]BarData)
	for _, b := range googlBars {
		googlMap[b.Date] = b
	}

	var commonDates []string
	for _, b := range vooBars {
		if _, ok1 := uproMap[b.Date]; ok1 {
			if _, ok2 := googlMap[b.Date]; ok2 {
				commonDates = append(commonDates, b.Date)
			}
		}
	}

	vooDateMap := make(map[string]BarData)
	for _, b := range vooBars {
		vooDateMap[b.Date] = b
	}

	// Detect 3-day decline signals on VOO
	signalMap := make(map[string]bool)
	for i := 3; i < len(commonDates); i++ {
		d0 := commonDates[i]
		d1 := commonDates[i-1]
		d2 := commonDates[i-2]
		d3 := commonDates[i-3]

		if vooDateMap[d0].Close < vooDateMap[d1].Close &&
			vooDateMap[d1].Close < vooDateMap[d2].Close &&
			vooDateMap[d2].Close < vooDateMap[d3].Close {
			signalMap[d0] = true
		}
	}

	// Simulation Setup
	startGooglPrice := googlMap[commonDates[0]].Close
	googlShares := int(*initialCapital / startGooglPrice)
	cash := *initialCapital - float64(googlShares)*startGooglPrice
	uproShares := 0
	uproEntryPrice := 0.0

	// Benchmark holdings
	vooShares := *initialCapital / vooDateMap[commonDates[0]].Close
	googlOnlyShares := int(*initialCapital / startGooglPrice)
	googlOnlyCash := *initialCapital - float64(googlOnlyShares)*startGooglPrice

	inUproUntil := -1
	inUpro := false

	var trades []ExecutedTrade
	var dailyPoints []DailyPoint

	stratPeak := *initialCapital
	vooPeak := *initialCapital
	googlPeak := *initialCapital
	stratMaxDD := 0.0
	vooMaxDD := 0.0
	googlMaxDD := 0.0

	maxHoldDays := 8
	takeProfitPct := 0.05

	for i := 0; i < len(commonDates); i++ {
		date := commonDates[i]
		vooClose := vooDateMap[date].Close
		uproClose := uproMap[date].Close
		googlClose := googlMap[date].Close

		// Dividends for GOOGL
		if divAmt, hasDiv := googlDivMap[date]; hasDiv {
			if googlShares > 0 {
				cash += float64(googlShares) * divAmt
			}
			if googlOnlyShares > 0 {
				googlOnlyCash += float64(googlOnlyShares) * divAmt
			}
		}

		isEntry := false
		isExit := false
		tradePnL := 0.0
		tradeRet := 0.0

		// Check 3-Day Dip Signal Entry into UPRO (65% Allocation)
		if signalMap[date] && i > inUproUntil && !inUpro {
			totalPortVal := cash + float64(googlShares)*googlClose
			targetUproCap := totalPortVal * *allocRatio

			neededFromGoogl := targetUproCap - cash
			if neededFromGoogl > 0 && googlShares > 0 {
				sharesToSell := int(math.Ceil(neededFromGoogl / googlClose))
				if sharesToSell > googlShares {
					sharesToSell = googlShares
				}
				cash += float64(sharesToSell) * googlClose
				googlShares -= sharesToSell
			}

			buyCap := targetUproCap
			if cash < buyCap {
				buyCap = cash
			}
			shares := int(buyCap / uproClose)
			if shares > 0 {
				uproShares = shares
				uproEntryPrice = uproClose
				cash -= float64(uproShares) * uproEntryPrice
				inUpro = true
				isEntry = true

				exitIdx := i + maxHoldDays
				if exitIdx >= len(commonDates) {
					exitIdx = len(commonDates) - 1
				}

				actualExitIdx := exitIdx
				actualExitPrice := uproMap[commonDates[actualExitIdx]].Close
				actualExitReason := "TIME_UP_8DAYS"
				targetPrice := uproEntryPrice * (1.0 + takeProfitPct)

				for d := i + 1; d <= exitIdx; d++ {
					dDate := commonDates[d]
					if uproMap[dDate].High >= targetPrice {
						actualExitIdx = d
						actualExitPrice = targetPrice
						if uproMap[dDate].Open > targetPrice {
							actualExitPrice = uproMap[dDate].Open
						}
						actualExitReason = "🎯 PROFIT_TARGET_+5%"
						break
					}
				}

				pnl := float64(uproShares) * (actualExitPrice - uproEntryPrice)
				ret := (actualExitPrice - uproEntryPrice) / uproEntryPrice

				trades = append(trades, ExecutedTrade{
					TradeNum:   len(trades) + 1,
					EntryDate:  date,
					ExitDate:   commonDates[actualExitIdx],
					UproEntry:  uproEntryPrice,
					UproExit:   actualExitPrice,
					HoldDays:   actualExitIdx - i,
					ExitReason: actualExitReason,
					ReturnPct:  ret,
					NetPnL:     pnl,
					IsWin:      pnl > 0,
				})
				inUproUntil = actualExitIdx
			}
		}

		// Check Trade Exit: Liquidate UPRO and SWEEP PROCEEDS BACK INTO GOOGL!
		if inUpro && i == inUproUntil {
			currTrade := trades[len(trades)-1]
			realizedCash := float64(uproShares) * currTrade.UproExit
			cash += realizedCash
			tradePnL = currTrade.NetPnL
			tradeRet = currTrade.ReturnPct

			// Reinvest all available cash back into GOOGL shares
			newGooglShares := int(cash / googlClose)
			if newGooglShares > 0 {
				googlShares += newGooglShares
				cash -= float64(newGooglShares) * googlClose
			}

			inUpro = false
			uproShares = 0
			uproEntryPrice = 0.0
			isExit = true
		}

		// Equities & Drawdowns
		strategyEquity := cash + float64(googlShares)*googlClose + float64(uproShares)*uproClose
		vooEquity := vooShares * vooClose
		googlOnlyEquity := googlOnlyCash + float64(googlOnlyShares)*googlClose

		if strategyEquity > stratPeak {
			stratPeak = strategyEquity
		}
		stratDD := (stratPeak - strategyEquity) / stratPeak * 100.0
		if stratDD > stratMaxDD {
			stratMaxDD = stratDD
		}

		if vooEquity > vooPeak {
			vooPeak = vooEquity
		}
		vooDD := (vooPeak - vooEquity) / vooPeak * 100.0
		if vooDD > vooMaxDD {
			vooMaxDD = vooDD
		}

		if googlOnlyEquity > googlPeak {
			googlPeak = googlOnlyEquity
		}
		googlDD := (googlPeak - googlOnlyEquity) / googlPeak * 100.0
		if googlDD > googlMaxDD {
			googlMaxDD = googlDD
		}

		dailyPoints = append(dailyPoints, DailyPoint{
			Date:           date,
			TotalEquity:    strategyEquity,
			Drawdown:       stratDD,
			VooEquity:      vooEquity,
			VooDD:          vooDD,
			GooglEquity:    googlOnlyEquity,
			GooglDD:        googlDD,
			GooglShares:    googlShares,
			UproShares:     uproShares,
			Cash:           cash,
			InUproPosition: inUpro || isExit,
			IsTradeEntry:   isEntry,
			IsTradeExit:    isExit,
			TradePnl:       tradePnL,
			TradeReturnPct: tradeRet * 100.0,
		})
	}

	// Final Statistics
	stratFinal := dailyPoints[len(dailyPoints)-1].TotalEquity
	vooFinal := dailyPoints[len(dailyPoints)-1].VooEquity
	googlFinal := dailyPoints[len(dailyPoints)-1].GooglEquity

	stratProfit := stratFinal - *initialCapital
	vooProfit := vooFinal - *initialCapital
	googlProfit := googlFinal - *initialCapital

	stratCAGR := (math.Pow(stratFinal / *initialCapital, 1.0/5.0) - 1.0) * 100.0
	vooCAGR := (math.Pow(vooFinal / *initialCapital, 1.0/5.0) - 1.0) * 100.0
	googlCAGR := (math.Pow(googlFinal / *initialCapital, 1.0/5.0) - 1.0) * 100.0

	wins := 0
	grossGains := 0.0
	grossLosses := 0.0
	for _, t := range trades {
		if t.IsWin {
			wins++
			grossGains += t.NetPnL
		} else {
			grossLosses += math.Abs(t.NetPnL)
		}
	}
	winRate := float64(wins) / float64(len(trades)) * 100.0
	pf := grossGains / grossLosses

	fmt.Printf("\n=====================================================================================================\n")
	fmt.Printf("🚀 3-DAY DROP / 8-DAY HOLD / +5%% TP (65%% ALLOC) + GOOGL IDLE RESERVE SWEEP\n")
	fmt.Printf("=====================================================================================================\n\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Performance Metric", "65% UPRO Dip + GOOGL Sweep", "GOOGL Buy & Hold Benchmark", "VOO Buy & Hold Benchmark"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	table.Append([]string{"Starting Capital", fmt.Sprintf("$%.2f", *initialCapital), fmt.Sprintf("$%.2f", *initialCapital), fmt.Sprintf("$%.2f", *initialCapital)})
	table.Append([]string{"Ending Capital", fmt.Sprintf("💰 $%.2f", stratFinal), fmt.Sprintf("$%.2f", googlFinal), fmt.Sprintf("$%.2f", vooFinal)})
	table.Append([]string{"Total Net Profit", fmt.Sprintf("+$%.2f", stratProfit), fmt.Sprintf("+$%.2f", googlProfit), fmt.Sprintf("+$%.2f", vooProfit)})
	table.Append([]string{"Total Return (%)", fmt.Sprintf("+%.2f%%", (stratFinal-*initialCapital) / *initialCapital * 100.0), fmt.Sprintf("+%.2f%%", (googlFinal-*initialCapital) / *initialCapital * 100.0), fmt.Sprintf("+%.2f%%", (vooFinal-*initialCapital) / *initialCapital * 100.0)})
	table.Append([]string{"CAGR (Annualized)", fmt.Sprintf("🔥 %.2f%% / yr", stratCAGR), fmt.Sprintf("%.2f%% / yr", googlCAGR), fmt.Sprintf("%.2f%% / yr", vooCAGR)})
	table.Append([]string{"Max MTM Drawdown", fmt.Sprintf("🔴 %.2f%%", stratMaxDD), fmt.Sprintf("🔴 %.2f%%", googlMaxDD), fmt.Sprintf("🔴 %.2f%%", vooMaxDD)})
	table.Append([]string{"Strategy Trades", fmt.Sprintf("%d Trades (%d W / %d L)", len(trades), wins, len(trades)-wins), "0 Trades", "1 Trade"})
	table.Append([]string{"Trade Win Rate", fmt.Sprintf("🔥 %.1f%%", winRate), "N/A", "N/A"})
	table.Append([]string{"Profit Factor", fmt.Sprintf("🔥 %.2f", pf), "N/A", "N/A"})

	table.Render()

	// Generate HTML Report
	generateGooglHTMLReport(*htmlOutput, dailyPoints, trades, stratFinal, vooFinal, googlFinal, stratProfit, vooProfit, googlProfit, stratMaxDD, vooMaxDD, googlMaxDD, winRate, pf)
	fmt.Printf("\n✨ Interactive GOOGL sweep comparison chart saved to: %s\n\n", *htmlOutput)
}

func generateGooglHTMLReport(
	outputPath string,
	dailySeries []DailyPoint,
	trades []ExecutedTrade,
	stratEnd, vooEnd, googlEnd, stratPnl, vooPnl, googlPnl, stratDD, vooDD, googlDD, winRate, pf float64,
) {
	dailyJSON, _ := json.Marshal(dailySeries)

	tradeRows := ""
	for _, t := range trades {
		badgeClass := "badge-win"
		badgeText := "🟢 WIN"
		colorClass := "pos"
		if !t.IsWin {
			badgeClass = "badge-loss"
			badgeText = "🔴 LOSS"
			colorClass = "neg"
		}
		tradeRows += fmt.Sprintf(`
			<tr onclick="focusDate('%s')" style="cursor: pointer;" title="Click to zoom timeline to this trade">
				<td><strong>#%d</strong></td>
				<td>%s ➔ %s</td>
				<td>$%0.2f</td>
				<td>$%0.2f</td>
				<td>%dd</td>
				<td>%s</td>
				<td class="%s">%+0.2f%%</td>
				<td class="%s">$%+0.2f</td>
				<td><span class="badge %s">%s</span></td>
			</tr>
		`, t.EntryDate, t.TradeNum, t.EntryDate, t.ExitDate, t.UproEntry, t.UproExit, t.HoldDays, t.ExitReason,
			colorClass, t.ReturnPct*100, colorClass, t.NetPnL, badgeClass, badgeText)
	}

	htmlTemplate := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>UPRO 65% Dip Strategy + GOOGL Idle Reserve Sweep</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
    <style>
        :root {
            --bg: #090d16;
            --card: #111827;
            --card-hover: #172033;
            --border: #1e293b;
            --text: #f8fafc;
            --text-dim: #94a3b8;
            --accent: #38bdf8;
            --strategy: #10b981;
            --benchmark: #a855f7;
            --googl: #4285f4;
            --danger: #f43f5e;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; }
        body { background: var(--bg); color: var(--text); padding: 32px 20px; line-height: 1.6; }
        .container { max-width: 1280px; margin: 0 auto; }
        .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; padding-bottom: 16px; border-bottom: 1px solid var(--border); flex-wrap: wrap; gap: 12px; }
        .title { font-size: 24px; font-weight: 800; color: #fff; letter-spacing: -0.5px; }
        .subtitle { color: var(--text-dim); font-size: 14px; margin-top: 4px; }
        .controls { display: flex; gap: 8px; align-items: center; }
        .btn { background: #1e293b; color: var(--text); border: 1px solid var(--border); padding: 6px 14px; border-radius: 8px; font-size: 13px; font-weight: 600; cursor: pointer; transition: all 0.2s ease; }
        .btn:hover, .btn.active { background: var(--accent); color: #090d16; border-color: var(--accent); }
        
        .kpi-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px; margin-bottom: 24px; }
        .kpi-card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 20px; box-shadow: 0 4px 12px rgba(0,0,0,0.2); }
        .kpi-label { font-size: 13px; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.5px; }
        .kpi-value { font-size: 26px; font-weight: 800; margin-top: 6px; color: #fff; }
        .kpi-sub { font-size: 12px; color: var(--accent); margin-top: 4px; }
        
        .chart-card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 24px; margin-bottom: 24px; }
        .chart-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 18px; flex-wrap: wrap; gap: 10px; }
        .chart-title { font-size: 17px; font-weight: 700; color: #fff; display: flex; align-items: center; gap: 8px; }
        .legend-tag { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; margin-left: 12px; }
        .dot { width: 10px; height: 10px; border-radius: 50%; display: inline-block; }
        
        .table-card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 24px; overflow-x: auto; margin-top: 24px; }
        table { width: 100%; border-collapse: collapse; text-align: left; font-size: 14px; }
        th { color: var(--text-dim); font-weight: 600; padding: 12px 14px; border-bottom: 1px solid var(--border); }
        td { padding: 12px 14px; border-bottom: 1px solid rgba(255,255,255,0.05); }
        tbody tr:hover { background: var(--card-hover); }
        .pos { color: var(--strategy); font-weight: 700; }
        .neg { color: var(--danger); font-weight: 700; }
        .badge { padding: 3px 8px; border-radius: 6px; font-size: 11px; font-weight: 700; }
        .badge-win { background: rgba(16, 185, 129, 0.15); color: var(--strategy); border: 1px solid rgba(16, 185, 129, 0.3); }
        .badge-loss { background: rgba(244, 63, 94, 0.15); color: var(--danger); border: 1px solid rgba(244, 63, 94, 0.3); }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div>
                <h1 class="title">🚀 65% UPRO Dip Strategy + GOOGL Idle Reserve Sweep</h1>
                <p class="subtitle">Holding Idle Reserves in GOOGL & Rotating into UPRO on 3-Day Dips ($100k Base)</p>
            </div>
            <div class="controls">
                <button class="btn" onclick="setTimeframe('all')" id="btn-all">5 Years (All)</button>
                <button class="btn" onclick="setTimeframe('2y')" id="btn-2y">2 Years</button>
                <button class="btn" onclick="setTimeframe('1y')" id="btn-1y">1 Year</button>
                <button class="btn" onclick="setTimeframe('6m')" id="btn-6m">6 Months</button>
            </div>
        </div>

        <div class="kpi-grid">
            <div class="kpi-card">
                <div class="kpi-label">Strategy Final Equity</div>
                <div class="kpi-value" style="color: var(--strategy);">${{STRAT_END}}</div>
                <div class="kpi-sub">+${{STRAT_PNL}} Total Profit | {{WIN_RATE}}% Win Rate</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">GOOGL Buy & Hold Final</div>
                <div class="kpi-value" style="color: var(--googl);">${{GOOGL_END}}</div>
                <div class="kpi-sub">+${{GOOGL_PNL}} Total Profit | {{GOOGL_DD}}% Max DD</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">Strategy Max Drawdown</div>
                <div class="kpi-value" style="color: #38bdf8;">{{STRAT_DD}}%</div>
                <div class="kpi-sub">Reduced from GOOGL's -44.3% Drawdown</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">VOO Buy & Hold Final</div>
                <div class="kpi-value" style="color: var(--benchmark);">${{VOO_END}}</div>
                <div class="kpi-sub">+${{VOO_PNL}} Profit | 25.41% Max DD</div>
            </div>
        </div>

        <!-- MAIN EQUITY COMPARISON CHART -->
        <div class="chart-card">
            <div class="chart-header">
                <div class="chart-title">
                    <span>💰 Cumulative Portfolio Equity ($100,000 Starting Cash)</span>
                    <span class="legend-tag"><span class="dot" style="background: #10b981;"></span><strong>Strategy (65% UPRO Dip + GOOGL Sweep)</strong></span>
                    <span class="legend-tag"><span class="dot" style="background: #4285f4;"></span><strong>GOOGL Buy & Hold</strong></span>
                    <span class="legend-tag"><span class="dot" style="background: #a855f7;"></span><strong>VOO Buy & Hold</strong></span>
                </div>
            </div>
            <div style="height: 420px; position: relative;">
                <canvas id="equityChart"></canvas>
            </div>
        </div>

        <!-- DRAWDOWN UNDERWATER CHART -->
        <div class="chart-card">
            <div class="chart-header">
                <div class="chart-title">
                    <span>📉 Mark-to-Market Drawdown Comparison (%)</span>
                </div>
            </div>
            <div style="height: 220px; position: relative;">
                <canvas id="drawdownChart"></canvas>
            </div>
        </div>

        <!-- EXECUTED TRADES TABLE -->
        <div class="table-card">
            <div class="chart-title" style="margin-bottom: 16px;">
                <span>📜 All Strategy Trades Executed on UPRO</span>
            </div>
            <table>
                <thead>
                    <tr>
                        <th>#</th>
                        <th>Trade Window</th>
                        <th>UPRO Entry</th>
                        <th>Exit Price</th>
                        <th>Hold</th>
                        <th>Exit Reason</th>
                        <th>Return %</th>
                        <th>Net PnL ($)</th>
                        <th>Result</th>
                    </tr>
                </thead>
                <tbody>
                    {{TRADE_ROWS}}
                </tbody>
            </table>
        </div>
    </div>

    <script>
        const rawPoints = {{DAILY_JSON}};
        let currentFilter = 'all';

        function getFilteredPoints() {
            if (currentFilter === '6m') return rawPoints.slice(-126);
            if (currentFilter === '1y') return rawPoints.slice(-252);
            if (currentFilter === '2y') return rawPoints.slice(-504);
            return rawPoints;
        }

        // Equity Chart
        const equityCtx = document.getElementById('equityChart').getContext('2d');
        let equityChart = new Chart(equityCtx, {
            type: 'line',
            data: buildEquityData(getFilteredPoints()),
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { mode: 'index', intersect: false },
                plugins: {
                    legend: { display: false },
                    tooltip: {
                        backgroundColor: '#111827',
                        borderColor: '#1e293b',
                        borderWidth: 1,
                        padding: 12,
                        callbacks: {
                            label: function(context) {
                                let label = context.dataset.label || '';
                                if (label) label += ': ';
                                if (context.parsed.y !== null) {
                                    label += '$' + context.parsed.y.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
                                }
                                return label;
                            }
                        }
                    }
                },
                scales: {
                    x: { grid: { color: 'rgba(255,255,255,0.03)' }, ticks: { color: '#94a3b8', maxTicksLimit: 12 } },
                    y: { grid: { color: 'rgba(255,255,255,0.05)' }, ticks: { color: '#94a3b8', callback: function(v) { return '$' + (v/1000) + 'k'; } } }
                }
            }
        });

        // Drawdown Chart
        const ddCtx = document.getElementById('drawdownChart').getContext('2d');
        let ddChart = new Chart(ddCtx, {
            type: 'line',
            data: buildDrawdownData(getFilteredPoints()),
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { mode: 'index', intersect: false },
                plugins: {
                    legend: { display: false },
                    tooltip: {
                        backgroundColor: '#111827',
                        borderColor: '#1e293b',
                        borderWidth: 1,
                        padding: 10,
                        callbacks: {
                            label: function(context) {
                                return context.dataset.label + ': -' + context.parsed.y.toFixed(2) + '%';
                            }
                        }
                    }
                },
                scales: {
                    x: { grid: { color: 'rgba(255,255,255,0.03)' }, ticks: { color: '#94a3b8', maxTicksLimit: 12 } },
                    y: { grid: { color: 'rgba(255,255,255,0.05)' }, ticks: { color: '#94a3b8', callback: function(v) { return '-' + v + '%'; } } }
                }
            }
        });

        function buildEquityData(pts) {
            return {
                labels: pts.map(p => p.date),
                datasets: [
                    {
                        label: '65% UPRO Dip + GOOGL Sweep',
                        data: pts.map(p => p.total_equity),
                        borderColor: '#10b981',
                        borderWidth: 2.5,
                        pointRadius: 0,
                        tension: 0.1
                    },
                    {
                        label: 'GOOGL Buy & Hold',
                        data: pts.map(p => p.googl_equity),
                        borderColor: '#4285f4',
                        borderWidth: 2,
                        borderDash: [3, 3],
                        pointRadius: 0,
                        tension: 0.1
                    },
                    {
                        label: 'VOO Buy & Hold',
                        data: pts.map(p => p.voo_equity),
                        borderColor: '#a855f7',
                        borderWidth: 1.5,
                        borderDash: [4, 4],
                        pointRadius: 0,
                        tension: 0.1
                    }
                ]
            };
        }

        function buildDrawdownData(pts) {
            return {
                labels: pts.map(p => p.date),
                datasets: [
                    {
                        label: 'Strategy Drawdown',
                        data: pts.map(p => p.drawdown),
                        borderColor: '#10b981',
                        backgroundColor: 'rgba(16, 185, 129, 0.15)',
                        borderWidth: 1.5,
                        pointRadius: 0,
                        fill: true,
                        tension: 0.1
                    },
                    {
                        label: 'GOOGL Drawdown',
                        data: pts.map(p => p.googl_dd),
                        borderColor: '#4285f4',
                        backgroundColor: 'rgba(66, 133, 244, 0.08)',
                        borderWidth: 1.5,
                        pointRadius: 0,
                        fill: true,
                        tension: 0.1
                    },
                    {
                        label: 'VOO Drawdown',
                        data: pts.map(p => p.voo_dd),
                        borderColor: '#a855f7',
                        backgroundColor: 'rgba(168, 85, 247, 0.08)',
                        borderWidth: 1.5,
                        pointRadius: 0,
                        fill: true,
                        tension: 0.1
                    }
                ]
            };
        }

        function setTimeframe(tf) {
            currentFilter = tf;
            document.querySelectorAll('.controls .btn').forEach(b => b.classList.remove('active'));
            const btn = document.getElementById('btn-' + tf);
            if (btn) btn.classList.add('active');

            const filtered = getFilteredPoints();
            equityChart.data = buildEquityData(filtered);
            equityChart.update();
            ddChart.data = buildDrawdownData(filtered);
            ddChart.update();
        }

        function focusDate(dateStr) {
            const idx = rawPoints.findIndex(p => p.date === dateStr);
            if (idx === -1) return;
            const start = Math.max(0, idx - 20);
            const end = Math.min(rawPoints.length, idx + 30);
            const zoomed = rawPoints.slice(start, end);

            document.querySelectorAll('.controls .btn').forEach(b => b.classList.remove('active'));
            equityChart.data = buildEquityData(zoomed);
            equityChart.update();
            ddChart.data = buildDrawdownData(zoomed);
            ddChart.update();
            window.scrollTo({ top: 120, behavior: 'smooth' });
        }

        document.getElementById('btn-all').classList.add('active');
    </script>
</body>
</html>`

	replacements := map[string]string{
		"{{STRAT_END}}":     fmt.Sprintf("%0.2f", stratEnd),
		"{{VOO_END}}":       fmt.Sprintf("%0.2f", vooEnd),
		"{{GOOGL_END}}":     fmt.Sprintf("%0.2f", googlEnd),
		"{{STRAT_PNL}}":     fmt.Sprintf("%0.2f", stratPnl),
		"{{VOO_PNL}}":       fmt.Sprintf("%0.2f", vooPnl),
		"{{GOOGL_PNL}}":     fmt.Sprintf("%0.2f", googlPnl),
		"{{STRAT_DD}}":      fmt.Sprintf("%0.2f", stratDD),
		"{{VOO_DD}}":        fmt.Sprintf("%0.2f", vooDD),
		"{{GOOGL_DD}}":      fmt.Sprintf("%0.2f", googlDD),
		"{{WIN_RATE}}":      fmt.Sprintf("%0.1f", winRate),
		"{{TRADE_ROWS}}":    tradeRows,
		"{{DAILY_JSON}}":    string(dailyJSON),
	}

	content := htmlTemplate
	for k, v := range replacements {
		content = strings.ReplaceAll(content, k, v)
	}

	_ = os.WriteFile(outputPath, []byte(content), 0644)
}
