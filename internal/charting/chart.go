// Package charting provides reusable interactive HTML reports with Chart.js.
package charting

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"

	"github.com/darianmavgo/backtestgosqlite/internal/dipsim"
	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

//go:embed chart_template.html
var defaultHTMLTemplate string

// Series represents one line on the equity/drawdown charts.
type Series struct {
	Label       string    `json:"label"`
	Equity      []float64 `json:"equity"`
	Drawdown    []float64 `json:"drawdown"`
	Color       string    `json:"color"`
	BorderWidth float64   `json:"border_width"`
	IsDash      bool      `json:"is_dash"`
}

// ChartData holds the raw series data marshaled to JSON for Chart.js.
type ChartData struct {
	Dates  []string `json:"dates"`
	Series []Series `json:"series"`
}

// KPICard represents a hero statistic widget in the header.
type KPICard struct {
	Label string
	Value string
	Sub   string
	Color string
}

// ComparisonTable holds matrix rows for multi-strategy reports.
type ComparisonTable struct {
	Headers []string
	Rows    [][]string
}

// ReportView is passed to the HTML template renderer.
type ReportView struct {
	Title           string
	Subtitle        string
	KPICards        []KPICard
	Trades          []dipsim.ExecutedTrade
	ComparisonTable *ComparisonTable
	ChartDataJSON   template.JS
}

// GenerateHTML renders the Chart.js report and saves it to outputPath.
func GenerateHTML(outputPath string, view ReportView) error {
	dir := filepath.Dir(outputPath)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}

	funcMap := template.FuncMap{
		"multiply": func(a, b float64) float64 { return a * b },
	}

	tmpl, err := template.New("chart").Funcs(funcMap).Parse(defaultHTMLTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse chart template: %w", err)
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create report file %s: %w", outputPath, err)
	}
	defer f.Close()

	return tmpl.Execute(f, view)
}

// FromResult builds a complete ReportView from a single DipSimResult + benchmark bars.
func FromResult(result dipsim.DipSimResult, benchmarkBars []models.BarData, initialCapital float64) ReportView {
	dates := make([]string, len(result.DailySeries))
	stratEq := make([]float64, len(result.DailySeries))
	stratDD := make([]float64, len(result.DailySeries))

	for i, pt := range result.DailySeries {
		dates[i] = pt.Date
		stratEq[i] = pt.Equity
		stratDD[i] = pt.Drawdown
	}

	// Build benchmark series
	bmEq := make([]float64, len(dates))
	bmDD := make([]float64, len(dates))
	if len(benchmarkBars) > 0 {
		bmStart := benchmarkBars[0].Close
		bmPeak := initialCapital
		bmMap := make(map[string]float64, len(benchmarkBars))
		for _, b := range benchmarkBars {
			bmMap[b.Date] = b.Close
		}
		lastEq := initialCapital
		for i, d := range dates {
			if closePrice, ok := bmMap[d]; ok {
				lastEq = (closePrice / bmStart) * initialCapital
			}
			if lastEq > bmPeak {
				bmPeak = lastEq
			}
			dd := (bmPeak - lastEq) / bmPeak * 100.0
			bmEq[i] = lastEq
			bmDD[i] = dd
		}
	}

	chartData := ChartData{
		Dates: dates,
		Series: []Series{
			{
				Label:       result.Config.TradeSymbol + " Strategy",
				Equity:      stratEq,
				Drawdown:    stratDD,
				Color:       "#10b981",
				BorderWidth: 2.8,
			},
			{
				Label:       result.Config.SignalSymbol + " Buy & Hold Benchmark",
				Equity:      bmEq,
				Drawdown:    bmDD,
				Color:       "#a855f7",
				BorderWidth: 1.8,
				IsDash:      true,
			},
		},
	}

	jsonData, _ := json.Marshal(chartData)

	kpis := []KPICard{
		{
			Label: "Final Account Value",
			Value: fmt.Sprintf("$%.2f", result.EndingCap),
			Sub:   fmt.Sprintf("+$%.2f Net Profit", result.NetProfit),
			Color: "#10b981",
		},
		{
			Label: "Annualized CAGR",
			Value: fmt.Sprintf("%.2f%% / yr", result.CAGR),
			Sub:   fmt.Sprintf("+%.2f%% Total Return", result.TotalReturn),
			Color: "#38bdf8",
		},
		{
			Label: "Max MTM Drawdown",
			Value: fmt.Sprintf("%.2f%%", result.MTMMaxDD),
			Sub:   fmt.Sprintf("Calmar Ratio: %.2f", result.CalmarRatio),
			Color: "#38bdf8",
		},
		{
			Label: "Win Rate & Trades",
			Value: fmt.Sprintf("%.1f%%", result.WinRate),
			Sub:   fmt.Sprintf("%d Trades (%dW / %dL) | %.1fd Avg Hold", result.TotalTrades, result.Wins, result.Losses, result.AvgHoldDays),
			Color: "#f59e0b",
		},
	}

	return ReportView{
		Title:         result.Config.Label,
		Subtitle:      fmt.Sprintf("Signal: %s %d-Day %s | Trade: %s (%.0f%% Allocation + %.1f%% T-Bill Yield)", result.Config.SignalSymbol, result.Config.SignalDays, result.Config.SignalDirection, result.Config.TradeSymbol, result.Config.AllocationPct*100, result.Config.CashYieldAnnual*100),
		KPICards:      kpis,
		Trades:        result.Trades,
		ChartDataJSON: template.JS(string(jsonData)),
	}
}

// FromComboResult builds a ReportView for dual long/short combo strategies.
func FromComboResult(combo dipsim.ComboResult, benchmarkBars []models.BarData) ReportView {
	dates := make([]string, len(combo.DailySeries))
	stratEq := make([]float64, len(combo.DailySeries))
	stratDD := make([]float64, len(combo.DailySeries))
	bmEq := make([]float64, len(combo.DailySeries))
	bmDD := make([]float64, len(combo.DailySeries))

	for i, pt := range combo.DailySeries {
		dates[i] = pt.Date
		stratEq[i] = pt.Equity
		stratDD[i] = pt.Drawdown
	}
	for i, pt := range combo.VOOSeries {
		bmEq[i] = pt.Equity
		bmDD[i] = pt.Drawdown
	}

	chartData := ChartData{
		Dates: dates,
		Series: []Series{
			{
				Label:       fmt.Sprintf("👑 Dual Combo (%s + %s)", combo.LongConfig.TradeSymbol, combo.ShortConfig.TradeSymbol),
				Equity:      stratEq,
				Drawdown:    stratDD,
				Color:       "#10b981",
				BorderWidth: 2.8,
			},
			{
				Label:       "VOO Buy & Hold Benchmark",
				Equity:      bmEq,
				Drawdown:    bmDD,
				Color:       "#a855f7",
				BorderWidth: 1.8,
				IsDash:      true,
			},
		},
	}

	jsonData, _ := json.Marshal(chartData)

	kpis := []KPICard{
		{
			Label: "Combo Final Equity",
			Value: fmt.Sprintf("$%.2f", combo.EndingCap),
			Sub:   fmt.Sprintf("+$%.2f Net Profit", combo.NetProfit),
			Color: "#10b981",
		},
		{
			Label: "Annualized CAGR",
			Value: fmt.Sprintf("%.2f%% / yr", combo.CAGR),
			Sub:   fmt.Sprintf("+%.2f%% Total Return", combo.TotalReturn),
			Color: "#38bdf8",
		},
		{
			Label: "Max MTM Drawdown",
			Value: fmt.Sprintf("%.2f%%", combo.MTMMaxDD),
			Sub:   fmt.Sprintf("Calmar Ratio: %.2f", combo.CalmarRatio),
			Color: "#38bdf8",
		},
		{
			Label: "Trades & Win Rate",
			Value: fmt.Sprintf("%.1f%% WR", combo.WinRate),
			Sub:   fmt.Sprintf("%d Trades (%d Long / %d Short)", combo.TotalTrades, combo.LongTrades, combo.ShortTrades),
			Color: "#f59e0b",
		},
	}

	return ReportView{
		Title:         fmt.Sprintf("👑 All-Weather Dual Combo: %s (Long Dips) + %s (Short Bear Rallies)", combo.LongConfig.TradeSymbol, combo.ShortConfig.TradeSymbol),
		Subtitle:      fmt.Sprintf("Long %s on 3-day drops + Short %s on 3-day rallies in bear markets (65%% Allocation + 4.5%% T-Bills)", combo.LongConfig.TradeSymbol, combo.ShortConfig.TradeSymbol),
		KPICards:      kpis,
		Trades:        combo.Trades,
		ChartDataJSON: template.JS(string(jsonData)),
	}
}

// FromMultiResults builds a comparison report view for multiple parameter tiers or multiple ETFs.
func FromMultiResults(title, subtitle string, results []dipsim.DipSimResult, benchmarkBars []models.BarData, initialCapital float64) ReportView {
	if len(results) == 0 {
		return ReportView{Title: title, Subtitle: subtitle}
	}

	dates := make([]string, len(results[0].DailySeries))
	for i, pt := range results[0].DailySeries {
		dates[i] = pt.Date
	}

	palette := []string{
		"#38bdf8", "#0ea5e9", "#10b981", "#059669", "#f59e0b", "#d97706", "#f43f5e", "#e11d48", "#a855f7", "#6366f1",
	}

	seriesList := make([]Series, 0, len(results)+1)

	// Add benchmark
	if len(benchmarkBars) > 0 {
		bmStart := benchmarkBars[0].Close
		bmPeak := initialCapital
		bmMap := make(map[string]float64, len(benchmarkBars))
		for _, b := range benchmarkBars {
			bmMap[b.Date] = b.Close
		}
		bmEq := make([]float64, len(dates))
		bmDD := make([]float64, len(dates))
		lastEq := initialCapital
		for i, d := range dates {
			if closePrice, ok := bmMap[d]; ok {
				lastEq = (closePrice / bmStart) * initialCapital
			}
			if lastEq > bmPeak {
				bmPeak = lastEq
			}
			dd := (bmPeak - lastEq) / bmPeak * 100.0
			bmEq[i] = lastEq
			bmDD[i] = dd
		}
		seriesList = append(seriesList, Series{
			Label:       "Benchmark Buy & Hold",
			Equity:      bmEq,
			Drawdown:    bmDD,
			Color:       "#94a3b8",
			BorderWidth: 1.8,
			IsDash:      true,
		})
	}

	for idx, r := range results {
		eq := make([]float64, len(dates))
		dd := make([]float64, len(dates))
		for i, pt := range r.DailySeries {
			if i < len(dates) {
				eq[i] = pt.Equity
				dd[i] = pt.Drawdown
			}
		}
		seriesList = append(seriesList, Series{
			Label:       r.Config.Label,
			Equity:      eq,
			Drawdown:    dd,
			Color:       palette[idx%len(palette)],
			BorderWidth: 2.2,
		})
	}

	chartData := ChartData{
		Dates:  dates,
		Series: seriesList,
	}
	jsonData, _ := json.Marshal(chartData)

	tableHeaders := []string{"Rank", "Configuration", "Ending Capital", "5-Yr Net Profit", "CAGR", "Max MTM DD", "Calmar", "Win Rate", "Trades"}
	tableRows := make([][]string, len(results))
	for i, r := range results {
		tableRows[i] = []string{
			fmt.Sprintf("#%d", i+1),
			r.Config.Label,
			fmt.Sprintf("$%.2f", r.EndingCap),
			fmt.Sprintf("+$%.2f", r.NetProfit),
			fmt.Sprintf("%.2f%% / yr", r.CAGR),
			fmt.Sprintf("-%.2f%%", r.MTMMaxDD),
			fmt.Sprintf("⭐ %.2f", r.CalmarRatio),
			fmt.Sprintf("%.1f%%", r.WinRate),
			fmt.Sprintf("%d", r.TotalTrades),
		}
	}

	return ReportView{
		Title:           title,
		Subtitle:        subtitle,
		ComparisonTable: &ComparisonTable{Headers: tableHeaders, Rows: tableRows},
		ChartDataJSON:   template.JS(string(jsonData)),
	}
}
