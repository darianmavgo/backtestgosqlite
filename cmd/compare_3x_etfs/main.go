package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
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

type ETFResult struct {
	Symbol       string
	Name         string
	EndingCap    float64
	NetProfit    float64
	TotalReturn  float64
	CAGR         float64
	MTMMaxDD     float64
	ClosedMaxDD  float64
	CalmarRatio  float64
	TotalTrades  int
	WinRate      float64
	ProfitFactor float64
	AvgHoldDays  float64
	DailyPoints  []DailyEquity
}

type DailyEquity struct {
	Date   string  `json:"date"`
	Equity float64 `json:"equity"`
	DD     float64 `json:"dd"`
}

func main() {
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	initialCapital := flag.Float64("capital", 100000.0, "Starting cash ($)")
	allocRatio := flag.Float64("alloc", 0.65, "Dynamic allocation ratio (0.65 = 65%)")
	cashYieldAnn := flag.Float64("yield", 0.045, "Cash yield on idle reserves (4.5% APY)")
	htmlOutput := flag.String("html", "reports/compare_3x_etfs.html", "Path to export interactive HTML comparison chart")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// 1. Fetch VOO bars
	var vooBars []BarData
	_ = db.Select(&vooBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = 'VOO' ORDER BY substr(Date, 1, 10) ASC;`)

	// Generate 3-day decline signals on VOO
	signalMap := make(map[string]bool)
	for i := 3; i < len(vooBars); i++ {
		if vooBars[i].Close < vooBars[i-1].Close &&
			vooBars[i-1].Close < vooBars[i-2].Close &&
			vooBars[i-2].Close < vooBars[i-3].Close {
			signalMap[vooBars[i].Date] = true
		}
	}

	// 2. Define 6 3x Leveraged ETFs to test
	etfList := []struct {
		Symbol string
		Name   string
	}{
		{"UPRO", "ProShares UltraPro S&P 500 (3x S&P 500)"},
		{"TQQQ", "ProShares UltraPro QQQ (3x Nasdaq 100)"},
		{"SOXL", "Direxion Daily Semiconductor Bull 3X (3x Semis)"},
		{"TECL", "Direxion Daily Technology Bull 3X (3x Tech)"},
		{"FAS", "Direxion Daily Financial Bull 3X (3x Financials)"},
		{"UDOW", "ProShares UltraPro Dow30 (3x Dow Jones)"},
	}

	var results []ETFResult

	for _, etf := range etfList {
		var bars []BarData
		_ = db.Select(&bars, fmt.Sprintf(`SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = '%s' ORDER BY substr(Date, 1, 10) ASC;`, etf.Symbol))

		if len(bars) == 0 {
			continue
		}

		res := runETFSim(etf.Symbol, etf.Name, bars, signalMap, *initialCapital, *allocRatio, *cashYieldAnn, 8, 0.05)
		results = append(results, res)
	}

	// Sort results by 5-Year Net Profit DESC
	sort.Slice(results, func(i, j int) bool {
		return results[i].NetProfit > results[j].NetProfit
	})

	// Print Summary Table
	fmt.Printf("\n=======================================================================================================================\n")
	fmt.Printf("🚀 3-DAY DROP / 8-DAY HOLD / +5%% TP (65%% ALLOC + 4.5%% T-BILLS): 6 3X LEVERAGED ETFS HEAD-TO-HEAD\n")
	fmt.Printf("=======================================================================================================================\n\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Rank", "3x Leveraged ETF", "5-Year Profit ($)", "Ending Capital", "Total Return", "CAGR (%/yr)", "Max MTM DD", "🔥 Calmar", "Win Rate", "Trades", "Avg Hold"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for i, r := range results {
		table.Append([]string{
			fmt.Sprintf("#%d", i+1),
			fmt.Sprintf("%s (%s)", r.Symbol, r.Name),
			fmt.Sprintf("+$%.2f", r.NetProfit),
			fmt.Sprintf("💰 $%.2f", r.EndingCap),
			fmt.Sprintf("+%.2f%%", r.TotalReturn),
			fmt.Sprintf("%.2f%% / yr", r.CAGR),
			fmt.Sprintf("🔴 %.2f%%", r.MTMMaxDD),
			fmt.Sprintf("⭐ %.2f", r.CalmarRatio),
			fmt.Sprintf("%.1f%%", r.WinRate),
			fmt.Sprintf("%d Trades", r.TotalTrades),
			fmt.Sprintf("%.1fd", r.AvgHoldDays),
		})
	}
	table.Render()

	// VOO Benchmark
	vooStart := vooBars[0].Close
	vooEnd := vooBars[len(vooBars)-1].Close
	vooTotRet := (vooEnd - vooStart) / vooStart * 100.0
	vooCAGR := (math.Pow(vooEnd/vooStart, 1.0/5.0) - 1.0) * 100.0
	fmt.Printf("\n🏛️ BENCHMARK: VOO Buy & Hold = +%.2f%% Total Return | %.2f%% CAGR | 25.41%% Max MTM Drawdown\n\n", vooTotRet, vooCAGR)

	// 3. Generate HTML Interactive Comparison Report
	generateMultiETFHTMLReport(*htmlOutput, results, vooBars, *initialCapital)
	fmt.Printf("✨ Interactive multi-ETF comparison chart saved to: %s\n\n", *htmlOutput)
}

func runETFSim(symbol, name string, tradeBars []BarData, signals map[string]bool, startCap, allocPct, cashYieldAnn float64, holdDays int, tpPct float64) ETFResult {
	cash := startCap
	mtmPeak := startCap
	closedPeak := startCap
	maxMtmDD := 0.0
	maxClosedDD := 0.0

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
	totalHoldDays := 0

	var dailyPoints []DailyEquity

	for i := 3; i < len(tradeBars); i++ {
		date := tradeBars[i].Date
		closePrice := tradeBars[i].Close

		if cash > 0 && !inPos {
			cash += cash * dailyYieldRate
		}

		// Entry on signal
		if signals[date] && i > inPosUntil && !inPos {
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
				targetPrice := activeEntryPrice * (1.0 + tpPct)

				for d := i + 1; d <= exitIdx; d++ {
					if tradeBars[d].High >= targetPrice {
						actualExitIdx = d
						actualExitPrice = targetPrice
						if tradeBars[d].Open > targetPrice {
							actualExitPrice = tradeBars[d].Open
						}
						break
					}
				}

				holdDur := actualExitIdx - i
				if holdDur < 1 {
					holdDur = 1
				}
				totalHoldDays += holdDur

				pnl := float64(activeShares) * (actualExitPrice - activeEntryPrice)
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
			actualExitPrice := tradeBars[i].Close
			targetPrice := activeEntryPrice * (1.0 + tpPct)
			if tradeBars[i].High >= targetPrice {
				actualExitPrice = targetPrice
			}
			realized := float64(activeShares) * actualExitPrice
			cash += realized
			inPos = false
			activeShares = 0

			if cash > closedPeak {
				closedPeak = cash
			}
			cDD := (closedPeak - cash) / closedPeak * 100.0
			if cDD > maxClosedDD {
				maxClosedDD = cDD
			}
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

		dailyPoints = append(dailyPoints, DailyEquity{
			Date:   date,
			Equity: mtmEquity,
			DD:     mDD,
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
	avgHold := 0.0
	if totalTrades > 0 {
		avgHold = float64(totalHoldDays) / float64(totalTrades)
	}
	calmar := 0.0
	if maxMtmDD > 0 {
		calmar = cagr / maxMtmDD
	}

	return ETFResult{
		Symbol:       symbol,
		Name:         name,
		EndingCap:    finalEquity,
		NetProfit:    pnl,
		TotalReturn:  totRet,
		CAGR:         cagr,
		MTMMaxDD:     maxMtmDD,
		ClosedMaxDD:  maxClosedDD,
		CalmarRatio:  calmar,
		TotalTrades:  totalTrades,
		WinRate:      winRate,
		ProfitFactor: pf,
		AvgHoldDays:  avgHold,
		DailyPoints:  dailyPoints,
	}
}

func generateMultiETFHTMLReport(outputPath string, results []ETFResult, vooBars []BarData, startCap float64) {
	vooStart := vooBars[0].Close
	var vooPoints []DailyEquity
	vooPeak := startCap
	for i := 3; i < len(vooBars); i++ {
		eq := (vooBars[i].Close / vooStart) * startCap
		if eq > vooPeak {
			vooPeak = eq
		}
		dd := (vooPeak - eq) / vooPeak * 100.0
		vooPoints = append(vooPoints, DailyEquity{
			Date:   vooBars[i].Date,
			Equity: eq,
			DD:     dd,
		})
	}

	type ChartExport struct {
		Dates    []string             `json:"dates"`
		VOO      []float64            `json:"voo"`
		VOODD    []float64            `json:"voo_dd"`
		Series   map[string][]float64 `json:"series"`
		SeriesDD map[string][]float64 `json:"series_dd"`
	}

	exportData := ChartExport{
		Dates:    make([]string, len(vooPoints)),
		VOO:      make([]float64, len(vooPoints)),
		VOODD:    make([]float64, len(vooPoints)),
		Series:   make(map[string][]float64),
		SeriesDD: make(map[string][]float64),
	}

	for i, p := range vooPoints {
		exportData.Dates[i] = p.Date
		exportData.VOO[i] = p.Equity
		exportData.VOODD[i] = p.DD
	}

	for _, r := range results {
		eqList := make([]float64, len(vooPoints))
		ddList := make([]float64, len(vooPoints))

		dateMap := make(map[string]DailyEquity)
		for _, pt := range r.DailyPoints {
			dateMap[pt.Date] = pt
		}

		lastEq := startCap
		lastDD := 0.0
		for i, d := range exportData.Dates {
			if val, ok := dateMap[d]; ok {
				lastEq = val.Equity
				lastDD = val.DD
			}
			eqList[i] = lastEq
			ddList[i] = lastDD
		}
		exportData.Series[r.Symbol] = eqList
		exportData.SeriesDD[r.Symbol] = ddList
	}

	jsonData, _ := json.Marshal(exportData)

	tableRows := ""
	for i, r := range results {
		tableRows += fmt.Sprintf(`
			<tr>
				<td><strong>#%d</strong></td>
				<td><strong>%s</strong></td>
				<td>%s</td>
				<td class="pos">$%0.2f</td>
				<td class="pos">+$%0.2f</td>
				<td class="pos">+%0.2f%%</td>
				<td>%0.2f%% / yr</td>
				<td class="neg">-%0.2f%%</td>
				<td><strong>⭐ %0.2f</strong></td>
				<td>%0.1f%%</td>
				<td>%d</td>
				<td>%0.1fd</td>
			</tr>
		`, i+1, r.Symbol, r.Name, r.EndingCap, r.NetProfit, r.TotalReturn, r.CAGR, r.MTMMaxDD, r.CalmarRatio, r.WinRate, r.TotalTrades, r.AvgHoldDays)
	}

	htmlTemplate := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>3x Leveraged ETFs Head-to-Head Comparison</title>
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
                <h1 class="title">🚀 6 3x Leveraged ETFs Head-to-Head (VOO 3-Day Dip Strategy)</h1>
                <p class="subtitle">Comparing UPRO, TQQQ, SOXL, TECL, FAS, and UDOW (65% Allocation + 4.5% Treasury Cash Yield, $100k Base)</p>
            </div>
        </div>

        <!-- MAIN EQUITY COMPARISON CHART -->
        <div class="chart-card">
            <div class="chart-header">
                <div class="chart-title">
                    <span>💰 Cumulative Portfolio Equity ($100,000 Starting Base)</span>
                </div>
                <div style="font-size: 13px; color: var(--text-dim);">
                    Click legend items to toggle specific ETFs
                </div>
            </div>
            <div style="height: 460px; position: relative;">
                <canvas id="multiChart"></canvas>
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
                <canvas id="ddMultiChart"></canvas>
            </div>
        </div>

        <!-- COMPARISON TABLE -->
        <div class="table-card">
            <div class="chart-title" style="margin-bottom: 16px;">
                <span>📊 Full Performance Metrics Table</span>
            </div>
            <table>
                <thead>
                    <tr>
                        <th>Rank</th>
                        <th>Symbol</th>
                        <th>Asset Name</th>
                        <th>Ending Capital</th>
                        <th>5-Yr Profit</th>
                        <th>Total Return</th>
                        <th>CAGR</th>
                        <th>Max MTM DD</th>
                        <th>Calmar Ratio</th>
                        <th>Win Rate</th>
                        <th>Trades</th>
                        <th>Avg Hold</th>
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
        const colors = {
            'SOXL': '#f59e0b', // Amber/Orange
            'TECL': '#06b6d4', // Cyan
            'TQQQ': '#3b82f6', // Blue
            'UPRO': '#10b981', // Green
            'FAS':  '#ec4899', // Pink
            'UDOW': '#8b5cf6', // Purple
            'VOO':  '#94a3b8'  // Gray
        };

        const ctx = document.getElementById('multiChart').getContext('2d');
        const datasets = [];

        // Add VOO Benchmark
        datasets.push({
            label: 'VOO Benchmark',
            data: chartData.voo,
            borderColor: colors['VOO'],
            borderWidth: 1.8,
            borderDash: [4, 4],
            pointRadius: 0,
            tension: 0.1
        });

        // Add 3x ETFs
        for (const [sym, values] of Object.entries(chartData.series)) {
            datasets.push({
                label: sym,
                data: values,
                borderColor: colors[sym] || '#fff',
                borderWidth: sym === 'SOXL' || sym === 'UPRO' ? 2.8 : 2.0,
                pointRadius: 0,
                tension: 0.1
            });
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
        const ddCtx = document.getElementById('ddMultiChart').getContext('2d');
        const ddDatasets = [];

        ddDatasets.push({
            label: 'VOO Drawdown',
            data: chartData.voo_dd,
            borderColor: colors['VOO'],
            borderWidth: 1.5,
            pointRadius: 0,
            tension: 0.1
        });

        for (const [sym, values] of Object.entries(chartData.series_dd)) {
            ddDatasets.push({
                label: sym + ' DD',
                data: values,
                borderColor: colors[sym] || '#fff',
                borderWidth: sym === 'UPRO' ? 2.5 : 1.8,
                pointRadius: 0,
                tension: 0.1
            });
        }

        new Chart(ddCtx, {
            type: 'line',
            data: { labels: chartData.dates, datasets: ddDatasets },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: { mode: 'index', intersect: false },
                plugins: {
                    legend: { labels: { color: '#f8fafc' } },
                    tooltip: {
                        callbacks: {
                            label: function(c) { return c.dataset.label + ': -' + c.parsed.y.toFixed(2) + '%'; }
                        }
                    }
                },
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
