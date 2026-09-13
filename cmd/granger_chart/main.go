package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
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

func generateBar(title string, data []GrangerRow) *charts.Bar {
	bar := charts.NewBar()
	bar.SetGlobalOptions(
		charts.WithTitleOpts(opts.Title{
			Title:    title,
			Subtitle: "Green bars (P-Value < 0.05) indicate significant predictive causality",
		}),
		charts.WithYAxisOpts(opts.YAxis{
			Name: "P-Value",
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
		
		color := "#1f77b4" // Blue (not significant)
		if d.PValue < 0.05 {
			color = "#2ca02c" // Green (significant)
		}

		yData = append(yData, opts.BarData{
			Value: d.PValue,
			ItemStyle: &opts.ItemStyle{
				Color: color,
			},
		})
	}

	bar.SetXAxis(xData).AddSeries("P-Value", yData)

	return bar
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

	log.Printf("Loaded %d rows.", len(allData))

	grouped := make(map[string][]GrangerRow)
	for _, row := range allData {
		grouped[row.Target] = append(grouped[row.Target], row)
	}

	page := components.NewPage()
	page.PageTitle = "Granger Causality (Go-Echarts)"

	for target, data := range grouped {
		title := fmt.Sprintf("Does VOO Lead %s?", target)
		bar := generateBar(title, data)
		page.AddCharts(bar)
	}

	outPath := filepath.Join("reports", "granger_causality_go.html")
	f, err := os.Create(outPath)
	if err != nil {
		log.Fatalf("Failed to create output file: %v", err)
	}
	defer f.Close()

	page.Render(io.MultiWriter(f))
	log.Printf("Successfully generated Go-Echarts visualization at %s", outPath)
}
