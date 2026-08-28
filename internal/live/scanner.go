// Package live provides real-time market scanning, signal detection, and SQLite state tracking for live execution.
package live

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

// LivePosition represents an open position currently held in the account.
type LivePosition struct {
	ID          int       `db:"id" json:"id"`
	Symbol      string    `db:"symbol" json:"symbol"`
	Direction   string    `db:"direction" json:"direction"` // "LONG_TECL" or "SHORT_SPXU"
	Shares      int       `db:"shares" json:"shares"`
	EntryPrice  float64   `db:"entry_price" json:"entry_price"`
	EntryDate   string    `db:"entry_date" json:"entry_date"`
	TargetPrice float64   `db:"target_price" json:"target_price"`
	StopPrice   float64   `db:"stop_price" json:"stop_price"`
	MaxHoldDays int       `db:"max_hold_days" json:"max_hold_days"`
	MaxExitDate string    `db:"max_exit_date" json:"max_exit_date"`
	Status      string    `db:"status" json:"status"` // "OPEN", "CLOSED_TARGET", "CLOSED_STOP", "CLOSED_TIME"
	CreatedAt   string    `db:"created_at" json:"created_at"`
}

// ScanResult contains the evaluated EOD market condition and generated trading signals.
type ScanResult struct {
	Date          string  `json:"date"`
	VOOClose      float64 `json:"voo_close"`
	VOOSMA200     float64 `json:"voo_sma200"`
	IsBullRegime  bool    `json:"is_bull_regime"`
	StreakDays    int     `json:"streak_days"`
	StreakType    string  `json:"streak_type"` // "DROP", "RALLY", "NONE"
	Action        string  `json:"action"`      // "BUY_TECL", "BUY_SPXU", "HOLD_CASH", "CLOSE_POSITION"
	ActionReason  string  `json:"action_reason"`
	ActiveHolding *LivePosition `json:"active_holding,omitempty"`
}

// InitLiveDB initializes the SQLite live state tracking database schema.
func InitLiveDB(dbPath string) (*sqlx.DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}

	db, err := sqlx.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open live DB %s: %w", dbPath, err)
	}

	schema := `
		CREATE TABLE IF NOT EXISTS live_positions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol TEXT NOT NULL,
			direction TEXT NOT NULL,
			shares INTEGER NOT NULL,
			entry_price REAL NOT NULL,
			entry_date TEXT NOT NULL,
			target_price REAL NOT NULL,
			stop_price REAL NOT NULL,
			max_hold_days INTEGER NOT NULL,
			max_exit_date TEXT NOT NULL,
			status TEXT DEFAULT 'OPEN',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS live_audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			scan_date TEXT NOT NULL,
			voo_close REAL NOT NULL,
			voo_sma200 REAL NOT NULL,
			streak_type TEXT NOT NULL,
			streak_days INTEGER NOT NULL,
			action TEXT NOT NULL,
			action_reason TEXT NOT NULL,
			raw_payload TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS live_trade_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol TEXT NOT NULL,
			direction TEXT NOT NULL,
			entry_date TEXT NOT NULL,
			exit_date TEXT NOT NULL,
			entry_price REAL NOT NULL,
			exit_price REAL NOT NULL,
			shares INTEGER NOT NULL,
			gross_pnl REAL NOT NULL,
			net_pnl REAL NOT NULL,
			return_pct REAL NOT NULL,
			exit_reason TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("failed to create live state schema: %w", err)
	}

	return db, nil
}

// GetOpenPosition retrieves the currently active position if any.
func GetOpenPosition(db *sqlx.DB) (*LivePosition, error) {
	var pos LivePosition
	err := db.Get(&pos, `SELECT id, symbol, direction, shares, entry_price, entry_date, target_price, stop_price, max_hold_days, max_exit_date, status, created_at FROM live_positions WHERE status = 'OPEN' ORDER BY id DESC LIMIT 1;`)
	if err != nil {
		return nil, nil // No active position
	}
	return &pos, nil
}

// RecordEntry records a new position into the live state tracking database.
func RecordEntry(db *sqlx.DB, symbol, direction string, shares int, entryPrice, targetPrice, stopPrice float64, maxHoldDays int, entryDate, maxExitDate string) error {
	query := `
		INSERT INTO live_positions (
			symbol, direction, shares, entry_price, target_price, stop_price, max_hold_days, entry_date, max_exit_date, status
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'OPEN');
	`
	_, err := db.Exec(query, symbol, direction, shares, entryPrice, targetPrice, stopPrice, maxHoldDays, entryDate, maxExitDate)
	return err
}

// RecordExit closes an open position and persists the trade result in live history.
func RecordExit(db *sqlx.DB, positionID int, exitDate string, exitPrice float64, exitReason string) error {
	var pos LivePosition
	err := db.Get(&pos, `SELECT id, symbol, direction, shares, entry_price, entry_date FROM live_positions WHERE id = ?;`, positionID)
	if err != nil {
		return fmt.Errorf("position %d not found: %w", positionID, err)
	}

	pnl := float64(pos.Shares) * (exitPrice - pos.EntryPrice)
	ret := (exitPrice - pos.EntryPrice) / pos.EntryPrice

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`UPDATE live_positions SET status = ? WHERE id = ?;`, exitReason, positionID)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO live_trade_history (
			symbol, direction, entry_date, exit_date, entry_price, exit_price, shares, gross_pnl, net_pnl, return_pct, exit_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, pos.Symbol, pos.Direction, pos.EntryDate, exitDate, pos.EntryPrice, exitPrice, pos.Shares, pnl, pnl, ret, exitReason)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// LogScanAudit saves daily scan evaluation into SQLite audit history.
func LogScanAudit(db *sqlx.DB, res ScanResult) error {
	payload, _ := json.Marshal(res)
	query := `
		INSERT INTO live_audit_log (scan_date, voo_close, voo_sma200, streak_type, streak_days, action, action_reason, raw_payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?);
	`
	_, err := db.Exec(query, res.Date, res.VOOClose, res.VOOSMA200, res.StreakType, res.StreakDays, res.Action, res.ActionReason, string(payload))
	return err
}

// FetchLiveBars downloads the latest daily bars for a ticker from Yahoo Finance.
func FetchLiveBars(symbol string, daysBack int) ([]models.BarData, error) {
	url := fmt.Sprintf("https://query1.finance.yahoo.com/v8/finance/chart/%s?range=2y&interval=1d", symbol)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP error fetching %s: %w", symbol, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Yahoo Finance error for %s (Status: %d)", symbol, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	type YahooResponse struct {
		Chart struct {
			Result []struct {
				Timestamp []int64 `json:"timestamp"`
				Indicators struct {
					Quote []struct {
						Open   []*float64 `json:"open"`
						High   []*float64 `json:"high"`
						Low    []*float64 `json:"low"`
						Close  []*float64 `json:"close"`
						Volume []*int64   `json:"volume"`
					} `json:"quote"`
					Adjclose []struct {
						Adjclose []*float64 `json:"adjclose"`
					} `json:"adjclose"`
				} `json:"indicators"`
			} `json:"result"`
		} `json:"chart"`
	}

	var yResp YahooResponse
	if err := json.Unmarshal(body, &yResp); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w", err)
	}

	if len(yResp.Chart.Result) == 0 || len(yResp.Chart.Result[0].Timestamp) == 0 {
		return nil, fmt.Errorf("no bar data found for %s", symbol)
	}

	res := yResp.Chart.Result[0]
	q := res.Indicators.Quote[0]
	var bars []models.BarData

	for i, ts := range res.Timestamp {
		if q.Open[i] == nil || q.High[i] == nil || q.Low[i] == nil || q.Close[i] == nil {
			continue
		}
		t := time.Unix(ts, 0).UTC()
		dateStr := t.Format("2006-01-02")

		adjClose := *q.Close[i]
		if len(res.Indicators.Adjclose) > 0 && len(res.Indicators.Adjclose[0].Adjclose) > i && res.Indicators.Adjclose[0].Adjclose[i] != nil {
			adjClose = *res.Indicators.Adjclose[0].Adjclose[i]
		}
		vol := int64(0)
		if q.Volume[i] != nil {
			vol = *q.Volume[i]
		}

		bars = append(bars, models.BarData{
			Date:     dateStr,
			Open:     *q.Open[i],
			High:     *q.High[i],
			Low:      *q.Low[i],
			Close:    *q.Close[i],
			AdjClose: adjClose,
			Volume:   vol,
		})
	}

	sort.Slice(bars, func(i, j int) bool {
		return bars[i].Date < bars[j].Date
	})

	if daysBack > 0 && len(bars) > daysBack {
		bars = bars[len(bars)-daysBack:]
	}

	return bars, nil
}

// EvaluateDailyMarket scans the latest market data to determine signals and active position state.
func EvaluateDailyMarket(vooBars []models.BarData, activePos *LivePosition) ScanResult {
	n := len(vooBars)
	if n < 4 {
		return ScanResult{Action: "HOLD_CASH", ActionReason: "Insufficient VOO bar history"}
	}

	latestDate := vooBars[n-1].Date
	latestClose := vooBars[n-1].Close

	// Calculate 200-Day SMA
	smaWindow := 200
	if n < smaWindow {
		smaWindow = n
	}
	sumClose := 0.0
	for i := n - smaWindow; i < n; i++ {
		sumClose += vooBars[i].Close
	}
	sma200 := sumClose / float64(smaWindow)
	isBull := latestClose >= sma200

	// Calculate Streak
	streakDays := 0
	streakType := "NONE"

	if vooBars[n-1].Close < vooBars[n-2].Close {
		streakType = "DROP"
		streakDays = 1
		for i := n - 2; i >= 1; i-- {
			if vooBars[i].Close < vooBars[i-1].Close {
				streakDays++
			} else {
				break
			}
		}
	} else if vooBars[n-1].Close > vooBars[n-2].Close {
		streakType = "RALLY"
		streakDays = 1
		for i := n - 2; i >= 1; i-- {
			if vooBars[i].Close > vooBars[i-1].Close {
				streakDays++
			} else {
				break
			}
		}
	}

	// Base Result
	res := ScanResult{
		Date:          latestDate,
		VOOClose:      latestClose,
		VOOSMA200:     sma200,
		IsBullRegime:  isBull,
		StreakDays:    streakDays,
		StreakType:    streakType,
		ActiveHolding: activePos,
	}

	// 1. If currently holding an active position, evaluate exit criteria
	if activePos != nil && activePos.Status == "OPEN" {
		res.Action = "HOLD_ACTIVE_POSITION"
		res.ActionReason = fmt.Sprintf("Holding %d shares of %s (Entered at $%.2f on %s). Target: $%.2f | Stop: $%.2f | Max Exit: %s",
			activePos.Shares, activePos.Symbol, activePos.EntryPrice, activePos.EntryDate, activePos.TargetPrice, activePos.StopPrice, activePos.MaxExitDate)
		return res
	}

	// 2. Evaluate Entry Criteria:
	// A. Bull Engine: 3-Day Drop on VOO -> Buy TECL
	if streakType == "DROP" && streakDays >= 3 {
		res.Action = "BUY_TECL"
		res.ActionReason = fmt.Sprintf("🟢 BULL SETUP TRIGGERED: VOO has dropped for %d consecutive days. Mean-reversion snapback expected.", streakDays)
		return res
	}

	// B. Bear Engine: 3-Day Rally on VOO AND VOO < 200 SMA -> Buy SPXU
	if streakType == "RALLY" && streakDays >= 3 && !isBull {
		res.Action = "BUY_SPXU"
		res.ActionReason = fmt.Sprintf("🔴 BEAR SETUP TRIGGERED: VOO rallied for %d consecutive days while in Bear Regime (VOO $%.2f < SMA200 $%.2f). Dead-cat bounce fade.", streakDays, latestClose, sma200)
		return res
	}

	// C. No Setup -> Hold Cash
	regimeStr := "Bull Regime (VOO ≥ 200 SMA)"
	if !isBull {
		regimeStr = "Bear Regime (VOO < 200 SMA)"
	}
	res.Action = "HOLD_CASH"
	res.ActionReason = fmt.Sprintf("💵 NO SETUP: VOO is in %s with %d %s days. Cash earns IBKR ~4.8%% APY.", regimeStr, streakDays, strings.ToLower(streakType))
	return res
}
