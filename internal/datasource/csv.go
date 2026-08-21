package datasource

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// CSVColumnMapping specifies the zero-indexed column positions in a CSV file.
type CSVColumnMapping struct {
	Date     int
	Open     int
	High     int
	Low      int
	Close    int
	AdjClose int
	Volume   int
	Symbol   int
}

// CSVDataSource loads historical bar data from CSV files.
type CSVDataSource struct {
	FilePath string
	Mapping  *CSVColumnMapping
}

// NewCSVDataSource creates a CSV data source with optional custom column mappings.
func NewCSVDataSource(filePath string, mapping *CSVColumnMapping) *CSVDataSource {
	return &CSVDataSource{
		FilePath: filePath,
		Mapping:  mapping,
	}
}

func (c *CSVDataSource) Name() string {
	return "csv"
}

// Fetch reads bars from the configured CSV file or directory.
func (c *CSVDataSource) Fetch(ctx context.Context, req FetchRequest) ([]models.Bar, error) {
	fileInfo, err := os.Stat(c.FilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to access CSV path %s: %w", c.FilePath, err)
	}

	if fileInfo.IsDir() {
		// Try finding symbol specific CSV in directory
		targetFile := filepath.Join(c.FilePath, fmt.Sprintf("%s.csv", req.Symbol))
		if _, err := os.Stat(targetFile); err == nil {
			return c.readSingleCSV(targetFile, req)
		}
		targetFileLower := filepath.Join(c.FilePath, fmt.Sprintf("%s.csv", strings.ToLower(req.Symbol)))
		if _, err := os.Stat(targetFileLower); err == nil {
			return c.readSingleCSV(targetFileLower, req)
		}
		return nil, fmt.Errorf("symbol file %s.csv not found in %s", req.Symbol, c.FilePath)
	}

	return c.readSingleCSV(c.FilePath, req)
}

func (c *CSVDataSource) readSingleCSV(filePath string, req FetchRequest) ([]models.Bar, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening csv %s: %w", filePath, err)
	}
	defer file.Close()

	return ParseCSVReader(file, req.Symbol, c.Mapping)
}

// ParseCSVReader parses OHLCV bars from any io.Reader.
func ParseCSVReader(r io.Reader, defaultSymbol string, customMapping *CSVColumnMapping) ([]models.Bar, error) {
	csvReader := csv.NewReader(r)
	csvReader.TrimLeadingSpace = true

	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read csv data: %w", err)
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("csv contains insufficient data (rows: %d)", len(records))
	}

	mapping := customMapping
	if mapping == nil {
		mapping = detectCSVMapping(records[0])
	}

	var bars []models.Bar
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) == 0 || (len(row) == 1 && strings.TrimSpace(row[0]) == "") {
			continue
		}

		sym := defaultSymbol
		if mapping.Symbol >= 0 && mapping.Symbol < len(row) && strings.TrimSpace(row[mapping.Symbol]) != "" {
			sym = strings.TrimSpace(row[mapping.Symbol])
		}

		if sym == "" {
			sym = "UNKNOWN"
		}

		if mapping.Date < 0 || mapping.Date >= len(row) {
			continue
		}

		rawDate := strings.TrimSpace(row[mapping.Date])
		parsedDt, normalizedDate := parseDateString(rawDate)

		o := parseFloatt(row, mapping.Open)
		h := parseFloatt(row, mapping.High)
		l := parseFloatt(row, mapping.Low)
		c := parseFloatt(row, mapping.Close)
		adj := parseFloatt(row, mapping.AdjClose)
		if adj == 0 {
			adj = c
		}
		v := parseIntt(row, mapping.Volume)

		bars = append(bars, models.Bar{
			Idx:        len(bars) + 1,
			Symbol:     sym,
			Date:       normalizedDate,
			Timeframe:  "1d",
			AssetClass: "equity",
			Open:       o,
			High:       h,
			Low:        l,
			Close:      c,
			AdjClose:   adj,
			Volume:     v,
			ParsedDt:   parsedDt,
		})
	}

	return bars, nil
}

func detectCSVMapping(header []string) *CSVColumnMapping {
	m := &CSVColumnMapping{
		Date:     -1,
		Open:     -1,
		High:     -1,
		Low:      -1,
		Close:    -1,
		AdjClose: -1,
		Volume:   -1,
		Symbol:   -1,
	}

	for i, col := range header {
		clean := strings.ToLower(strings.TrimSpace(col))
		clean = strings.ReplaceAll(clean, "_", "")
		clean = strings.ReplaceAll(clean, " ", "")

		switch {
		case clean == "date" || clean == "timestamp" || clean == "datetime" || clean == "time":
			if m.Date == -1 {
				m.Date = i
			}
		case clean == "open":
			m.Open = i
		case clean == "high":
			m.High = i
		case clean == "low":
			m.Low = i
		case clean == "close":
			m.Close = i
		case clean == "adjclose" || clean == "adjustedclose":
			m.AdjClose = i
		case clean == "volume" || clean == "vol":
			m.Volume = i
		case clean == "symbol" || clean == "ticker":
			m.Symbol = i
		}
	}

	// Fallback to standard 0:Date, 1:Open, 2:High, 3:Low, 4:Close, 5:Volume if headers not detected
	if m.Date == -1 && len(header) >= 6 {
		m.Date = 0
		m.Open = 1
		m.High = 2
		m.Low = 3
		m.Close = 4
		m.Volume = 5
	}

	return m
}

func parseDateString(raw string) (time.Time, string) {
	formats := []string{
		"2006-01-02 15:04:05",
		"2006-01-02",
		"2006/01/02",
		"01/02/2006",
		"02-01-2006",
		time.RFC3339,
	}

	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t, t.Format("2006-01-02")
		}
	}

	if len(raw) >= 10 {
		return time.Time{}, raw[:10]
	}
	return time.Time{}, raw
}

func parseFloatt(row []string, idx int) float64 {
	if idx < 0 || idx >= len(row) {
		return 0
	}
	val, err := strconv.ParseFloat(strings.TrimSpace(row[idx]), 64)
	if err != nil {
		return 0
	}
	return val
}

func parseIntt(row []string, idx int) int64 {
	if idx < 0 || idx >= len(row) {
		return 0
	}
	val, err := strconv.ParseInt(strings.TrimSpace(row[idx]), 10, 64)
	if err != nil {
		// Try float conversion then cast to int64
		if fVal, fErr := strconv.ParseFloat(strings.TrimSpace(row[idx]), 64); fErr == nil {
			return int64(fVal)
		}
		return 0
	}
	return val
}
