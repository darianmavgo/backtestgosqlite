package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/analytics"
	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/transaction_calc"
)

func main() {
	var inPath string
	flag.StringVar(&inPath, "in", "", "Path to IBKR CSV file")
	flag.Parse()

	if inPath == "" {
		log.Fatal("Must provide -in path/to/csv")
	}

	trades, eqPoints, initialCash, err := transaction_calc.ParseIBKR(inPath)
	if err != nil {
		log.Fatalf("Failed to parse CSV: %v", err)
	}

	if len(trades) == 0 {
		log.Println("No matched trades found in CSV.")
	}

	report := analytics.CalculatePerformanceMetrics(initialCash, trades, eqPoints)

	baseName := strings.TrimSuffix(filepath.Base(inPath), filepath.Ext(inPath))
	outPath := appenv.ReportFile(baseName + ".html")

	var allDates []string
	var eqCurve []float64
	var ddCurve []float64

	highWaterMark := initialCash
	for _, p := range eqPoints {
		allDates = append(allDates, p.Date)
		eqCurve = append(eqCurve, p.TotalEquity)
		if p.TotalEquity > highWaterMark {
			highWaterMark = p.TotalEquity
		}
		dd := 0.0
		if highWaterMark > 0 {
			dd = (p.TotalEquity - highWaterMark) / highWaterMark
		}
		ddCurve = append(ddCurve, dd)
	}

	startDate := ""
	endDate := ""
	if len(allDates) > 0 {
		startDate = allDates[0]
		endDate = allDates[len(allDates)-1]
	}

	htmlData := analytics.MultiStrategyHTMLData{
		Title:       fmt.Sprintf("IBKR Live Account: %s", baseName),
		GeneratedAt: time.Now().Format(time.RFC3339),
		Symbol:      "PORTFOLIO",
		StartDate:   startDate,
		EndDate:     endDate,
		TotalDays:   len(allDates),
		TotalYears:  float64(len(allDates)) / 252.0,
		InitialCap:  initialCash,
		Strategies: []analytics.StrategyReportData{
			{
				ID:     "live",
				Name:   "Live IBKR Trades",
				Type:   "live",
				Report: report,
				Trades: trades,
			},
		},
		AllDates: allDates,
		EquityCurves: map[string][]float64{
			"live": eqCurve,
		},
		DrawdownCurves: map[string][]float64{
			"live": ddCurve,
		},
	}

	today := time.Now().Format("2006-01-02")
	actualOutPath := filepath.Join(filepath.Dir(outPath), today, filepath.Base(outPath))

	if err := analytics.GenerateComparisonHTML(outPath, htmlData); err != nil {
		log.Fatalf("Failed to generate HTML: %v", err)
	}

	fmt.Printf("Generated report at: %s\n", actualOutPath)
}
