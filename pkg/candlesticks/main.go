package candlesticks

import (
	"fmt"
	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/darianmavgo/backtestgosqlite/pkg/cliutils"
	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

type OHLC struct {
	Symbol string  `db:"symbol"`
	Date   string  `db:"Date"`
	Open   float64 `db:"open"`
	High   float64 `db:"high"`
	Low    float64 `db:"low"`
	Close  float64 `db:"close"`
}

func generateKline(title string, data []OHLC) *charts.Kline {
	kline := charts.NewKLine()
	kline.SetGlobalOptions(
		charts.WithTitleOpts(opts.Title{
			Title: title,
		}),
		charts.WithXAxisOpts(opts.XAxis{
			SplitNumber: 20,
		}),
		charts.WithYAxisOpts(opts.YAxis{
			Scale: opts.Bool(true),
		}),
		charts.WithDataZoomOpts(opts.DataZoom{
			Type:       "slider",
			Start:      0,
			End:        1, // Show roughly 1% of data initially (for a 1-hour window out of 2 months)
			XAxisIndex: []int{0},
		}),
		charts.WithDataZoomOpts(opts.DataZoom{
			Type:       "inside",
			XAxisIndex: []int{0},
		}),
		charts.WithTooltipOpts(opts.Tooltip{
			Show:    opts.Bool(true),
			Trigger: "axis",
			AxisPointer: &opts.AxisPointer{
				Type: "cross",
			},
		}),
	)

	xData := make([]string, 0, len(data))
	yData := make([]opts.KlineData, 0, len(data))

	for _, d := range data {
		xData = append(xData, d.Date)
		// ECharts Kline format: [open, close, lowest, highest]
		yData = append(yData, opts.KlineData{Value: [4]float64{d.Open, d.Close, d.Low, d.High}})
	}

	kline.SetXAxis(xData).AddSeries("kline", yData)

	// Set colors for the candlesticks
	kline.SetSeriesOptions(
		charts.WithItemStyleOpts(opts.ItemStyle{
			Color:        "#00da3c", // Up candle (cyan/green)
			Color0:       "#ec0000", // Down candle (magenta/red)
			BorderColor:  "#008F28",
			BorderColor0: "#8A0000",
		}),
	)

	return kline
}

// Config holds the settings of a candlestick report.
type Config struct {
	DB      string // market DB
	OutPath string // HTML file to write
}

// DefaultConfig returns the CLI defaults.
func DefaultConfig() Config {
	return Config{DB: cliutils.GetDefaultMarketDB(), OutPath: appenv.ReportFile("candlesticks_go.html")}
}

// Main is the CLI entry point.
func Main() {
	out, err := Run(DefaultConfig())
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Successfully generated Go-Echarts visualization at %s", out)
}

// Run renders the 1m candlestick report and returns the written path.
func Run(cfg Config) (string, error) {
	dbPath := cfg.DB
	log.Printf("Connecting to %s...", dbPath)

	db, err := sqlx.Open("sqlite", dbPath)
	if err != nil {
		return "", fmt.Errorf("failed to open db: %w", err)
	}
	defer db.Close()

	query := `
		SELECT symbol, Date, open, high, low, close 
		FROM backtest_start 
		WHERE timeframe = '1m' 
		  AND Date >= '2025-03-01' 
		  AND Date < '2025-05-01'
		ORDER BY Date ASC
	`

	var allData []OHLC
	if err := db.Select(&allData, query); err != nil {
		return "", fmt.Errorf("query failed: %w", err)
	}

	log.Printf("Loaded %d rows.", len(allData))

	// Group by symbol
	grouped := make(map[string][]OHLC)
	for _, row := range allData {
		grouped[row.Symbol] = append(grouped[row.Symbol], row)
	}

	page := components.NewPage()
	page.PageTitle = "Go-Echarts Candlesticks"

	symbols := []string{"VOO", "GLD", "UTEN"}
	for _, sym := range symbols {
		data, ok := grouped[sym]
		if !ok || len(data) == 0 {
			log.Printf("Warning: no data for %s", sym)
			continue
		}

		k := generateKline(fmt.Sprintf("%s (1m Candlesticks)", sym), data)
		page.AddCharts(k)
	}

	outPath := cfg.OutPath
	_ = os.MkdirAll(filepath.Dir(outPath), 0755)
	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer f.Close()

	page.Render(io.MultiWriter(f))
	return outPath, nil
}
