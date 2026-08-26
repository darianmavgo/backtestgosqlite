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

type InverseTrade struct {
	TradeNum   int
	SignalDate string
	EntryDate  string
	ExitDate   string
	EntryPrice float64
	ExitPrice  float64
	HoldDays   int
	ExitReason string
	ReturnPct  float64
	NetPnL     float64
	IsWin      bool
}

type InverseSimResult struct {
	Symbol       string
	Name         string
	Filter       string
	StartingCap  float64
	EndingCap    float64
	NetProfit    float64
	TotalReturn  float64
	CAGR         float64
	MTMMaxDD     float64
	CalmarRatio  float64
	TotalTrades  int
	WinRate      float64
	ProfitFactor float64
	Trades       []InverseTrade
	DailySeries  []DailyPoint
}

type DailyPoint struct {
	Date      string  `json:"date"`
	Equity    float64 `json:"equity"`
	Drawdown  float64 `json:"drawdown"`
	VooEquity float64 `json:"voo_equity"`
	VooDD     float64 `json:"voo_dd"`
}

func main() {
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	initialCapital := flag.Float64("capital", 100000.0, "Starting cash ($)")
	allocRatio := flag.Float64("alloc", 0.65, "Dynamic allocation ratio (0.65 = 65%)")
	cashYieldAnn := flag.Float64("yield", 0.045, "Annual cash yield on idle reserves (4.5% APY)")
	htmlOutput := flag.String("html", "reports/inverse_3x_study.html", "Path to export interactive HTML comparison chart")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// 1. Fetch VOO bars
	var vooBars []BarData
	_ = db.Select(&vooBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = 'VOO' ORDER BY substr(Date, 1, 10) ASC;`)

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

	// 2. Frequency & Streak Distribution Analysis on VOO UP Streaks
	fmt.Printf("\n=====================================================================================================\n")
	fmt.Printf("📊 PART 1: FREQUENCY OF VOO CONSECUTIVE UP DAYS (5-YEAR TIMELINE: 2021-2026, 1273 BARS)\n")
	fmt.Printf("=====================================================================================================\n\n")

	streakCounts := make(map[int]int)
	currentUpStreak := 0
	total3DayUpSignals := 0
	reversalDownNextDay := 0

	for i := 1; i < len(vooBars); i++ {
		if vooBars[i].Close > vooBars[i-1].Close {
			currentUpStreak++
		} else {
			if currentUpStreak > 0 {
				streakCounts[currentUpStreak]++
			}
			currentUpStreak = 0
		}

		// Check if 3 consecutive up days
		if i >= 3 && vooBars[i].Close > vooBars[i-1].Close &&
			vooBars[i-1].Close > vooBars[i-2].Close &&
			vooBars[i-2].Close > vooBars[i-3].Close {
			total3DayUpSignals++
			if i+1 < len(vooBars) {
				if vooBars[i+1].Close < vooBars[i].Close {
					reversalDownNextDay++
				}
			}
		}
	}
	if currentUpStreak > 0 {
		streakCounts[currentUpStreak]++
	}

	streakTable := tablewriter.NewWriter(os.Stdout)
	streakTable.SetHeader([]string{"Consecutive UP Days", "Occurrences in 5 Years", "Cumulative Probability", "Next Day Direction"})
	streakTable.SetBorder(true)
	streakTable.SetAutoWrapText(false)

	cumUpCount := 0
	for s := 8; s >= 1; s-- {
		cumUpCount += streakCounts[s]
	}

	runningCum := 0
	for s := 1; s <= 8; s++ {
		c := streakCounts[s]
		runningCum += c
		nextDayReversalPct := ""
		if s == 3 {
			nextDayReversalPct = fmt.Sprintf("🔻 %.1f%% drop next day", float64(reversalDownNextDay)/float64(total3DayUpSignals)*100.0)
		}
		streakTable.Append([]string{
			fmt.Sprintf("%d Days UP in a Row", s),
			fmt.Sprintf("%d times", c),
			fmt.Sprintf("%.1f%% of all rallies", float64(c)/float64(cumUpCount)*100.0),
			nextDayReversalPct,
		})
	}
	streakTable.Render()

	fmt.Printf("\n📌 TOTAL 3-DAY UP STREAK SIGNALS: %d occurrences\n", total3DayUpSignals)
	fmt.Printf("📌 NEXT-DAY REVERSAL PROBABILITY: %.1f%% of the time, Day 4 is a down day\n\n", float64(reversalDownNextDay)/float64(total3DayUpSignals)*100.0)

	// 3. Backtest 3-Day UP Streak ➔ Buy Inverse 3x ETFs (SPXU, SQQQ, SOXS)
	inverseETFs := []struct {
		Symbol string
		Name   string
	}{
		{"SPXU", "ProShares UltraPro Short S&P 500 (-3x S&P 500)"},
		{"SQQQ", "ProShares UltraPro Short QQQ (-3x Nasdaq 100)"},
		{"SOXS", "Direxion Daily Semiconductor Bear 3X (-3x Semis)"},
	}

	var results []InverseSimResult

	for _, etf := range inverseETFs {
		var bars []BarData
		_ = db.Select(&bars, fmt.Sprintf(`SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = '%s' ORDER BY substr(Date, 1, 10) ASC;`, etf.Symbol))

		// 1. All Market Regimes (No Filter)
		resAll := runInverseSim(etf.Symbol, etf.Name, "All Regimes", vooBars, bars, sigBars, false, *initialCapital, *allocRatio, *cashYieldAnn, 8, 0.05)
		results = append(results, resAll)

		// 2. Bear Market Only Filter (VOO < SMA200)
		resBear := runInverseSim(etf.Symbol, etf.Name, "Bear Regime (VOO < SMA200)", vooBars, bars, sigBars, true, *initialCapital, *allocRatio, *cashYieldAnn, 8, 0.05)
		results = append(results, resBear)
	}

	fmt.Printf("=======================================================================================================================\n")
	fmt.Printf("🚀 PART 2: INVERSE 3X ETF BACKTEST (VOO 3-DAY UP ➔ BUY INVERSE 3X ETF ➔ +5%% REVERSION TARGET / 8-DAY HOLD)\n")
	fmt.Printf("=======================================================================================================================\n\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Inverse 3x ETF", "Market Filter", "5-Year Profit ($)", "Ending Capital", "Total Return", "CAGR (%/yr)", "Max MTM DD", "🔥 Calmar", "Win Rate", "Trades"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, r := range results {
		table.Append([]string{
			r.Symbol,
			r.Filter,
			fmt.Sprintf("+$%.2f", r.NetProfit),
			fmt.Sprintf("💰 $%.2f", r.EndingCap),
			fmt.Sprintf("%+0.2f%%", r.TotalReturn),
			fmt.Sprintf("%.2f%% / yr", r.CAGR),
			fmt.Sprintf("🔴 %.2f%%", r.MTMMaxDD),
			fmt.Sprintf("⭐ %.2f", r.CalmarRatio),
			fmt.Sprintf("%.1f%%", r.WinRate),
			fmt.Sprintf("%d Trades", r.TotalTrades),
		})
	}
	table.Render()

	// 4. Generate Interactive Report
	generateInverseHTMLReport(*htmlOutput, results, vooBars, *initialCapital)
	fmt.Printf("\n✨ Interactive Inverse 3x Study saved to: %s\n\n", *htmlOutput)
}

func runInverseSim(symbol, name, filterName string, vooBars, tradeBars []BarData, sigBars []SignalBar, requireBearFilter bool, startCap, allocPct, cashYieldAnn float64, holdDays int, tpPct float64) InverseSimResult {
	cash := startCap
	mtmPeak := startCap
	maxMtmDD := 0.0

	dailyYieldRate := math.Pow(1.0+cashYieldAnn, 1.0/252.0) - 1.0

	inPosUntil := -1
	activeShares := 0
	activeEntryPrice := 0.0
	inPos := false

	wins := 0
	losses := 0
	grossGains := 0.0
	grossLosses := 0.0
	totalTrades := 0

	var trades []InverseTrade
	var dailyPoints []DailyPoint

	vooStart := vooBars[0].Close

	for i := 3; i < len(tradeBars) && i < len(vooBars); i++ {
		date := tradeBars[i].Date
		closePrice := tradeBars[i].Close
		vooClose := vooBars[i].Close

		if cash > 0 && !inPos {
			cash += cash * dailyYieldRate
		}

		// 3-Day UP Streak on VOO: Close[i] > Close[i-1] > Close[i-2] > Close[i-3]
		is3dUp := (vooBars[i].Close > vooBars[i-1].Close &&
			vooBars[i-1].Close > vooBars[i-2].Close &&
			vooBars[i-2].Close > vooBars[i-3].Close)

		if is3dUp && requireBearFilter && sigBars[i].Close >= sigBars[i].SMA200 {
			is3dUp = false
		}

		// Entry
		if is3dUp && i > inPosUntil && !inPos {
			posCap := cash * allocPct
			shares := int(posCap / closePrice)
			if shares > 0 {
				activeShares = shares
				activeEntryPrice = closePrice
				cash -= float64(activeShares) * activeEntryPrice
				inPos = true

				exitIdx := i + holdDays
				if exitIdx >= len(tradeBars) {
					exitIdx = len(tradeBars) - 1
				}

				actualExitIdx := exitIdx
				actualExitPrice := tradeBars[actualExitIdx].Close
				actualExitReason := "TIME_UP_8DAYS"
				targetPrice := activeEntryPrice * (1.0 + tpPct)

				for d := i + 1; d <= exitIdx; d++ {
					if tradeBars[d].High >= targetPrice {
						actualExitIdx = d
						actualExitPrice = targetPrice
						if tradeBars[d].Open > targetPrice {
							actualExitPrice = tradeBars[d].Open
						}
						actualExitReason = "🎯 PROFIT_TARGET_+5%"
						break
					}
				}

				pnl := float64(activeShares) * (actualExitPrice - activeEntryPrice)
				ret := (actualExitPrice - activeEntryPrice) / activeEntryPrice

				trades = append(trades, InverseTrade{
					TradeNum:   len(trades) + 1,
					SignalDate: date,
					EntryDate:  date,
					ExitDate:   tradeBars[actualExitIdx].Date,
					EntryPrice: activeEntryPrice,
					ExitPrice:  actualExitPrice,
					HoldDays:   actualExitIdx - i,
					ExitReason: actualExitReason,
					ReturnPct:  ret,
					NetPnL:     pnl,
					IsWin:      pnl > 0,
				})

				if pnl > 0 {
					wins++
					grossGains += pnl
				} else {
					losses++
					grossLosses += math.Abs(pnl)
				}
				totalTrades++
				inPosUntil = actualExitIdx
			}
		}

		// Exit
		if inPos && i == inPosUntil {
			currTrade := trades[len(trades)-1]
			realized := float64(activeShares) * currTrade.ExitPrice
			cash += realized
			inPos = false
			activeShares = 0
		}

		posVal := 0.0
		if inPos {
			posVal = float64(activeShares) * closePrice
		}
		mtmEquity := cash + posVal
		if mtmEquity > mtmPeak {
			mtmPeak = mtmEquity
		}
		mDD := (mtmPeak - mtmEquity) / mtmPeak * 100.0
		if mDD > maxMtmDD {
			maxMtmDD = mDD
		}

		vooEquity := (vooClose / vooStart) * startCap
		dailyPoints = append(dailyPoints, DailyPoint{
			Date:      date,
			Equity:    mtmEquity,
			Drawdown:  mDD,
			VooEquity: vooEquity,
		})
	}

	posVal := 0.0
	if inPos {
		posVal = float64(activeShares) * tradeBars[len(tradeBars)-1].Close
	}
	finalEquity := cash + posVal
	pnl := finalEquity - startCap
	totRet := pnl / startCap * 100.0
	cagr := (math.Pow(finalEquity/startCap, 1.0/5.0) - 1.0) * 100.0

	winRate := 0.0
	if totalTrades > 0 {
		winRate = float64(wins) / float64(totalTrades) * 100.0
	}
	pf := 0.0
	if grossLosses > 0 {
		pf = grossGains / grossLosses
	}
	calmar := 0.0
	if maxMtmDD > 0 {
		calmar = cagr / maxMtmDD
	}

	return InverseSimResult{
		Symbol:       symbol,
		Name:         name,
		Filter:       filterName,
		StartingCap:  startCap,
		EndingCap:    finalEquity,
		NetProfit:    pnl,
		TotalReturn:  totRet,
		CAGR:         cagr,
		MTMMaxDD:     maxMtmDD,
		CalmarRatio:  calmar,
		TotalTrades:  totalTrades,
		WinRate:      winRate,
		ProfitFactor: pf,
		Trades:       trades,
		DailySeries:  dailyPoints,
	}
}

func generateInverseHTMLReport(outputPath string, results []InverseSimResult, vooBars []BarData, startCap float64) {
	type ExportStructure struct {
		Dates    []string             `json:"dates"`
		VOO      []float64            `json:"voo"`
		Series   map[string][]float64 `json:"series"`
		SeriesDD map[string][]float64 `json:"series_dd"`
	}

	dates := make([]string, len(results[0].DailySeries))
	voo := make([]float64, len(results[0].DailySeries))
	series := make(map[string][]float64)
	seriesDD := make(map[string][]float64)

	for i, pt := range results[0].DailySeries {
		dates[i] = pt.Date
		voo[i] = pt.VooEquity
	}

	for _, r := range results {
		key := fmt.Sprintf("%s (%s)", r.Symbol, r.Filter)
		eqList := make([]float64, len(dates))
		ddList := make([]float64, len(dates))
		for i, pt := range r.DailySeries {
			eqList[i] = pt.Equity
			ddList[i] = pt.Drawdown
		}
		series[key] = eqList
		seriesDD[key] = ddList
	}

	exportObj := ExportStructure{
		Dates:    dates,
		VOO:      voo,
		Series:   series,
		SeriesDD: seriesDD,
	}

	jsonData, _ := json.Marshal(exportObj)

	tableRows := ""
	for i, r := range results {
		posClass := "pos"
		if r.NetProfit < 0 {
			posClass = "neg"
		}
		tableRows += fmt.Sprintf(`
			<tr>
				<td><strong>#%d</strong></td>
				<td><strong>%s</strong></td>
				<td>%s</td>
				<td>%s</td>
				<td class="%s">$%0.2f</td>
				<td class="%s">+$%0.2f</td>
				<td class="%s">%+0.2f%%</td>
				<td>%0.2f%% / yr</td>
				<td class="neg">-%0.2f%%</td>
				<td>⭐ %0.2f</td>
				<td>%0.1f%%</td>
				<td>%d</td>
			</tr>
		`, i+1, r.Symbol, r.Name, r.Filter, posClass, r.EndingCap, posClass, r.NetProfit, posClass, r.TotalReturn, r.CAGR, r.MTMMaxDD, r.CalmarRatio, r.WinRate, r.TotalTrades)
	}

	htmlTemplate := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Inverse 3x Reversion Study (VOO 3-Day UP Streak)</title>
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
            --pos: #10b981;
            --neg: #f43f5e;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; }
        body { background: var(--bg); color: var(--text); padding: 32px 20px; line-height: 1.6; }
        .container { max-width: 1320px; margin: 0 auto; }
        .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; padding-bottom: 16px; border-bottom: 1px solid var(--border); flex-wrap: wrap; gap: 12px; }
        .title { font-size: 24px; font-weight: 800; color: #fff; }
        .subtitle { color: var(--text-dim); font-size: 14px; margin-top: 4px; }
        
        .chart-card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 24px; margin-bottom: 24px; }
        .chart-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 18px; flex-wrap: wrap; gap: 10px; }
        .chart-title { font-size: 17px; font-weight: 700; color: #fff; }
        
        .table-card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 24px; overflow-x: auto; margin-top: 24px; }
        table { width: 100%; border-collapse: collapse; text-align: left; font-size: 14px; }
        th { color: var(--text-dim); font-weight: 600; padding: 12px 14px; border-bottom: 1px solid var(--border); }
        td { padding: 12px 14px; border-bottom: 1px solid rgba(255,255,255,0.05); }
        tbody tr:hover { background: var(--card-hover); }
        .pos { color: var(--pos); font-weight: 700; }
        .neg { color: var(--neg); font-weight: 700; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div>
                <h1 class="title">📉 Inverse 3x Mean-Reversion Study: Shorting After 3 UP Days</h1>
                <p class="subtitle">Testing SPXU (-3x S&P 500), SQQQ (-3x Nasdaq), and SOXS (-3x Semis) on VOO 3-Day Up Signals ($100k Base)</p>
            </div>
        </div>

        <!-- MAIN EQUITY COMPARISON CHART -->
        <div class="chart-card">
            <div class="chart-header">
                <div class="chart-title">
                    <span>💰 Cumulative Portfolio Equity ($100,000 Starting Cash)</span>
                </div>
            </div>
            <div style="height: 460px; position: relative;">
                <canvas id="inverseChart"></canvas>
            </div>
        </div>

        <!-- DRAWDOWN UNDERWATER CHART -->
        <div class="chart-card">
            <div class="chart-header">
                <div class="chart-title">
                    <span>📉 Mark-to-Market Drawdown Comparison (%)</span>
                </div>
            </div>
            <div style="height: 240px; position: relative;">
                <canvas id="ddInverseChart"></canvas>
            </div>
        </div>

        <!-- TABLE -->
        <div class="table-card">
            <div class="chart-title" style="margin-bottom: 16px;">
                <span>📊 Full Performance Metrics Table (Shorting After 3-Day Rallies)</span>
            </div>
            <table>
                <thead>
                    <tr>
                        <th>#</th>
                        <th>Symbol</th>
                        <th>Asset Name</th>
                        <th>Regime Filter</th>
                        <th>Ending Capital</th>
                        <th>5-Yr Profit</th>
                        <th>Total Return</th>
                        <th>CAGR</th>
                        <th>Max MTM DD</th>
                        <th>Calmar</th>
                        <th>Win Rate</th>
                        <th>Trades</th>
                    </tr>
                </thead>
                <tbody>
                    {{TABLE_ROWS}}
                </tbody>
            </table>
        </div>
    </div>

    <script>
        const chartData = {{JSON_DATA}};
        const colors = [
            '#f43f5e', '#fb7185', // SPXU
            '#a855f7', '#c084fc', // SQQQ
            '#f59e0b', '#fbbf24', // SOXS
            '#10b981'             // VOO
        ];

        const ctx = document.getElementById('inverseChart').getContext('2d');
        const datasets = [];

        datasets.push({
            label: 'VOO Benchmark (Long)',
            data: chartData.voo,
            borderColor: '#10b981',
            borderWidth: 2,
            pointRadius: 0,
            tension: 0.1
        });

        let idx = 0;
        for (const [name, values] of Object.entries(chartData.series)) {
            datasets.push({
                label: name,
                data: values,
                borderColor: colors[idx % colors.length],
                borderWidth: 2,
                pointRadius: 0,
                tension: 0.1
            });
            idx++;
        }

        new Chart(ctx, {
            type: 'line',
            data: { labels: chartData.dates, datasets: datasets },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { mode: 'index', intersect: false },
                plugins: {
                    legend: { labels: { color: '#f8fafc', font: { weight: 'bold' } } },
                    tooltip: {
                        backgroundColor: '#111827',
                        borderColor: '#1e293b',
                        borderWidth: 1,
                        callbacks: {
                            label: function(c) {
                                return c.dataset.label + ': $' + c.parsed.y.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
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
        const ddCtx = document.getElementById('ddInverseChart').getContext('2d');
        const ddDatasets = [];
        idx = 0;
        for (const [name, values] of Object.entries(chartData.series_dd)) {
            ddDatasets.push({
                label: name + ' DD',
                data: values,
                borderColor: colors[idx % colors.length],
                borderWidth: 1.8,
                pointRadius: 0,
                tension: 0.1
            });
            idx++;
        }

        new Chart(ddCtx, {
            type: 'line',
            data: { labels: chartData.dates, datasets: ddDatasets },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { mode: 'index', intersect: false },
                plugins: { legend: { labels: { color: '#f8fafc' } } },
                scales: {
                    x: { grid: { color: 'rgba(255,255,255,0.03)' }, ticks: { color: '#94a3b8', maxTicksLimit: 12 } },
                    y: { grid: { color: 'rgba(255,255,255,0.05)' }, ticks: { color: '#94a3b8', callback: v => '-' + v + '%' } }
                }
            }
        });
    </script>
</body>
</html>`

	htmlTemplate = strings.ReplaceAll(htmlTemplate, "{{TABLE_ROWS}}", tableRows)
	htmlTemplate = strings.ReplaceAll(htmlTemplate, "{{JSON_DATA}}", string(jsonData))

	_ = os.WriteFile(outputPath, []byte(htmlTemplate), 0644)
}
