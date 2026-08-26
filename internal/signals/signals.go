// Package signals provides SQL-driven entry signal detection for dip/rally backtesting.
//
// Signals are defined as .sql files in sql/signals/ that return a single column `signal_date`.
// The Go code loads the SQL, binds parameters (e.g. :signal_symbol), and executes it against
// the SQLite database to produce a set of trigger dates.
package signals

import (
	"fmt"
	"os"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/jmoiron/sqlx"
)

// SignalResult holds the output of a signal detection query.
type SignalResult struct {
	Dates    map[string]bool // Set of dates where the signal fired
	DateList []string        // Ordered list of signal dates
}

// LoadFromSQL reads a SQL signal file, binds parameters, executes it, and returns the signal dates.
// The SQL file must return a column named `signal_date`.
//
// Parameters are passed as key-value pairs and substituted for :key placeholders in the SQL.
// Example: LoadFromSQL(db, "sql/signals/consecutive_drop.sql", map[string]string{"signal_symbol": "VOO"})
func LoadFromSQL(db *sqlx.DB, sqlFilePath string, params map[string]string) (*SignalResult, error) {
	content, err := os.ReadFile(sqlFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read signal SQL file %s: %w", sqlFilePath, err)
	}

	query := string(content)

	// Substitute named parameters (:key -> 'value')
	for k, v := range params {
		placeholder := ":" + k
		query = strings.ReplaceAll(query, placeholder, "'"+v+"'")
	}

	type row struct {
		SignalDate string `db:"signal_date"`
	}
	var rows []row
	if err := db.Select(&rows, query); err != nil {
		return nil, fmt.Errorf("failed to execute signal SQL %s: %w", sqlFilePath, err)
	}

	result := &SignalResult{
		Dates: make(map[string]bool, len(rows)),
	}
	for _, r := range rows {
		result.Dates[r.SignalDate] = true
		result.DateList = append(result.DateList, r.SignalDate)
	}
	return result, nil
}

// DetectConsecutiveDrops detects N consecutive down closes on the given bars.
// This is the pure-Go fallback when no SQL file is used.
func DetectConsecutiveDrops(bars []models.BarData, consecutiveDays int) map[string]bool {
	signals := make(map[string]bool)
	for i := consecutiveDays; i < len(bars); i++ {
		allDown := true
		for s := 0; s < consecutiveDays; s++ {
			if bars[i-s].Close >= bars[i-s-1].Close {
				allDown = false
				break
			}
		}
		if allDown {
			signals[bars[i].Date] = true
		}
	}
	return signals
}

// DetectConsecutiveRallies detects N consecutive up closes on the given bars.
func DetectConsecutiveRallies(bars []models.BarData, consecutiveDays int) map[string]bool {
	signals := make(map[string]bool)
	for i := consecutiveDays; i < len(bars); i++ {
		allUp := true
		for s := 0; s < consecutiveDays; s++ {
			if bars[i-s].Close <= bars[i-s-1].Close {
				allUp = false
				break
			}
		}
		if allUp {
			signals[bars[i].Date] = true
		}
	}
	return signals
}

// FilterByRegime filters signal dates to only those matching the given regime condition.
// Supported regimes: "VOO < SMA200", "VOO < SMA50", "VOO >= SMA200", "All Regimes".
func FilterByRegime(signalDates map[string]bool, sigBars []models.SignalBar, regime string) map[string]bool {
	if regime == "" || regime == "All Regimes" {
		return signalDates
	}

	sigMap := make(map[string]models.SignalBar, len(sigBars))
	for _, s := range sigBars {
		sigMap[s.Date] = s
	}

	filtered := make(map[string]bool)
	for date := range signalDates {
		sb, ok := sigMap[date]
		if !ok {
			continue
		}
		switch regime {
		case "VOO < SMA200":
			if sb.Close < sb.SMA200 {
				filtered[date] = true
			}
		case "VOO < SMA50":
			if sb.Close < sb.SMA50 {
				filtered[date] = true
			}
		case "VOO >= SMA200":
			if sb.Close >= sb.SMA200 {
				filtered[date] = true
			}
		default:
			filtered[date] = true
		}
	}
	return filtered
}
