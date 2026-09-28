package analytics

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

//go:embed report_template.html
var reportTemplateHTML string

type MultiStrategyHTMLData struct {
	Title          string                   `json:"title"`
	Symbol         string                   `json:"symbol"`
	StartDate      string                   `json:"start_date"`
	EndDate        string                   `json:"end_date"`
	TotalDays      int                      `json:"total_days"`
	TotalYears     float64                  `json:"total_years"`
	InitialCap     float64                  `json:"initial_cap"`
	Strategies     []StrategyReportData     `json:"strategies"`
	AllDates       []string                 `json:"all_dates"`
	EquityCurves   map[string][]float64     `json:"equity_curves"`
	DrawdownCurves map[string][]float64     `json:"drawdown_curves"`
}

type StrategyReportData struct {
	ID     string                   `json:"id"`
	Name   string                   `json:"name"`
	Type   string                   `json:"type"`
	Report models.PerformanceReport `json:"report"`
	Trades []models.Trade           `json:"trades"`
}

// GenerateComparisonHTML creates a rich, modern, interactive HTML report with Chart.js charts.
func GenerateComparisonHTML(outputPath string, data MultiStrategyHTMLData) error {
	// Inject date-partitioned subdirectory based on today's date
	dir := filepath.Dir(outputPath)
	base := filepath.Base(outputPath)
	today := time.Now().Format("2006-01-02")

	// Create partitioned directory path
	partitionedDir := filepath.Join(dir, today)
	if partitionedDir != "" {
		_ = os.MkdirAll(partitionedDir, 0755)
	}

	// Reconstruct the full output path
	partitionedOutputPath := filepath.Join(partitionedDir, base)

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON data: %w", err)
	}

	replacements := map[string]string{
		"{{TITLE}}":       data.Title,
		"{{START_DATE}}":  data.StartDate,
		"{{END_DATE}}":    data.EndDate,
		"{{TOTAL_YEARS}}": fmt.Sprintf("%.1f", data.TotalYears),
		"{{TOTAL_DAYS}}":  fmt.Sprintf("%d", data.TotalDays),
		"{{INITIAL_CAP}}": fmt.Sprintf("%.2f", data.InitialCap),
		"{{JSON_DATA}}":   string(jsonBytes),
	}

	outputContent := reportTemplateHTML
	for k, v := range replacements {
		outputContent = strings.ReplaceAll(outputContent, k, v)
	}

	return os.WriteFile(partitionedOutputPath, []byte(outputContent), 0644)
}
