package main

import (
	"encoding/json"
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

type DailyPoint struct {
	Date       string  `db:"date"`
	Close      float64 `db:"close"`
	PrevClose  float64 `db:"prev_close"`
	IsDown     int     `db:"is_down"`
	StreakID   int     `db:"streak_id"`
	StreakDays int     `db:"streak_days"`
}

func main() {
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	symbolFlag := flag.String("symbol", "VOO", "Symbol to chart (e.g. VOO, QQQ, SPY)")
	htmlOutput := flag.String("html", "reports/voo_streaks.html", "Path to export interactive HTML chart report")
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

	// 3. Fetch Full Chronological Daily Price Series with Streak Flags
	timeQuery := `
		SELECT 
			date,
			close,
			COALESCE(prev_close, close) AS prev_close,
			COALESCE(is_down, 0) AS is_down,
			COALESCE(streak_id, 0) AS streak_id,
			COALESCE(streak_days, 0) AS streak_days
		FROM is_down_slice
		WHERE symbol = ?
		ORDER BY date ASC;
	`
	var dailySeries []DailyPoint
	if err := db.Select(&dailySeries, timeQuery, sym); err != nil {
		log.Fatalf("Time series query error: %v", err)
	}

	// 4. Render Terminal Visual Bar Chart
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

	// 5. Generate Modern HTML/Canvas Interactive Time Graph Report
	generateInteractiveTimeGraphReport(*htmlOutput, sym, stats, events, dailySeries, totStreaks, fourPlusCount)
	fmt.Printf("✨ Interactive graphical time-series chart saved to: %s\n\n", *htmlOutput)
}

func generateInteractiveTimeGraphReport(
	outputPath, symbol string,
	stats []StreakStat,
	events []StreakEvent,
	series []DailyPoint,
	totalStreaks, fourPlusCount int,
) {
	// 1. Prepare JSON payloads for Time Graph
	type PointData struct {
		Date       string  `json:"date"`
		Close      float64 `json:"close"`
		PrevClose  float64 `json:"prev_close"`
		IsDown     int     `json:"is_down"`
		StreakDays int     `json:"streak_days"`
		IsStreak4  bool    `json:"is_streak_4"`
	}

	points := make([]PointData, len(series))
	for i, d := range series {
		points[i] = PointData{
			Date:       d.Date,
			Close:      d.Close,
			PrevClose:  d.PrevClose,
			IsDown:     d.IsDown,
			StreakDays: d.StreakDays,
			IsStreak4:  (d.IsDown == 1 && d.StreakDays >= 4),
		}
	}
	pointsJSON, _ := json.Marshal(points)

	// 2. Prepare JSON for Histogram
	var labels []string
	var counts []int
	var colors []string
	var pcts []float64

	for _, s := range stats {
		labels = append(labels, fmt.Sprintf("%d-Day Streak", s.StreakDays))
		counts = append(counts, s.Occurrences)
		pcts = append(pcts, s.PctOfTotal)
		if s.StreakDays >= 4 {
			colors = append(colors, "#f43f5e") // Accent red for 4+ days
		} else {
			colors = append(colors, "#38bdf8") // Cyan for normal pullbacks
		}
	}

	labelsJSON, _ := json.Marshal(labels)
	countsJSON, _ := json.Marshal(counts)
	colorsJSON, _ := json.Marshal(colors)
	pctJSON, _ := json.Marshal(pcts)

	eventRows := ""
	for i, e := range events {
		eventRows += fmt.Sprintf(`
			<tr onclick="focusStreak('%s', '%s')" style="cursor: pointer;" title="Click to zoom timeline to this streak">
				<td><strong>%s</strong></td>
				<td>%s ➔ %s</td>
				<td><span class="badge">%d Days</span></td>
				<td>$%0.2f</td>
				<td>$%0.2f</td>
				<td class="negative">%0.2f%%</td>
				<td><button class="btn-sm" onclick="event.stopPropagation(); focusStreak('%s', '%s')">🔎 View</button></td>
			</tr>
		`, e.StartDate, e.EndDate, symbol, e.StartDate, e.EndDate, e.StreakDays, e.StartPrice, e.TroughPrice, e.DropPct, e.StartDate, e.EndDate)
		if i >= 30 {
			break
		}
	}

	quickPct := 100.0 - (float64(fourPlusCount) * 100.0 / float64(totalStreaks))
	fourPct := float64(fourPlusCount) * 100.0 / float64(totalStreaks)
	annualFreq := float64(fourPlusCount) / 5.0

	templateHTML := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{SYMBOL}} Price Timeline & Decline Streaks</title>
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
            --accent-glow: rgba(56, 189, 248, 0.25);
            --danger: #f43f5e;
            --danger-glow: rgba(244, 63, 94, 0.35);
            --success: #10b981;
        }
        * { box-sizing: border-box; margin: 0; padding: 0; font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; }
        body { background: var(--bg); color: var(--text); padding: 32px 20px; line-height: 1.6; }
        .container { max-width: 1200px; margin: 0 auto; }
        .header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 24px; padding-bottom: 16px; border-bottom: 1px solid var(--border); flex-wrap: wrap; gap: 12px; }
        .title { font-size: 24px; font-weight: 800; color: #fff; letter-spacing: -0.5px; }
        .subtitle { color: var(--text-dim); font-size: 14px; margin-top: 4px; }
        .kpi-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 16px; margin-bottom: 24px; }
        .kpi-card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 20px; box-shadow: 0 4px 12px rgba(0,0,0,0.2); }
        .kpi-label { font-size: 13px; color: var(--text-dim); text-transform: uppercase; letter-spacing: 0.5px; }
        .kpi-value { font-size: 28px; font-weight: 800; margin-top: 6px; color: #fff; }
        .kpi-sub { font-size: 12px; color: var(--accent); margin-top: 4px; }
        .chart-card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 24px; margin-bottom: 24px; }
        .chart-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 18px; flex-wrap: wrap; gap: 10px; }
        .chart-title { font-size: 17px; font-weight: 700; color: #fff; display: flex; align-items: center; gap: 8px; }
        .controls { display: flex; gap: 8px; align-items: center; }
        .btn { background: #1e293b; color: var(--text); border: 1px solid var(--border); padding: 6px 14px; border-radius: 8px; font-size: 13px; font-weight: 600; cursor: pointer; transition: all 0.2s ease; }
        .btn:hover, .btn.active { background: var(--accent); color: #090d16; border-color: var(--accent); }
        .btn-sm { background: rgba(56, 189, 248, 0.15); color: var(--accent); border: 1px solid rgba(56, 189, 248, 0.3); padding: 4px 10px; border-radius: 6px; font-size: 11px; font-weight: 700; cursor: pointer; }
        .btn-sm:hover { background: var(--accent); color: #090d16; }
        .grid-2col { display: grid; grid-template-columns: 2fr 1fr; gap: 20px; margin-bottom: 24px; }
        @media (max-width: 900px) { .grid-2col { grid-template-columns: 1fr; } }
        .table-card { background: var(--card); border: 1px solid var(--border); border-radius: 12px; padding: 24px; overflow-x: auto; }
        table { width: 100%; border-collapse: collapse; text-align: left; font-size: 14px; }
        th { color: var(--text-dim); font-weight: 600; padding: 12px 14px; border-bottom: 1px solid var(--border); }
        td { padding: 12px 14px; border-bottom: 1px solid rgba(255,255,255,0.05); }
        tbody tr:hover { background: var(--card-hover); }
        .badge { background: rgba(244, 63, 94, 0.15); color: var(--danger); border: 1px solid rgba(244, 63, 94, 0.3); padding: 4px 8px; border-radius: 6px; font-weight: 700; font-size: 12px; }
        .negative { color: var(--danger); font-weight: 700; }
        .legend-indicator { display: inline-block; width: 10px; height: 10px; border-radius: 50%; margin-right: 6px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <div>
                <h1 class="title">📈 {{SYMBOL}} Price Timeline & Decline Streak Highlight Graph</h1>
                <p class="subtitle">Chronological historical chart with red glowing overlays for continuous &ge; 4-day losing streaks</p>
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
                <div class="kpi-label">Total Decline Sequences</div>
                <div class="kpi-value">{{TOTAL_STREAKS}}</div>
                <div class="kpi-sub">5-Year Daily Dataset</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">1-3 Day Quick Pullbacks</div>
                <div class="kpi-value">{{QUICK_PCT}}%</div>
                <div class="kpi-sub">{{QUICK_COUNT}} Total Occurrences</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">4+ Day Extended Declines</div>
                <div class="kpi-value" style="color: var(--danger);">{{FOUR_PLUS_COUNT}}</div>
                <div class="kpi-sub">Highlighted in Red on Time Graph ({{FOUR_PLUS_PCT}}%)</div>
            </div>
            <div class="kpi-card">
                <div class="kpi-label">Annual Frequency (4d+)</div>
                <div class="kpi-value">~{{ANNUAL_FREQ}} / yr</div>
                <div class="kpi-sub">Occurs Once Every ~38 Trading Days</div>
            </div>
        </div>

        <!-- MAIN INTERACTIVE TIME GRAPH -->
        <div class="chart-card">
            <div class="chart-header">
                <div class="chart-title">
                    <span>📅 {{SYMBOL}} Daily Close Price Timeline</span>
                    <span style="font-size: 13px; font-weight: normal; color: var(--text-dim); margin-left: 12px;">
                        <span class="legend-indicator" style="background: #38bdf8;"></span>Price Line &nbsp;
                        <span class="legend-indicator" style="background: #f43f5e;"></span>4+ Day Decline Highlight
                    </span>
                </div>
                <div style="font-size: 12px; color: var(--text-dim);">
                    Hover over red markers to inspect streak drop percentage
                </div>
            </div>
            <div style="height: 420px; position: relative;">
                <canvas id="timelineChart"></canvas>
            </div>
        </div>

        <!-- 2-COLUMN SECTION: HISTOGRAM & RECENT EVENTS -->
        <div class="grid-2col">
            <div class="chart-card">
                <div class="chart-title" style="margin-bottom: 16px;">
                    <span>📊 Decline Streak Duration Histogram</span>
                </div>
                <div style="height: 320px; position: relative;">
                    <canvas id="histogramChart"></canvas>
                </div>
            </div>

            <div class="table-card">
                <div class="chart-title" style="margin-bottom: 16px;">
                    <span>🔍 4+ Day Decline Log</span>
                </div>
                <table>
                    <thead>
                        <tr>
                            <th>Symbol</th>
                            <th>Window</th>
                            <th>Days</th>
                            <th>Drop %</th>
                            <th>Action</th>
                        </tr>
                    </thead>
                    <tbody>
                        {{EVENT_ROWS}}
                    </tbody>
                </table>
            </div>
        </div>
    </div>

    <script>
        const rawPoints = {{POINTS_JSON}};
        let currentFilter = 'all';

        function getFilteredPoints() {
            if (currentFilter === '6m') return rawPoints.slice(-126);
            if (currentFilter === '1y') return rawPoints.slice(-252);
            if (currentFilter === '2y') return rawPoints.slice(-504);
            return rawPoints;
        }

        function buildTimelineData(pts) {
            const labels = pts.map(p => p.date);
            const closePrices = pts.map(p => p.close);

            // Point styles & colors: highlight 4+ day decline streaks with glowing red points
            const pointBackgroundColors = pts.map(p => p.is_streak_4 ? '#f43f5e' : 'transparent');
            const pointBorderColors = pts.map(p => p.is_streak_4 ? '#ffffff' : 'transparent');
            const pointRadius = pts.map(p => p.is_streak_4 ? 5 : 0);
            const pointHoverRadius = pts.map(p => p.is_streak_4 ? 8 : 4);

            return {
                labels: labels,
                datasets: [{
                    label: '{{SYMBOL}} Price',
                    data: closePrices,
                    borderColor: '#38bdf8',
                    borderWidth: 2,
                    pointBackgroundColor: pointBackgroundColors,
                    pointBorderColor: pointBorderColors,
                    pointRadius: pointRadius,
                    pointHoverRadius: pointHoverRadius,
                    tension: 0.1,
                    fill: {
                        target: 'origin',
                        above: 'rgba(56, 189, 248, 0.05)'
                    }
                }]
            };
        }

        // Initialize Timeline Chart
        const timelineCtx = document.getElementById('timelineChart').getContext('2d');
        let timelineChart = new Chart(timelineCtx, {
            type: 'line',
            data: buildTimelineData(getFilteredPoints()),
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
                        titleColor: '#fff',
                        bodyColor: '#94a3b8',
                        padding: 12,
                        callbacks: {
                            label: function(context) {
                                const pt = getFilteredPoints()[context.dataIndex];
                                let label = 'Close: $' + pt.close.toFixed(2);
                                if (pt.is_streak_4) {
                                    label += ' | ⚠️ ' + pt.streak_days + '-Day Decline Streak!';
                                }
                                return label;
                            }
                        }
                    }
                },
                scales: {
                    x: {
                        grid: { color: 'rgba(255,255,255,0.03)' },
                        ticks: { color: '#94a3b8', maxTicksLimit: 12 }
                    },
                    y: {
                        grid: { color: 'rgba(255,255,255,0.05)' },
                        ticks: {
                            color: '#94a3b8',
                            callback: function(value) { return '$' + value; }
                        }
                    }
                }
            }
        });

        function setTimeframe(tf) {
            currentFilter = tf;
            document.querySelectorAll('.controls .btn').forEach(b => b.classList.remove('active'));
            const activeBtn = document.getElementById('btn-' + tf);
            if (activeBtn) activeBtn.classList.add('active');

            timelineChart.data = buildTimelineData(getFilteredPoints());
            timelineChart.update();
        }

        function focusStreak(startDate, endDate) {
            const idx = rawPoints.findIndex(p => p.date === startDate);
            if (idx === -1) return;
            
            const startIdx = Math.max(0, idx - 25);
            const endIdx = Math.min(rawPoints.length, idx + 35);
            const zoomed = rawPoints.slice(startIdx, endIdx);

            document.querySelectorAll('.controls .btn').forEach(b => b.classList.remove('active'));
            timelineChart.data = buildTimelineData(zoomed);
            timelineChart.update();
            window.scrollTo({ top: 120, behavior: 'smooth' });
        }

        // Initialize Histogram Chart
        const histCtx = document.getElementById('histogramChart').getContext('2d');
        new Chart(histCtx, {
            type: 'bar',
            data: {
                labels: {{LABELS_JSON}},
                datasets: [{
                    data: {{COUNTS_JSON}},
                    backgroundColor: {{COLORS_JSON}},
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
                                const pcts = {{PCT_JSON}};
                                return 'Probability: ' + pcts[context.dataIndex] + '%';
                            }
                        }
                    }
                },
                scales: {
                    x: { grid: { color: 'rgba(255,255,255,0.04)' }, ticks: { color: '#94a3b8' } },
                    y: { grid: { color: 'rgba(255,255,255,0.05)' }, ticks: { color: '#94a3b8' } }
                }
            }
        });

        document.getElementById('btn-all').classList.add('active');
    </script>
</body>
</html>`

	replacements := map[string]string{
		"{{SYMBOL}}":           symbol,
		"{{TOTAL_STREAKS}}":    fmt.Sprintf("%d", totalStreaks),
		"{{QUICK_PCT}}":        fmt.Sprintf("%.1f", quickPct),
		"{{QUICK_COUNT}}":      fmt.Sprintf("%d", totalStreaks-fourPlusCount),
		"{{FOUR_PLUS_COUNT}}":  fmt.Sprintf("%d", fourPlusCount),
		"{{FOUR_PLUS_PCT}}":    fmt.Sprintf("%.1f", fourPct),
		"{{ANNUAL_FREQ}}":      fmt.Sprintf("%.1f", annualFreq),
		"{{EVENT_ROWS}}":       eventRows,
		"{{POINTS_JSON}}":      string(pointsJSON),
		"{{LABELS_JSON}}":      string(labelsJSON),
		"{{COUNTS_JSON}}":      string(countsJSON),
		"{{COLORS_JSON}}":      string(colorsJSON),
		"{{PCT_JSON}}":         string(pctJSON),
	}

	htmlContent := templateHTML
	for k, v := range replacements {
		htmlContent = strings.ReplaceAll(htmlContent, k, v)
	}

	_ = os.WriteFile(outputPath, []byte(htmlContent), 0644)
}
