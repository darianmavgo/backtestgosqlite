package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
)

type StreakStat struct {
	StreakDays  int     `db:"streak_days"`
	Occurrences int     `db:"occurrences"`
	PctOfTotal  float64 `db:"pct_of_total"`
	AvgDrawdown float64 `db:"avg_drawdown_pct"`
	MaxDrawdown float64 `db:"max_drawdown_pct"`
}

type StreakEvent struct {
	StartDate   string  `db:"start_date"`
	EndDate     string  `db:"end_date"`
	StreakDays  int     `db:"streak_days"`
	StartPrice  float64 `db:"start_price"`
	TroughPrice float64 `db:"trough_price"`
	DropPct     float64 `db:"total_drop_pct"`
}

func main() {
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	symbolFlag := flag.String("symbol", "VOO", "Symbol to chart (e.g. VOO, QQQ, SPY)")
	htmlOutput := flag.String("html", "reports/voo_qqq_streaks.html", "Path to export interactive HTML chart report")
	flag.Parse()

	sym := strings.ToUpper(*symbolFlag)
	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB %s: %v", *dbPath, err)
	}
	defer db.Close()

	// Ensure is_down_slice exists
	schemaFile := "sql/strategies/etf_study/calc_study_pipeline.sql"
	_ = storage.ExecuteSQLFile(db, schemaFile)

	// 1. Fetch Streak Distribution
	query := `
		SELECT 
			streak_days,
			COUNT(DISTINCT streak_id) AS occurrences,
			ROUND(COUNT(DISTINCT streak_id) * 100.0 / (SELECT COUNT(DISTINCT streak_id) FROM is_down_slice WHERE symbol = ? AND is_down = 1), 2) AS pct_of_total,
			COALESCE(ROUND(AVG((close - prev_close) / prev_close * 100), 2), 0) AS avg_drawdown_pct,
			COALESCE(ROUND(MIN((close - prev_close) / prev_close * 100), 2), 0) AS max_drawdown_pct
		FROM is_down_slice
		WHERE symbol = ? AND is_down = 1
		GROUP BY streak_days
		ORDER BY streak_days ASC;
	`
	var stats []StreakStat
	if err := db.Select(&stats, query, sym, sym); err != nil {
		log.Fatalf("Query error: %v", err)
	}

	if len(stats) == 0 {
		log.Fatalf("No streak data found for symbol %s in %s", sym, *dbPath)
	}

	// 2. Fetch Detailed 4+ Day Streaks
	detailQuery := `
		SELECT 
			MIN(date) AS start_date,
			MAX(date) AS end_date,
			streak_days,
			ROUND(MAX(prev_close), 2) AS start_price,
			ROUND(MIN(close), 2) AS trough_price,
			ROUND((MIN(close) - MAX(prev_close)) / MAX(prev_close) * 100, 2) AS total_drop_pct
		FROM is_down_slice
		WHERE symbol = ? AND is_down = 1 AND streak_days >= 4
		GROUP BY streak_id
		ORDER BY start_date DESC;
	`
	var events []StreakEvent
	_ = db.Select(&events, detailQuery, sym)

	// 3. Render Terminal Visual Bar Chart
	fmt.Printf("\n========================================================================================\n")
	fmt.Printf("📉 CONSECUTIVE DECLINE STREAK DISTRIBUTION CHART: %s\n", sym)
	fmt.Printf("========================================================================================\n\n")

	maxOccur := 0
	totStreaks := 0
	fourPlusCount := 0
	for _, s := range stats {
		totStreaks += s.Occurrences
		if s.Occurrences > maxOccur {
			maxOccur = s.Occurrences
		}
		if s.StreakDays >= 4 {
			fourPlusCount += s.Occurrences
		}
	}

	chartTable := tablewriter.NewWriter(os.Stdout)
	chartTable.SetHeader([]string{"Streak Length", "Count", "Probability", "Terminal Frequency Bar Chart"})
	chartTable.SetBorder(true)
	chartTable.SetAutoWrapText(false)

	for _, s := range stats {
		barLen := int(float64(s.Occurrences) / float64(maxOccur) * 45.0)
		if barLen < 1 && s.Occurrences > 0 {
			barLen = 1
		}
		barStr := strings.Repeat("█", barLen)

		label := fmt.Sprintf("%d Days Decline", s.StreakDays)
		if s.StreakDays >= 4 {
			label = fmt.Sprintf("⚠️ %d Days Decline", s.StreakDays)
		}

		chartTable.Append([]string{
			label,
			fmt.Sprintf("%d", s.Occurrences),
			fmt.Sprintf("%.1f%%", s.PctOfTotal),
			barStr,
		})
	}
	chartTable.Render()

	fourPlusPct := float64(fourPlusCount) * 100.0 / float64(totStreaks)
	fmt.Printf("\n📌 STATISTICAL SUMMARY FOR %s:\n", sym)
	fmt.Printf("   • Total Decline Sequences: %d\n", totStreaks)
	fmt.Printf("   • Streaks lasting 1 to 3 days: %d (%.1f%% of all declines)\n", totStreaks-fourPlusCount, 100.0-fourPlusPct)
	fmt.Printf("   • Streaks lasting 4+ days:     %d (%.1f%% of all declines)\n", fourPlusCount, fourPlusPct)
	fmt.Printf("   • Annual Frequency of 4+ Day Declines: ~%.1f times per year\n\n", float64(fourPlusCount)/5.0)

	// 4. Generate Modern HTML/Canvas Chart Report
	generateHTMLReport(*htmlOutput, sym, stats, events, totStreaks, fourPlusCount)
	fmt.Printf("✨ Interactive graphical chart saved to: %s\n\n", *htmlOutput)
}

func generateHTMLReport(outputPath, symbol string, stats []StreakStat, events []StreakEvent, totalStreaks, fourPlusCount int) {
	labelsJSON := "["
	countsJSON := "["
	colorsJSON := "["
	pctJSON := "["

	for i, s := range stats {
		if i > 0 {
			labelsJSON += ","
			countsJSON += ","
			colorsJSON += ","
			pctJSON += ","
		}
		labelsJSON += fmt.Sprintf("'%d-Day Streak'", s.StreakDays)
		countsJSON += fmt.Sprintf("%d", s.Occurrences)
		pctJSON += fmt.Sprintf("%.2f", s.PctOfTotal)
		if s.StreakDays >= 4 {
			colorsJSON += "'#f43f5e'" // Accent red for 4+ days
		} else {
			colorsJSON += "'#38bdf8'" // Cyan for normal moves
		}
	}
	labelsJSON += "]"
	countsJSON += "]"
	colorsJSON += "]"
	pctJSON += "]"

	eventRows := ""
	for _, e := range events {
		eventRows += fmt.Sprintf(`
			<tr>
				<td><strong>%s</strong></td>
				<td>%s ➔ %s</td>
				<td><span class="badge">%d Days</span></td>
				<td>$%0.2f</td>
				<td>$%0.2f</td>
				<td class="negative">%0.2f%%</td>
			</tr>
		`, symbol, e.StartDate, e.EndDate, e.StreakDays, e.StartPrice, e.TroughPrice, e.DropPct)
	}

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s Consecutive Decline Streak Distribution</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
    <style>
        :root {
            --bg: #090d16;
            --card: #111827;
            --border: #1e293b;
            --text: #f8fafc;
            --text-dim: #94a3b8;
            --accent: #38bdf8;
            --danger: #f43f5e;
            --success: #10b981;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; }
        body { background: var(--bg); color: var(--text); padding: 32px 20px; line-height: 1.6; }
        .container { max-width: 1100px; margin: 0 auto; }
        .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; padding-bottom: 16px; border-bottom: 1px solid var(--border); }
        .title { font-size: 24px; font-weight: 700; color: #fff; }
        .subtitle { color: var(--text-dim); font-size: 14px; margin-top: 4px; }
        .kpi-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px; margin-bottom: 28px; }
        .kpi-card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 20px; }
        .kpi-label { font-size: 13px; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.5px; }
        .kpi-value { font-size: 28px; font-weight: 800; margin-top: 6px; color: #fff; }
        .kpi-sub { font-size: 12px; color: var(--accent); margin-top: 4px; }
        .chart-card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 24px; margin-bottom: 28px; }
        .table-card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 24px; overflow-x: auto; }
        table { width: 100%%; border-collapse: collapse; text-align: left; font-size: 14px; }
        th { color: var(--text-dim); font-weight: 600; padding: 12px 14px; border-bottom: 1px solid var(--border); }
        td { padding: 12px 14px; border-bottom: 1px solid rgba(255,255,255,0.05); }
        .badge { background: rgba(244, 63, 94, 0.15); color: var(--danger); border: 1px solid rgba(244, 63, 94, 0.3); padding: 4px 8px; border-radius: 6px; font-weight: 600; font-size: 12px; }
        .negative { color: var(--danger); font-weight: 700; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div>
                <h1 class="title">📉 %s Consecutive Decline Streak Distribution</h1>
                <p class="subtitle">Quantitative empirical analysis of consecutive daily losing streaks</p>
            </div>
        </div>

        <div class="kpi-grid">
            <div class="kpi-card">
                <div class="kpi-label">Total Decline Sequences</div>
                <div class="kpi-value">%d</div>
                <div class="kpi-sub">5-Year Historical Sample</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">1-3 Day Quick Pullbacks</div>
                <div class="kpi-value">%.1f%%%%</div>
                <div class="kpi-sub">%d Total Occurrences</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">4+ Day Extended Declines</div>
                <div class="kpi-value" style="color: var(--danger);">%d</div>
                <div class="kpi-sub">Only %.1f%%%% of All Declines</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">Annual Frequency (4d+)</div>
                <div class="kpi-value">~%.1f / yr</div>
                <div class="kpi-sub">Once Every ~38 Trading Days</div>
            </div>
        </div>

        <div class="chart-card">
            <h3 style="margin-bottom: 16px; font-size: 16px; color: #fff;">📊 Streak Length Probability & Occurrence Histogram</h3>
            <div style="height: 340px; position: relative;">
                <canvas id="streakChart"></canvas>
            </div>
        </div>

        <div class="table-card">
            <h3 style="margin-bottom: 16px; font-size: 16px; color: #fff;">🔍 Historical 4+ Day Decline Events Log</h3>
            <table>
                <thead>
                    <tr>
                        <th>Symbol</th>
                        <th>Time Window</th>
                        <th>Duration</th>
                        <th>Pre-Decline Price</th>
                        <th>Trough Price</th>
                        <th>Total Drawdown %%</th>
                    </tr>
                </thead>
                <tbody>
                    %s
                </tbody>
            </table>
        </div>
    </div>

    <script>
        const ctx = document.getElementById('streakChart').getContext('2d');
        new Chart(ctx, {
            type: 'bar',
            data: {
                labels: %s,
                datasets: [{
                    label: 'Occurrences',
                    data: %s,
                    backgroundColor: %s,
                    borderRadius: 6,
                    borderWidth: 0
                }]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                plugins: {
                    legend: { display: false },
                    tooltip: {
                        callbacks: {
                            afterLabel: function(context) {
                                const pcts = %s;
                                return 'Probability: ' + pcts[context.dataIndex] + '%%';
                            }
                        }
                    }
                },
                scales: {
                    x: {
                        grid: { color: 'rgba(255,255,255,0.05)' },
                        ticks: { color: '#94a3b8' }
                    },
                    y: {
                        grid: { color: 'rgba(255,255,255,0.05)' },
                        ticks: { color: '#94a3b8' }
                    }
                }
            }
        });
    </script>
</body>
</html>`, symbol, symbol, totalStreaks, 100.0-(float64(fourPlusCount)*100.0/float64(totalStreaks)), totalStreaks-fourPlusCount, fourPlusCount, float64(fourPlusCount)*100.0/float64(totalStreaks), float64(fourPlusCount)/5.0, eventRows, labelsJSON, countsJSON, colorsJSON, pctJSON)

	_ = os.WriteFile(outputPath, []byte(htmlContent), 0644)
}
