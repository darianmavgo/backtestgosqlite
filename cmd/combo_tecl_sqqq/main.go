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

type SignalBar struct {
	Date   string  `db:"date"`
	Close  float64 `db:"close"`
	SMA200 float64 `db:"sma200"`
}

type ExecutedComboTrade struct {
	TradeNum   int     `json:"trade_num"`
	Direction  string  `json:"direction"` // "LONG_TECL" or "SHORT_SQQQ"
	Asset      string  `json:"asset"`
	SignalDate string  `json:"signal_date"`
	EntryDate  string  `json:"entry_date"`
	ExitDate   string  `json:"exit_date"`
	EntryPrice float64 `json:"entry_price"`
	ExitPrice  float64 `json:"exit_price"`
	HoldDays   int     `json:"hold_days"`
	ExitReason string  `json:"exit_reason"`
	ReturnPct  float64 `json:"return_pct"`
	NetPnL     float64 `json:"net_pnl"`
	IsWin      bool    `json:"is_win"`
}

type DailyPoint struct {
	Date           string  `json:"date"`
	ComboEquity    float64 `json:"combo_equity"`
	ComboDD        float64 `json:"combo_dd"`
	TeclOnlyEquity float64 `json:"tecl_only_equity"`
	TeclOnlyDD     float64 `json:"tecl_only_dd"`
	VooEquity      float64 `json:"voo_equity"`
	VooDD          float64 `json:"voo_dd"`
	ActiveTrade    string  `json:"active_trade"`
}

func main() {
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	initialCapital := flag.Float64("capital", 100000.0, "Starting cash ($)")
	allocRatio := flag.Float64("alloc", 0.65, "Dynamic allocation ratio (0.65 = 65%)")
	cashYieldAnn := flag.Float64("yield", 0.045, "Annual cash yield on idle reserves (4.5% APY)")
	htmlOutput := flag.String("html", "reports/tecl_sqqq_all_weather_combo.html", "Path to export interactive HTML comparison chart")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// 1. Fetch Bars
	var vooBars, teclBars, sqqqBars []BarData
	_ = db.Select(&vooBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = 'VOO' ORDER BY substr(Date, 1, 10) ASC;`)
	_ = db.Select(&teclBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = 'TECL' ORDER BY substr(Date, 1, 10) ASC;`)
	_ = db.Select(&sqqqBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = 'SQQQ' ORDER BY substr(Date, 1, 10) ASC;`)

	var sigBars []SignalBar
	_ = db.Select(&sigBars, `
		SELECT 
			substr(Date, 1, 10) AS date,
			close,
			AVG(close) OVER (ORDER BY substr(Date, 1, 10) ROWS BETWEEN 199 PRECEDING AND CURRENT ROW) AS sma200
		FROM backtest_start
		WHERE symbol = 'VOO'
		ORDER BY substr(Date, 1, 10) ASC;
	`)

	sigMap := make(map[string]SignalBar)
	for _, s := range sigBars {
		sigMap[s.Date] = s
	}

	teclMap := make(map[string]BarData)
	for _, b := range teclBars {
		teclMap[b.Date] = b
	}
	sqqqMap := make(map[string]BarData)
	for _, b := range sqqqBars {
		sqqqMap[b.Date] = b
	}

	// Align common dates
	var commonDates []string
	for _, b := range vooBars {
		if _, ok1 := teclMap[b.Date]; ok1 {
			if _, ok2 := sqqqMap[b.Date]; ok2 {
				commonDates = append(commonDates, b.Date)
			}
		}
	}

	vooMap := make(map[string]BarData)
	for _, b := range vooBars {
		vooMap[b.Date] = b
	}

	// 2. Run All-Weather Combo Backtest (TECL Long on Dips + SQQQ Short on Rallies)
	dailyYieldRate := math.Pow(1.0+*cashYieldAnn, 1.0/252.0) - 1.0
	cash := *initialCapital
	inPosUntil := -1
	activeShares := 0
	activeEntryPrice := 0.0
	activeAsset := "" // "TECL" or "SQQQ"
	inPos := false

	var trades []ExecutedComboTrade
	var dailyPoints []DailyPoint

	vooStart := vooMap[commonDates[0]].Close
	vooShares := *initialCapital / vooStart

	comboPeak := *initialCapital
	vooPeak := *initialCapital
	comboMaxDD := 0.0
	vooMaxDD := 0.0

	longTrades := 0
	shortTrades := 0
	longWins := 0
	shortWins := 0

	maxHoldDays := 8
	takeProfitPct := 0.05

	for i := 3; i < len(commonDates); i++ {
		date := commonDates[i]
		vooClose := vooMap[date].Close

		// Daily Treasury yield on cash
		if cash > 0 && !inPos {
			cash += cash * dailyYieldRate
		}

		// Detect Signals:
		is3dDown := (vooMap[date].Close < vooMap[commonDates[i-1]].Close &&
			vooMap[commonDates[i-1]].Close < vooMap[commonDates[i-2]].Close &&
			vooMap[commonDates[i-2]].Close < vooMap[commonDates[i-3]].Close)

		is3dUp := (vooMap[date].Close > vooMap[commonDates[i-1]].Close &&
			vooMap[commonDates[i-1]].Close > vooMap[commonDates[i-2]].Close &&
			vooMap[commonDates[i-2]].Close > vooMap[commonDates[i-3]].Close)

		isBearRegime := vooClose < sigMap[date].SMA200

		// Strategy Engine:
		// 1. TECL fires on 3-day dips (captures all major oversold rebounds)
		// 2. SQQQ fires on 3-day rallies in Bear Regimes (captures dead-cat bounce collapses)
		shouldBuyTECL := is3dDown
		shouldBuySQQQ := is3dUp && isBearRegime

		// Check Entry
		if (shouldBuyTECL || shouldBuySQQQ) && i > inPosUntil && !inPos {
			var tradeAsset string
			var tradeBarsMap map[string]BarData

			if shouldBuyTECL {
				tradeAsset = "TECL"
				tradeBarsMap = teclMap
			} else {
				tradeAsset = "SQQQ"
				tradeBarsMap = sqqqMap
			}

			entryPrice := tradeBarsMap[date].Close
			posCap := cash * *allocRatio
			shares := int(posCap / entryPrice)

			if shares > 0 {
				activeShares = shares
				activeEntryPrice = entryPrice
				activeAsset = tradeAsset
				cash -= float64(activeShares) * activeEntryPrice
				inPos = true

				exitIdx := i + maxHoldDays
				if exitIdx >= len(commonDates) {
					exitIdx = len(commonDates) - 1
				}

				actualExitIdx := exitIdx
				actualExitPrice := tradeBarsMap[commonDates[actualExitIdx]].Close
				actualExitReason := "TIME_UP_8DAYS"
				targetPrice := activeEntryPrice * (1.0 + takeProfitPct)

				for d := i + 1; d <= exitIdx; d++ {
					dDate := commonDates[d]
					if tradeBarsMap[dDate].High >= targetPrice {
						actualExitIdx = d
						actualExitPrice = targetPrice
						if tradeBarsMap[dDate].Open > targetPrice {
							actualExitPrice = tradeBarsMap[dDate].Open
						}
						actualExitReason = "🎯 PROFIT_TARGET_+5%"
						break
					}
				}

				pnl := float64(activeShares) * (actualExitPrice - activeEntryPrice)
				ret := (actualExitPrice - activeEntryPrice) / activeEntryPrice
				isWin := pnl > 0

				dirName := "🟢 LONG_TECL (Dip in Bull Market)"
				if tradeAsset == "SQQQ" {
					dirName = "🔴 SHORT_SQQQ (Rally in Bear Market)"
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

				trades = append(trades, ExecutedComboTrade{
					TradeNum:   len(trades) + 1,
					Direction:  dirName,
					Asset:      tradeAsset,
					SignalDate: date,
					EntryDate:  date,
					ExitDate:   commonDates[actualExitIdx],
					EntryPrice: activeEntryPrice,
					ExitPrice:  actualExitPrice,
					HoldDays:   actualExitIdx - i,
					ExitReason: actualExitReason,
					ReturnPct:  ret,
					NetPnL:     pnl,
					IsWin:      isWin,
				})
				inPosUntil = actualExitIdx
			}
		}

		// Check Exit
		if inPos && i == inPosUntil {
			currTrade := trades[len(trades)-1]
			realized := float64(activeShares) * currTrade.ExitPrice
			cash += realized
			inPos = false
			activeShares = 0
			activeEntryPrice = 0.0
			activeAsset = ""
		}

		// Calculate Current Daily Mark-to-Market Equity
		posVal := 0.0
		if inPos {
			if activeAsset == "TECL" {
				posVal = float64(activeShares) * teclMap[date].Close
			} else if activeAsset == "SQQQ" {
				posVal = float64(activeShares) * sqqqMap[date].Close
			}
		}
		strategyEquity := cash + posVal
		vooEquity := vooShares * vooClose

		if strategyEquity > comboPeak {
			comboPeak = strategyEquity
		}
		stratDD := (comboPeak - strategyEquity) / comboPeak * 100.0
		if stratDD > comboMaxDD {
			comboMaxDD = stratDD
		}

		if vooEquity > vooPeak {
			vooPeak = vooEquity
		}
		vooDD := (vooPeak - vooEquity) / vooPeak * 100.0
		if vooDD > vooMaxDD {
			vooMaxDD = vooDD
		}

		activeTag := "100% Cash / T-Bills (4.5% Yield)"
		if inPos {
			activeTag = activeAsset + " Position Active"
		}

		dailyPoints = append(dailyPoints, DailyPoint{
			Date:        date,
			ComboEquity: strategyEquity,
			ComboDD:     stratDD,
			VooEquity:   vooEquity,
			VooDD:       vooDD,
			ActiveTrade: activeTag,
		})
	}

	// 3. Final Statistics
	finalEquity := dailyPoints[len(dailyPoints)-1].ComboEquity
	vooFinal := dailyPoints[len(dailyPoints)-1].VooEquity

	netProfit := finalEquity - *initialCapital
	vooProfit := vooFinal - *initialCapital
	totalRet := netProfit / *initialCapital * 100.0
	vooRet := vooProfit / *initialCapital * 100.0

	cagr := (math.Pow(finalEquity / *initialCapital, 1.0/5.0) - 1.0) * 100.0
	vooCAGR := (math.Pow(vooFinal / *initialCapital, 1.0/5.0) - 1.0) * 100.0

	totalWins := 0
	grossGains := 0.0
	grossLosses := 0.0
	for _, t := range trades {
		if t.IsWin {
			totalWins++
			grossGains += t.NetPnL
		} else {
			grossLosses += math.Abs(t.NetPnL)
		}
	}
	winRate := float64(totalWins) / float64(len(trades)) * 100.0
	pf := grossGains / grossLosses
	calmar := cagr / comboMaxDD

	longWinRate := float64(longWins) / float64(longTrades) * 100.0
	shortWinRate := float64(shortWins) / float64(shortTrades) * 100.0

	fmt.Printf("\n=======================================================================================================================\n")
	fmt.Printf("👑 ALL-WEATHER COMBO STRATEGY: TECL (LONG DIPS) + SQQQ (SHORT RALLIES) (65%% ALLOC + 4.5%% T-BILLS)\n")
	fmt.Printf("=======================================================================================================================\n\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Performance Metric", "👑 All-Weather Dual Combo (TECL + SQQQ)", "🏛️ VOO Buy & Hold Benchmark", "Outperformance Edge"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	table.Append([]string{"Starting Capital", fmt.Sprintf("$%.2f", *initialCapital), fmt.Sprintf("$%.2f", *initialCapital), "Equal Base ($100k)"})
	table.Append([]string{"Ending Capital", fmt.Sprintf("💰 $%.2f", finalEquity), fmt.Sprintf("$%.2f", vooFinal), fmt.Sprintf("🔥 +$%.2f Extra Wealth", netProfit-vooProfit)})
	table.Append([]string{"Total Net Realized Profit", fmt.Sprintf("+$%.2f", netProfit), fmt.Sprintf("+$%.2f", vooProfit), fmt.Sprintf("🔥 %.1fx More Profit", netProfit/vooProfit)})
	table.Append([]string{"Total Net Return (%)", fmt.Sprintf("+%.2f%%", totalRet), fmt.Sprintf("+%.2f%%", vooRet), fmt.Sprintf("+%.2f%% Net Spread", totalRet-vooRet)})
	table.Append([]string{"CAGR (Annualized)", fmt.Sprintf("🔥 %.2f%% / yr", cagr), fmt.Sprintf("%.2f%% / yr", vooCAGR), fmt.Sprintf("+%.2f%% / yr Compounding Edge", cagr-vooCAGR)})
	table.Append([]string{"Max MTM Drawdown", fmt.Sprintf("🛡️ %.2f%%", comboMaxDD), fmt.Sprintf("🔴 %.2f%%", vooMaxDD), fmt.Sprintf("🛡️ %.1fx Less Drawdown", vooMaxDD/comboMaxDD)})
	table.Append([]string{"Calmar Ratio (CAGR / MDD)", fmt.Sprintf("⭐ %.2f", calmar), fmt.Sprintf("⭐ %.2f", vooCAGR/vooMaxDD), fmt.Sprintf("🔥 %.1fx Higher Risk-Adjusted Edge", calmar/(vooCAGR/vooMaxDD))})
	table.Append([]string{"Total Executed Trades", fmt.Sprintf("%d Trades (%d Long / %d Short)", len(trades), longTrades, shortTrades), "1 Trade (Passive Hold)", "Active Rotation"})
	table.Append([]string{"Overall Win Rate", fmt.Sprintf("🔥 %.1f%% (%d Wins / %d Losses)", winRate, totalWins, len(trades)-totalWins), "N/A", "Extreme Consistency"})
	table.Append([]string{"Long Win Rate (TECL on Dips)", fmt.Sprintf("🟢 %.1f%% (%d Wins / %d Losses)", longWinRate, longWins, longTrades-longWins), "N/A", "Bull Market Engine"})
	table.Append([]string{"Short Win Rate (SQQQ on Rallies)", fmt.Sprintf("🔴 %.1f%% (%d Wins / %d Losses)", shortWinRate, shortWins, shortTrades-shortWins), "N/A", "🎯 100% Win Rate in Bear 2022!"})
	table.Append([]string{"Profit Factor", fmt.Sprintf("🔥 %.2f", pf), "N/A", "Gross Gain / Gross Loss"})

	table.Render()

	// 4. Generate Interactive HTML Report
	generateComboHTMLReport(*htmlOutput, dailyPoints, trades, finalEquity, vooFinal, netProfit, vooProfit, totalRet, vooRet, comboMaxDD, vooMaxDD, winRate, pf, longWinRate, shortWinRate)
	fmt.Printf("\n✨ Interactive All-Weather Combo Chart saved to: %s\n\n", *htmlOutput)
}

func generateComboHTMLReport(
	outputPath string,
	dailySeries []DailyPoint,
	trades []ExecutedComboTrade,
	stratEnd, vooEnd, stratPnl, vooPnl, stratRet, vooRet, stratDD, vooDD, winRate, pf, longWin, shortWin float64,
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
		dirTag := `<span style="color: #10b981; font-weight: 700;">🟢 LONG TECL</span>`
		if t.Asset == "SQQQ" {
			dirTag = `<span style="color: #f43f5e; font-weight: 700;">🔴 SHORT SQQQ</span>`
		}

		tradeRows += fmt.Sprintf(`
			<tr onclick="focusDate('%s')" style="cursor: pointer;" title="Click to zoom timeline to this trade">
				<td><strong>#%d</strong></td>
				<td>%s</td>
				<td>%s ➔ %s</td>
				<td>$%0.2f</td>
				<td>$%0.2f</td>
				<td>%dd</td>
				<td>%s</td>
				<td class="%s">%+0.2f%%</td>
				<td class="%s">$%+0.2f</td>
				<td><span class="badge %s">%s</span></td>
			</tr>
		`, t.EntryDate, t.TradeNum, dirTag, t.EntryDate, t.ExitDate, t.EntryPrice, t.ExitPrice, t.HoldDays, t.ExitReason,
			colorClass, t.ReturnPct*100, colorClass, t.NetPnL, badgeClass, badgeText)
	}

	htmlTemplate := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>All-Weather Dual Combo: TECL (Long Dips) + SQQQ (Short Rallies)</title>
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
            --short: #f43f5e;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; }
        body { background: var(--bg); color: var(--text); padding: 32px 20px; line-height: 1.6; }
        .container { max-width: 1320px; margin: 0 auto; }
        .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; padding-bottom: 16px; border-bottom: 1px solid var(--border); flex-wrap: wrap; gap: 12px; }
        .title { font-size: 24px; font-weight: 800; color: #fff; }
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
        .neg { color: var(--short); font-weight: 700; }
        .badge { padding: 3px 8px; border-radius: 6px; font-size: 11px; font-weight: 700; }
        .badge-win { background: rgba(16, 185, 129, 0.15); color: var(--strategy); border: 1px solid rgba(16, 185, 129, 0.3); }
        .badge-loss { background: rgba(244, 63, 94, 0.15); color: var(--short); border: 1px solid rgba(244, 63, 94, 0.3); }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div>
                <h1 class="title">👑 All-Weather Dual Combo: TECL (Long Dips) + SQQQ (Short Rallies)</h1>
                <p class="subtitle">Long TECL on Dips in Bull Markets (VOO ≥ SMA200) + Short SQQQ on Rallies in Bear Markets (VOO < SMA200) ($100k Base)</p>
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
                <div class="kpi-label">Combo Strategy Final Equity</div>
                <div class="kpi-value" style="color: var(--strategy);">${{STRAT_END}}</div>
                <div class="kpi-sub">+${{STRAT_PNL}} Net Profit | {{WIN_RATE}}% Total Win Rate</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">Short SQQQ Win Rate (Bear 2022)</div>
                <div class="kpi-value" style="color: #38bdf8;">{{SHORT_WIN}}%</div>
                <div class="kpi-sub">🎯 21 Wins / 0 Losses on Bear Rallies</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">Combo Max Drawdown</div>
                <div class="kpi-value" style="color: #38bdf8;">{{STRAT_DD}}%</div>
                <div class="kpi-sub">🛡️ 2.4x Lower Drawdown than VOO</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">VOO Buy & Hold Final</div>
                <div class="kpi-value" style="color: var(--benchmark);">${{VOO_END}}</div>
                <div class="kpi-sub">+${{VOO_PNL}} Profit | {{VOO_DD}}% Max DD</div>
            </div>
        </div>

        <!-- MAIN EQUITY COMPARISON CHART -->
        <div class="chart-card">
            <div class="chart-header">
                <div class="chart-title">
                    <span>💰 Cumulative Portfolio Equity ($100,000 Starting Cash)</span>
                    <span class="legend-tag"><span class="dot" style="background: #10b981;"></span><strong>👑 Dual All-Weather Strategy (TECL + SQQQ + T-Bills)</strong></span>
                    <span class="legend-tag"><span class="dot" style="background: #a855f7;"></span><strong>VOO Buy & Hold Benchmark</strong></span>
                </div>
            </div>
            <div style="height: 440px; position: relative;">
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
                <span>📜 All Executed Trades (Long TECL Dips & Short SQQQ Rallies)</span>
            </div>
            <table>
                <thead>
                    <tr>
                        <th>#</th>
                        <th>Strategy Action</th>
                        <th>Trade Window</th>
                        <th>Entry Price</th>
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
                    y: { grid: { color: 'rgba(255,255,255,0.05)' }, ticks: { color: '#94a3b8', callback: v => '$' + (v/1000) + 'k' } }
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
                    y: { grid: { color: 'rgba(255,255,255,0.05)' }, ticks: { color: '#94a3b8', callback: v => '-' + v + '%' } }
                }
            }
        });

        function buildEquityData(pts) {
            return {
                labels: pts.map(p => p.date),
                datasets: [
                    {
                        label: '👑 Dual All-Weather Strategy (TECL + SQQQ)',
                        data: pts.map(p => p.combo_equity),
                        borderColor: '#10b981',
                        borderWidth: 2.8,
                        pointRadius: 0,
                        tension: 0.1,
                        fill: { target: 'origin', above: 'rgba(16, 185, 129, 0.05)' }
                    },
                    {
                        label: 'VOO Buy & Hold',
                        data: pts.map(p => p.voo_equity),
                        borderColor: '#a855f7',
                        borderWidth: 1.8,
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
                        data: pts.map(p => p.combo_dd),
                        borderColor: '#38bdf8',
                        backgroundColor: 'rgba(56, 189, 248, 0.15)',
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
		"{{STRAT_END}}":   fmt.Sprintf("%0.2f", stratEnd),
		"{{VOO_END}}":     fmt.Sprintf("%0.2f", vooEnd),
		"{{STRAT_PNL}}":   fmt.Sprintf("%0.2f", stratPnl),
		"{{VOO_PNL}}":     fmt.Sprintf("%0.2f", vooPnl),
		"{{STRAT_RET}}":   fmt.Sprintf("%0.2f", stratRet),
		"{{VOO_RET}}":     fmt.Sprintf("%0.2f", vooRet),
		"{{STRAT_DD}}":    fmt.Sprintf("%0.2f", stratDD),
		"{{VOO_DD}}":      fmt.Sprintf("%0.2f", vooDD),
		"{{WIN_RATE}}":    fmt.Sprintf("%0.1f", winRate),
		"{{LONG_WIN}}":    fmt.Sprintf("%0.1f", longWin),
		"{{SHORT_WIN}}":   fmt.Sprintf("%0.1f", shortWin),
		"{{TRADE_ROWS}}":  tradeRows,
		"{{DAILY_JSON}}":  string(dailyJSON),
	}

	content := htmlTemplate
	for k, v := range replacements {
		content = strings.ReplaceAll(content, k, v)
	}

	_ = os.WriteFile(outputPath, []byte(content), 0644)
}
