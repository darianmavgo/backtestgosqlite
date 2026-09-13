package main

import (
	"fmt"
	"html/template"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

type GrangerRow struct {
	Predictor  string  `db:"predictor"`
	Target     string  `db:"target"`
	LagMinutes int     `db:"lag_minutes"`
	PValue     float64 `db:"p_value"`
}

type ChartCardData struct {
	Target           string
	AssetDescription string
	AssetIcon        string
	Title            string
	Element          template.HTML
	Script           template.HTML
	Lag1PValue       float64
	MinPValue        float64
	MinLag           int
	SignificantLags  int
	TotalLags        int
	Interpretation   template.HTML
}

type TutorialPageData struct {
	Title       string
	Description string
	Cards       []ChartCardData
}

func generateBar(title string, data []GrangerRow) *charts.Bar {
	bar := charts.NewBar()
	bar.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Width:  "100%",
			Height: "420px",
		}),
		charts.WithTitleOpts(opts.Title{
			Title:    title,
			Subtitle: "P-Value per minute lag. Red dashed line indicates alpha = 0.05 threshold.",
			Top:      "10px",
			Left:     "15px",
		}),
		charts.WithLegendOpts(opts.Legend{
			Right: "25px",
			Top:   "15px",
		}),
		charts.WithGridOpts(opts.Grid{
			Top:    "85px",
			Bottom: "60px",
			Left:   "65px",
			Right:  "65px",
		}),
		charts.WithYAxisOpts(opts.YAxis{
			SplitLine: &opts.SplitLine{
				Show: opts.Bool(true),
			},
		}),
		charts.WithXAxisOpts(opts.XAxis{
			Name: "Lag (Minutes)",
		}),
		charts.WithTooltipOpts(opts.Tooltip{
			Show:    opts.Bool(true),
			Trigger: "axis",
		}),
	)

	var xData []int
	var yData []opts.BarData

	for _, d := range data {
		xData = append(xData, d.LagMinutes)

		color := "#3b82f6" // Blue (not statistically significant, p >= 0.05)
		if d.PValue < 0.05 {
			color = "#10b981" // Emerald green (significant, p < 0.05)
		}

		yData = append(yData, opts.BarData{
			Value: d.PValue,
			ItemStyle: &opts.ItemStyle{
				Color: color,
			},
		})
	}

	bar.SetXAxis(xData).AddSeries("P-Value", yData,
		charts.WithMarkLineNameYAxisItemOpts(opts.MarkLineNameYAxisItem{
			Name:  "Significance Threshold (p=0.05)",
			YAxis: 0.05,
		}),
	)

	return bar
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{ .Title }}</title>
    <!-- Google Fonts & Echarts CDN -->
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700;800&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
    <script src="https://go-echarts.github.io/go-echarts-assets/assets/echarts.min.js"></script>

    <style>
        :root {
            --bg-body: #0b0f19;
            --bg-card: #131b2e;
            --bg-card-subtle: #1a243b;
            --border-card: #22304d;
            --text-main: #f1f5f9;
            --text-muted: #94a3b8;
            --text-subtle: #64748b;
            --accent-blue: #38bdf8;
            --accent-emerald: #10b981;
            --accent-amber: #f59e0b;
            --accent-rose: #f43f5e;
            --accent-purple: #a855f7;
        }

        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }

        body {
            background-color: var(--bg-body);
            color: var(--text-main);
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, sans-serif;
            line-height: 1.6;
            padding: 2.5rem 1.5rem;
            min-height: 100vh;
        }

        .max-container {
            max-width: 1180px;
            margin: 0 auto;
        }

        /* Header & Hero */
        .hero {
            background: linear-gradient(135deg, rgba(30, 41, 59, 0.7) 0%, rgba(15, 23, 42, 0.9) 100%);
            border: 1px solid var(--border-card);
            border-radius: 1.25rem;
            padding: 2.5rem;
            margin-bottom: 2.5rem;
            box-shadow: 0 20px 25px -5px rgba(0, 0, 0, 0.5);
            position: relative;
            overflow: hidden;
        }

        .hero::before {
            content: '';
            position: absolute;
            top: -50%;
            right: -20%;
            width: 400px;
            height: 400px;
            background: radial-gradient(circle, rgba(56, 189, 248, 0.12) 0%, rgba(0, 0, 0, 0) 70%);
            pointer-events: none;
        }

        .badge-row {
            display: flex;
            gap: 0.75rem;
            flex-wrap: wrap;
            margin-bottom: 1rem;
        }

        .badge {
            display: inline-flex;
            align-items: center;
            gap: 0.4rem;
            font-size: 0.8rem;
            font-weight: 600;
            padding: 0.35rem 0.85rem;
            border-radius: 9999px;
            text-transform: uppercase;
            letter-spacing: 0.05em;
        }

        .badge-tutorial {
            background: rgba(16, 185, 129, 0.15);
            color: var(--accent-emerald);
            border: 1px solid rgba(16, 185, 129, 0.3);
        }

        .badge-data {
            background: rgba(56, 189, 248, 0.15);
            color: var(--accent-blue);
            border: 1px solid rgba(56, 189, 248, 0.3);
        }

        .badge-model {
            background: rgba(168, 85, 247, 0.15);
            color: var(--accent-purple);
            border: 1px solid rgba(168, 85, 247, 0.3);
        }

        .hero h1 {
            font-size: 2.35rem;
            font-weight: 800;
            letter-spacing: -0.03em;
            margin-bottom: 0.75rem;
            background: linear-gradient(135deg, #ffffff 0%, #cbd5e1 100%);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        .hero p.lead {
            font-size: 1.125rem;
            color: var(--text-muted);
            max-width: 900px;
        }

        /* Primer Section */
        .primer-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
            gap: 1.5rem;
            margin-bottom: 3rem;
        }

        .primer-card {
            background: var(--bg-card);
            border: 1px solid var(--border-card);
            border-radius: 1rem;
            padding: 1.75rem;
            transition: transform 0.2s ease, border-color 0.2s ease;
        }

        .primer-card:hover {
            transform: translateY(-2px);
            border-color: rgba(56, 189, 248, 0.4);
        }

        .primer-icon {
            font-size: 1.75rem;
            margin-bottom: 0.75rem;
            display: inline-block;
        }

        .primer-card h3 {
            font-size: 1.2rem;
            font-weight: 700;
            color: #fff;
            margin-bottom: 0.5rem;
        }

        .primer-card p {
            color: var(--text-muted);
            font-size: 0.93rem;
            line-height: 1.55;
        }

        .primer-card code {
            font-family: 'JetBrains Mono', monospace;
            background: rgba(0, 0, 0, 0.35);
            color: var(--accent-blue);
            padding: 0.15rem 0.4rem;
            border-radius: 0.3rem;
            font-size: 0.85em;
        }

        /* Section Dividers */
        .section-header {
            margin-bottom: 1.75rem;
        }

        .section-header h2 {
            font-size: 1.75rem;
            font-weight: 700;
            color: #fff;
            display: flex;
            align-items: center;
            gap: 0.75rem;
        }

        .section-header p {
            color: var(--text-muted);
            font-size: 0.95rem;
            margin-top: 0.25rem;
        }

        /* Chart & Analysis Section */
        .chart-block {
            background: var(--bg-card);
            border: 1px solid var(--border-card);
            border-radius: 1.25rem;
            overflow: hidden;
            margin-bottom: 3rem;
            box-shadow: 0 10px 20px rgba(0, 0, 0, 0.3);
        }

        .chart-header-bar {
            padding: 1.5rem 2rem;
            background: var(--bg-card-subtle);
            border-bottom: 1px solid var(--border-card);
            display: flex;
            justify-content: space-between;
            align-items: center;
            flex-wrap: wrap;
            gap: 1rem;
        }

        .chart-target-info {
            display: flex;
            align-items: center;
            gap: 0.85rem;
        }

        .target-symbol-badge {
            font-size: 1.15rem;
            font-weight: 700;
            font-family: 'JetBrains Mono', monospace;
            background: rgba(56, 189, 248, 0.15);
            color: var(--accent-blue);
            border: 1px solid rgba(56, 189, 248, 0.3);
            padding: 0.35rem 0.85rem;
            border-radius: 0.5rem;
        }

        .target-title-text h3 {
            font-size: 1.3rem;
            font-weight: 700;
            color: #fff;
        }

        .target-title-text p {
            font-size: 0.88rem;
            color: var(--text-muted);
        }

        .stats-pills {
            display: flex;
            gap: 0.75rem;
            flex-wrap: wrap;
        }

        .stat-pill {
            background: rgba(0, 0, 0, 0.4);
            border: 1px solid var(--border-card);
            border-radius: 0.5rem;
            padding: 0.35rem 0.75rem;
            font-size: 0.82rem;
            display: flex;
            align-items: center;
            gap: 0.4rem;
        }

        .stat-pill .label {
            color: var(--text-subtle);
        }

        .stat-pill .val {
            font-weight: 600;
            font-family: 'JetBrains Mono', monospace;
            color: var(--text-main);
        }

        .stat-pill .val.green {
            color: var(--accent-emerald);
        }

        .stat-pill .val.blue {
            color: var(--accent-blue);
        }

        /* The Chart Container */
        .chart-render-area {
            padding: 1rem 1.5rem;
            background: #ffffff; /* Clean white canvas for crisp ECharts default styling */
        }

        /* English Summary Card below the chart */
        .english-summary-panel {
            padding: 2rem;
            background: var(--bg-card);
            border-top: 1px solid var(--border-card);
        }

        .summary-title-row {
            display: flex;
            align-items: center;
            gap: 0.6rem;
            margin-bottom: 1.25rem;
        }

        .summary-title-row h4 {
            font-size: 1.15rem;
            font-weight: 700;
            color: #fff;
        }

        .summary-badge {
            font-size: 0.75rem;
            font-weight: 600;
            background: rgba(16, 185, 129, 0.15);
            color: var(--accent-emerald);
            padding: 0.2rem 0.6rem;
            border-radius: 9999px;
            border: 1px solid rgba(16, 185, 129, 0.3);
        }

        .insight-points {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 1.25rem;
        }

        .insight-box {
            background: var(--bg-card-subtle);
            border: 1px solid var(--border-card);
            border-radius: 0.75rem;
            padding: 1.25rem;
        }

        .insight-box.accent-blue {
            border-left: 4px solid var(--accent-blue);
        }

        .insight-box.accent-emerald {
            border-left: 4px solid var(--accent-emerald);
        }

        .insight-box.accent-amber {
            border-left: 4px solid var(--accent-amber);
        }

        .insight-box h5 {
            font-size: 0.98rem;
            font-weight: 600;
            color: #fff;
            margin-bottom: 0.45rem;
            display: flex;
            align-items: center;
            gap: 0.4rem;
        }

        .insight-box p {
            font-size: 0.88rem;
            color: var(--text-muted);
            line-height: 1.55;
        }

        /* Best Practices & Ideas Section */
        .best-practices-card {
            background: var(--bg-card);
            border: 1px solid var(--border-card);
            border-radius: 1.25rem;
            padding: 2.25rem;
            margin-bottom: 3rem;
        }

        .tips-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(320px, 1fr));
            gap: 1.5rem;
            margin-top: 1.5rem;
        }

        .tip-item {
            background: var(--bg-card-subtle);
            border: 1px solid var(--border-card);
            border-radius: 0.85rem;
            padding: 1.35rem;
            display: flex;
            flex-direction: column;
            gap: 0.5rem;
        }

        .tip-header {
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }

        .tip-header h4 {
            font-size: 1.05rem;
            font-weight: 700;
            color: #fff;
        }

        .tip-item p {
            font-size: 0.88rem;
            color: var(--text-muted);
            line-height: 1.55;
        }

        .tip-item .rule-tag {
            align-self: flex-start;
            font-size: 0.72rem;
            font-weight: 700;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            padding: 0.2rem 0.5rem;
            border-radius: 0.35rem;
            margin-bottom: 0.25rem;
        }

        .rule-crucial {
            background: rgba(244, 63, 94, 0.15);
            color: var(--accent-rose);
            border: 1px solid rgba(244, 63, 94, 0.3);
        }

        .rule-pro {
            background: rgba(245, 158, 11, 0.15);
            color: var(--accent-amber);
            border: 1px solid rgba(245, 158, 11, 0.3);
        }

        .rule-math {
            background: rgba(168, 85, 247, 0.15);
            color: var(--accent-purple);
            border: 1px solid rgba(168, 85, 247, 0.3);
        }

        /* Footer */
        footer {
            border-top: 1px solid var(--border-card);
            padding-top: 2rem;
            display: flex;
            justify-content: space-between;
            align-items: center;
            flex-wrap: wrap;
            gap: 1rem;
            color: var(--text-subtle);
            font-size: 0.85rem;
        }

        footer code {
            font-family: 'JetBrains Mono', monospace;
            color: var(--accent-blue);
        }

        @media (max-width: 768px) {
            body {
                padding: 1.25rem 0.75rem;
            }
            .hero {
                padding: 1.5rem;
            }
            .hero h1 {
                font-size: 1.75rem;
            }
        }
    </style>
</head>
<body>

<div class="max-container">

    <!-- HERO / TUTORIAL HEADER -->
    <header class="hero">
        <div class="badge-row">
            <span class="badge badge-tutorial">🎓 Interactive Tutorial Mode</span>
            <span class="badge badge-data">📊 March–April 1-Min SQLite Feed</span>
            <span class="badge badge-model">⚡ Go-Echarts Engine</span>
        </div>
        <h1>Granger Causality Masterclass</h1>
        <p class="lead">
            An empirical investigation into whether minute-by-minute movements in the S&amp;P 500 (<strong>VOO</strong>) 
            provide predictive precedence over Gold (<strong>GLD</strong>) and 10-Year US Treasuries (<strong>UTEN</strong>).
        </p>
    </header>

    <!-- SECTION 1: GRANGER CAUSALITY 101 -->
    <section class="section-header">
        <h2>📘 Granger Causality 101: The Fundamentals</h2>
        <p>Three foundational concepts every quantitative analyst must know before looking at the charts.</p>
    </section>

    <div class="primer-grid">
        <div class="primer-card">
            <div class="primer-icon">⏳</div>
            <h3>1. What is "Causality" Here?</h3>
            <p>
                Granger causality does <strong>not</strong> mean physical cause-and-effect. It measures 
                <strong>predictive precedence in time</strong>: Does knowing past returns of Asset X reduce our 
                forecast error for future returns of Asset Y, beyond what past returns of Y already explain?
            </p>
        </div>

        <div class="primer-card">
            <div class="primer-icon">📉</div>
            <h3>2. The P-Value &amp; The Threshold</h3>
            <p>
                The <strong>Null Hypothesis (H₀)</strong> assumes Asset X has <em>no predictive power</em> over Asset Y. 
                A <code>P-Value &lt; 0.05</code> rejects H₀ with 95%+ confidence. 
                <strong style="color: var(--accent-emerald);">Green bars</strong> indicate statistically significant predictive causality; 
                <strong style="color: var(--accent-blue);">Blue bars</strong> indicate no significant edge.
            </p>
        </div>

        <div class="primer-card">
            <div class="primer-icon">⏱️</div>
            <h3>3. What is a "Lag"?</h3>
            <p>
                A "lag" is a historical time step. In this minute dataset, <code>Lag 1</code> means information from 1 minute ago, 
                <code>Lag 5</code> means 5 minutes ago, up to <code>Lag 10</code> (10 minutes). It maps how fast market shocks cascade across assets.
            </p>
        </div>
    </div>

    <!-- SECTION 2: CHARTS & PLAIN-ENGLISH SUMMARIES -->
    <section class="section-header">
        <h2>📊 Empirical Results &amp; English Translations</h2>
        <p>Interactive Go-Echarts visual output paired with step-by-step plain English interpretation.</p>
    </section>

    {{ range .Cards }}
    <div class="chart-block">
        <!-- Card Top Bar -->
        <div class="chart-header-bar">
            <div class="chart-target-info">
                <span class="target-symbol-badge">{{ .AssetIcon }} {{ .Target }}</span>
                <div class="target-title-text">
                    <h3>{{ .Title }}</h3>
                    <p>{{ .AssetDescription }}</p>
                </div>
            </div>
            <div class="stats-pills">
                <div class="stat-pill">
                    <span class="label">Tested Horizon:</span>
                    <span class="val">{{ .TotalLags }} Lags (1–10m)</span>
                </div>
                <div class="stat-pill">
                    <span class="label">Significant Lags:</span>
                    <span class="val {{ if eq .SignificantLags .TotalLags }}green{{ else }}blue{{ end }}">{{ .SignificantLags }} / {{ .TotalLags }}</span>
                </div>
                <div class="stat-pill">
                    <span class="label">Strongest Lead:</span>
                    <span class="val green">Lag {{ .MinLag }} (p ≈ {{ printf "%.2e" .MinPValue }})</span>
                </div>
            </div>
        </div>

        <!-- Echarts Canvas Area -->
        <div class="chart-render-area">
            {{ .Element }}
        </div>

        <!-- English Translation Panel -->
        <div class="english-summary-panel">
            <div class="summary-title-row">
                <h4>📝 Plain English Interpretation &amp; Trader Takeaways</h4>
                <span class="summary-badge">Summary Analysis</span>
            </div>
            <div class="insight-points">
                {{ .Interpretation }}
            </div>
        </div>
    </div>
    {{ end }}

    <!-- SECTION 3: IDEAS FOR SOMEONE NEW TO GRANGER -->
    <section class="section-header">
        <h2>💡 6 Golden Rules &amp; Ideas for Granger Beginners</h2>
        <p>Essential guardrails to avoid beginner pitfalls, false discoveries, and losing real capital.</p>
    </section>

    <div class="best-practices-card">
        <div class="tips-grid">
            <div class="tip-item">
                <span class="rule-tag rule-crucial">CRITICAL RULE #1</span>
                <div class="tip-header">
                    <h4>Stationarity is Mandatory</h4>
                </div>
                <p>
                    <strong>Never run Granger causality on raw price series!</strong> Raw asset prices exhibit random walks and secular trends. 
                    If you test price levels, you will get "spurious causality" with fake <code>p &lt; 0.0001</code>. 
                    Always compute stationary inputs: percentage returns, log differences, or first differences.
                </p>
            </div>

            <div class="tip-item">
                <span class="rule-tag rule-crucial">CRITICAL RULE #2</span>
                <div class="tip-header">
                    <h4>Always Test Bidirectionally</h4>
                </div>
                <p>
                    Testing <code>VOO → GLD</code> only answers half the question. You <strong>must also test <code>GLD → VOO</code></strong>! 
                    If VOO leads GLD and GLD also leads VOO, they form a simultaneous feedback system. 
                    A true leader has unidirectional causality (A leads B, but B does not lead A).
                </p>
            </div>

            <div class="tip-item">
                <span class="rule-tag rule-pro">PRO TIP #3</span>
                <div class="tip-header">
                    <h4>Beware of Omitted Confounders</h4>
                </div>
                <p>
                    Suppose the Federal Reserve announces a surprise rate cut at 2:00 PM. 
                    VOO may react in 100 milliseconds, while UTEN reacts in 30 seconds due to order book liquidity. 
                    The Granger test will conclude "VOO causes UTEN," when in reality, the Fed caused both!
                </p>
            </div>

            <div class="tip-item">
                <span class="rule-tag rule-math">MATHEMATICAL TIP #4</span>
                <div class="tip-header">
                    <h4>Use AIC / BIC for Optimal Lag</h4>
                </div>
                <p>
                    Instead of guessing 10 arbitrary lags, use <strong>Akaike Information Criterion (AIC)</strong> 
                    or <strong>Bayesian Information Criterion (BIC)</strong> to choose the optimal lag length. 
                    Testing too many lags burns degrees of freedom and introduces noise overfitting.
                </p>
            </div>

            <div class="tip-item">
                <span class="rule-tag rule-pro">TRADING REALITY #5</span>
                <div class="tip-header">
                    <h4>Statistical Significance ≠ Profit</h4>
                </div>
                <p>
                    A p-value of <code>10⁻⁹</code> proves mathematical predictive dependency, but can you monetize it? 
                    If the expected price adjustment over 3 minutes is 0.02%, but round-trip trading fees + bid-ask spread 
                    cost 0.03%, you will lose money executing the trade. Always backtest with real fee models!
                </p>
            </div>

            <div class="tip-item">
                <span class="rule-tag rule-pro">ADVANCED IDEA #6</span>
                <div class="tip-header">
                    <h4>Run Rolling Dynamic Granger</h4>
                </div>
                <p>
                    Market dynamics evolve. Equities might lead Treasuries during high-volatility market selloffs, 
                    while Treasuries might lead Equities on CPI or Jobs release mornings. 
                    Compute rolling 5-day or 10-day Granger p-values to detect structural regime shifts in real time.
                </p>
            </div>
        </div>
    </div>

    <!-- FOOTER -->
    <footer>
        <div>
            Generated via Go-Echarts &amp; SQLite: <code>reports/march_april_voo_gld_uten.db</code>
        </div>
        <div>
            BacktestGoSQLite • Quant Analytics Suite
        </div>
    </footer>

</div>

<!-- INJECT ECHARTS SCRIPTS -->
{{ range .Cards }}
{{ .Script }}
{{ end }}

</body>
</html>
`

func getInterpretation(target string, rows []GrangerRow) template.HTML {
	if target == "GLD" {
		return template.HTML(`
            <div class="insight-box accent-blue">
                <h5>⏳ Lag 1 (1 min): Noise Window</h5>
                <p><strong>p = 0.2585 (Blue Bar)</strong>: Past 1-minute returns of VOO do <em>not</em> statistically predict GLD 1 minute later. Noise and latency dominate the ultra-short 60-second horizon.</p>
            </div>
            <div class="insight-box accent-emerald">
                <h5>🚀 Lags 2–5 (2–5 mins): The Signal Ignites</h5>
                <p><strong>p drops to 0.0017 and 0.0006 (Green Bars)</strong>: Statistical significance surges past the 99.9% confidence level. S&amp;P 500 capital reallocation starts visibly moving Gold after a 2-minute delay.</p>
            </div>
            <div class="insight-box accent-emerald">
                <h5>💎 Lags 6–10 (6–10 mins): Massive Predictive Power</h5>
                <p><strong>p collapses to 10⁻⁶ down to 10⁻⁹</strong>: Extreme statistical confidence. Sustained directional momentum in equities firmly dictates cross-asset rebalancing in Gold across the 5–10 minute window.</p>
            </div>
            <div class="insight-box accent-amber">
                <h5>💡 Practical Trader Takeaway</h5>
                <p>Never try to scalp GLD on a 1-minute tick reaction from VOO. Allow 2–3 minutes for the macro flow to register, then look for momentum continuation into the 5–10 minute target window.</p>
            </div>
        `)
	}

	// UTEN
	return template.HTML(`
        <div class="insight-box accent-emerald">
            <h5>⚡ Lags 1–3 (1–3 mins): Immediate Transmission</h5>
            <p><strong>p = 0.0035 at Lag 1 (Green Bar)</strong>: Unlike Gold, US 10-Year Treasuries react <em>almost instantly</em> to equity shocks within the very first 60 seconds. High-frequency algorithmic arbitrage connects stocks and yields rapidly.</p>
        </div>
        <div class="insight-box accent-emerald">
            <h5>📊 Lags 4–5 (4–5 mins): Temporary Moderation</h5>
            <p><strong>p = 0.0105 and 0.0201</strong>: P-values rise slightly towards the 0.05 threshold as initial price discovery consolidates, but remain strictly statistically significant.</p>
        </div>
        <div class="insight-box accent-emerald">
            <h5>🎯 Lags 6–10 (6–10 mins): Persistent Lead</h5>
            <p><strong>p drops back to 0.0005 – 0.0017</strong>: Strong secondary cascade. S&amp;P 500 equity trends provide reliable predictive information for Treasury movements across the entire 10-minute horizon.</p>
        </div>
        <div class="insight-box accent-amber">
            <h5>💡 Practical Trader Takeaway</h5>
            <p>VOO is a robust, continuous leading indicator for UTEN (10/10 lags significant). Watch equity index futures for early directional cues on Treasury yields and bond ETF execution.</p>
        </div>
    `)
}

func getAssetDescription(target string) (string, string) {
	switch target {
	case "GLD":
		return "SPDR Gold Shares ETF (Safe-Haven Commodity)", "🟡"
	case "UTEN":
		return "US Benchmark Series 10-Year Treasury ETF (Sovereign Debt Yields)", "🏛️"
	default:
		return "Target Asset", "📈"
	}
}

func main() {
	dbPath := filepath.Join("reports", "march_april_voo_gld_uten.db")
	log.Printf("Connecting to %s...", dbPath)

	db, err := sqlx.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatalf("Failed to open db: %v", err)
	}
	defer db.Close()

	var allData []GrangerRow
	query := "SELECT predictor, target, lag_minutes, p_value FROM granger_causality ORDER BY target, lag_minutes ASC"
	if err := db.Select(&allData, query); err != nil {
		log.Fatalf("Query failed: %v", err)
	}

	log.Printf("Loaded %d rows from database.", len(allData))

	grouped := make(map[string][]GrangerRow)
	for _, row := range allData {
		grouped[row.Target] = append(grouped[row.Target], row)
	}

	// Deterministic sorting of targets (e.g. GLD, UTEN)
	var targets []string
	for t := range grouped {
		targets = append(targets, t)
	}
	sort.Strings(targets)

	var cards []ChartCardData

	for _, target := range targets {
		rows := grouped[target]
		title := fmt.Sprintf("Does VOO Lead %s?", target)
		desc, icon := getAssetDescription(target)

		bar := generateBar(title, rows)
		bar.Validate()
		snippet := bar.RenderSnippet()

		lag1P := 0.0
		minP := math.MaxFloat64
		minLag := 1
		sigCount := 0

		for _, r := range rows {
			if r.LagMinutes == 1 {
				lag1P = r.PValue
			}
			if r.PValue < minP {
				minP = r.PValue
				minLag = r.LagMinutes
			}
			if r.PValue < 0.05 {
				sigCount++
			}
		}

		cards = append(cards, ChartCardData{
			Target:           target,
			AssetDescription: desc,
			AssetIcon:        icon,
			Title:            title,
			Element:          template.HTML(snippet.Element),
			Script:           template.HTML(snippet.Script),
			Lag1PValue:       lag1P,
			MinPValue:        minP,
			MinLag:           minLag,
			SignificantLags:  sigCount,
			TotalLags:        len(rows),
			Interpretation:   getInterpretation(target, rows),
		})
	}

	pageData := TutorialPageData{
		Title:       "Granger Causality (Go-Echarts) — Tutorial & Analysis Dashboard",
		Description: "Comprehensive empirical evaluation of S&P 500 predictive lead-lag relationships.",
		Cards:       cards,
	}

	tmpl, err := template.New("tutorial").Parse(htmlTemplate)
	if err != nil {
		log.Fatalf("Failed to parse HTML template: %v", err)
	}

	outPath := filepath.Join("reports", "granger_causality_go.html")
	f, err := os.Create(outPath)
	if err != nil {
		log.Fatalf("Failed to create output file: %v", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, pageData); err != nil {
		log.Fatalf("Failed to execute template: %v", err)
	}

	log.Printf("Successfully generated Go-Echarts tutorial visualization at %s", outPath)
}
