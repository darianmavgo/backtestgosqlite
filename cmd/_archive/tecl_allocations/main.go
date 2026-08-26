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

type AllocResult struct {
	AllocPct     float64
	Name         string
	EndingCap    float64
	NetProfit    float64
	TotalReturn  float64
	CAGR         float64
	MTMMaxDD     float64
	ClosedMaxDD  float64
	CalmarRatio  float64
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
	cashYieldAnn := flag.Float64("yield", 0.045, "Annual cash yield on idle reserves (4.5% APY)")
	htmlOutput := flag.String("html", "reports/tecl_allocations.html", "Path to export interactive HTML comparison chart")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// 1. Fetch VOO bars for signals
	var vooBars []BarData
	_ = db.Select(&vooBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = 'VOO' ORDER BY substr(Date, 1, 10) ASC;`)

	signalMap := make(map[string]bool)
	for i := 3; i < len(vooBars); i++ {
		if vooBars[i].Close < vooBars[i-1].Close &&
			vooBars[i-1].Close < vooBars[i-2].Close &&
			vooBars[i-2].Close < vooBars[i-3].Close {
			signalMap[vooBars[i].Date] = true
		}
	}

	// 2. Fetch TECL bars
	var teclBars []BarData
	_ = db.Select(&teclBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = 'TECL' ORDER BY substr(Date, 1, 10) ASC;`)

	allocations := []float64{0.30, 0.40, 0.50, 0.60, 0.65, 0.75, 0.80, 0.85, 0.90, 1.00}
	var results []AllocResult

	for _, a := range allocations {
		res := runTECLSim(teclBars, signalMap, *initialCapital, a, *cashYieldAnn, 8, 0.05)
		results = append(results, res)
	}

	// Print Table
	fmt.Printf("\n=======================================================================================================================\n")
	fmt.Printf("🚀 TECL (3X TECHNOLOGY) ACROSS ALLOCATIONS: VOO 3-DAY DROP / 8-DAY HOLD / +5%% TP (+4.5%% T-BILLS)\n")
	fmt.Printf("=======================================================================================================================\n\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Allocation %", "5-Year Profit ($)", "Ending Capital", "Total Return", "CAGR (%/yr)", "MTM Max DD", "Closed DD", "🔥 Calmar", "DD <= 11% Status"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, r := range results {
		status := "✅ SAFE (< 11%)"
		if r.MTMMaxDD > 11.0 {
			status = "⚠️ Over 11%"
		}
		if r.MTMMaxDD <= 10.0 {
			status = "🛡️ ULTRA SAFE (< 10%)"
		}

		table.Append([]string{
			fmt.Sprintf("%.0f%% Dynamic", r.AllocPct*100),
			fmt.Sprintf("+$%.2f", r.NetProfit),
			fmt.Sprintf("💰 $%.2f", r.EndingCap),
			fmt.Sprintf("+%.2f%%", r.TotalReturn),
			fmt.Sprintf("%.2f%% / yr", r.CAGR),
			fmt.Sprintf("🔴 %.2f%%", r.MTMMaxDD),
			fmt.Sprintf("%.2f%%", r.ClosedMaxDD),
			fmt.Sprintf("⭐ %.2f", r.CalmarRatio),
			status,
		})
	}
	table.Render()

	// Benchmark
	vooStart := vooBars[0].Close
	vooEnd := vooBars[len(vooBars)-1].Close
	vooTotRet := (vooEnd - vooStart) / vooStart * 100.0
	vooCAGR := (math.Pow(vooEnd/vooStart, 1.0/5.0) - 1.0) * 100.0
	fmt.Printf("\n🏛️ BENCHMARK: VOO Buy & Hold = +%.2f%% Total Return | %.2f%% CAGR | 25.41%% Max MTM Drawdown\n\n", vooTotRet, vooCAGR)

	// Generate HTML Report
	generateTECLHTMLReport(*htmlOutput, results, vooBars, *initialCapital)
	fmt.Printf("✨ Interactive TECL allocation comparison chart saved to: %s\n\n", *htmlOutput)
}

func runTECLSim(tradeBars []BarData, signals map[string]bool, startCap, allocPct, cashYieldAnn float64, holdDays int, tpPct float64) AllocResult {
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

	var dailyPoints []DailyEquity

	for i := 3; i < len(tradeBars); i++ {
		date := tradeBars[i].Date
		closePrice := tradeBars[i].Close

		if cash > 0 && !inPos {
			cash += cash * dailyYieldRate
		}

		// Entry
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
				targetPrice := activeEntryPrice * (1.0 + tpPct)

				for d := i + 1; d <= exitIdx; d++ {
					if tradeBars[d].High >= targetPrice {
						actualExitIdx = d
						break
					}
				}
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
	calmar := 0.0
	if maxMtmDD > 0 {
		calmar = cagr / maxMtmDD
	}

	return AllocResult{
		AllocPct:    allocPct,
		Name:        fmt.Sprintf("%.0f%% Allocation", allocPct*100),
		EndingCap:   finalEquity,
		NetProfit:   pnl,
		TotalReturn: totRet,
		CAGR:        cagr,
		MTMMaxDD:    maxMtmDD,
		ClosedMaxDD: maxClosedDD,
		CalmarRatio: calmar,
		DailyPoints: dailyPoints,
	}
}

func generateTECLHTMLReport(outputPath string, results []AllocResult, vooBars []BarData, startCap float64) {
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
		exportData.Series[r.Name] = eqList
		exportData.SeriesDD[r.Name] = ddList
	}

	jsonData, _ := json.Marshal(exportData)

	tableRows := ""
	for _, r := range results {
		status := "✅ SAFE (< 11%)"
		if r.MTMMaxDD > 11.0 {
			status = "⚠️ Over 11%"
		}
		if r.MTMMaxDD <= 10.0 {
			status = "🛡️ ULTRA SAFE (< 10%)"
		}

		tableRows += fmt.Sprintf(`
			<tr>
				<td><strong>%.0f%% Allocation</strong></td>
				<td class="pos">$%0.2f</td>
				<td class="pos">+$%0.2f</td>
				<td class="pos">+%0.2f%%</td>
				<td>%0.2f%% / yr</td>
				<td class="neg">-%0.2f%%</td>
				<td>-%0.2f%%</td>
				<td><strong>⭐ %0.2f</strong></td>
				<td>%s</td>
			</tr>
		`, r.AllocPct*100, r.EndingCap, r.NetProfit, r.TotalReturn, r.CAGR, r.MTMMaxDD, r.ClosedMaxDD, r.CalmarRatio, status)
	}

	htmlTemplate := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>TECL (3x Tech) Across Allocations</title>
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
                <h1 class="title">🚀 TECL (3x Technology) Allocation Matrix</h1>
                <p class="subtitle">VOO 3-Day Drop / 8-Day Hold / +5% TP with 4.5% Treasury Cash Yield on Idle Reserves ($100k Base)</p>
            </div>
        </div>

        <!-- MAIN EQUITY COMPARISON CHART -->
        <div class="chart-card">
            <div class="chart-header">
                <div class="chart-title">
                    <span>💰 Cumulative Portfolio Growth Across Sizing Tiers ($100,000 Starting Cash)</span>
                </div>
                <div style="font-size: 13px; color: var(--text-dim);">
                    Click legend items to toggle specific allocations
                </div>
            </div>
            <div style="height: 460px; position: relative;">
                <canvas id="teclChart"></canvas>
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
                <canvas id="ddTeclChart"></canvas>
            </div>
        </div>

        <!-- COMPARISON TABLE -->
        <div class="table-card">
            <div class="chart-title" style="margin-bottom: 16px;">
                <span>📊 Full Allocation Performance Matrix</span>
            </div>
            <table>
                <thead>
                    <tr>
                        <th>Allocation</th>
                        <th>Ending Capital</th>
                        <th>5-Yr Profit</th>
                        <th>Total Return</th>
                        <th>CAGR</th>
                        <th>Max MTM DD</th>
                        <th>Closed DD</th>
                        <th>Calmar Ratio</th>
                        <th>Drawdown Status</th>
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
        const palette = [
            '#38bdf8', '#0ea5e9', '#0284c7', // Blues (30-50%)
            '#10b981', '#059669',            // Greens (60-65%)
            '#f59e0b', '#d97706',            // Ambers (75-80%)
            '#f43f5e', '#e11d48', '#be123c'  // Reds/Pinks (85-100%)
        ];

        const ctx = document.getElementById('teclChart').getContext('2d');
        const datasets = [];

        // VOO Benchmark
        datasets.push({
            label: 'VOO Benchmark',
            data: chartData.voo,
            borderColor: '#94a3b8',
            borderWidth: 1.8,
            borderDash: [4, 4],
            pointRadius: 0,
            tension: 0.1
        });

        let idx = 0;
        for (const [name, values] of Object.entries(chartData.series)) {
            datasets.push({
                label: name,
                data: values,
                borderColor: palette[idx % palette.length],
                borderWidth: name.includes('50%') || name.includes('65%') || name.includes('100%') ? 2.8 : 1.8,
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
        const ddCtx = document.getElementById('ddTeclChart').getContext('2d');
        const ddDatasets = [];

        ddDatasets.push({
            label: 'VOO Drawdown',
            data: chartData.voo_dd,
            borderColor: '#94a3b8',
            borderWidth: 1.5,
            pointRadius: 0,
            tension: 0.1
        });

        idx = 0;
        for (const [name, values] of Object.entries(chartData.series_dd)) {
            ddDatasets.push({
                label: name + ' DD',
                data: values,
                borderColor: palette[idx % palette.length],
                borderWidth: name.includes('50%') || name.includes('65%') ? 2.5 : 1.5,
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
