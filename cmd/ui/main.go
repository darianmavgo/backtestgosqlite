package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed web/index.html
var indexHTML []byte

var (
	masterDb    = "data/wc_master_backtest.db"
	settingsDb  = "data/settings.db"
	wcDir       = "."
	runningTask string
	taskOutput  string
	taskMutex   sync.Mutex
)

type SummaryRow struct {
	Symbol        string  `db:"symbol" json:"symbol"`
	Entries       int     `db:"entries" json:"entries"`
	SumWin3       int     `db:"sum_win3" json:"sum_win3"`
	SumWin5       int     `db:"sum_win5" json:"sum_win5"`
	Wins2010d     int     `db:"wins_20_10d" json:"wins_20_10d"`
	Win2010dRate  float64 `db:"win20_10d_rate" json:"win20_10d_rate"`
	AvgMaxGain10d float64 `db:"avg_max_gain_10d" json:"avg_max_gain_10d"`
	MaxMaxGain10d float64 `db:"max_max_gain_10d" json:"max_max_gain_10d"`
	AvgHighGapPct float64 `db:"avg_highgappct" json:"avg_highgappct"`
	Category      string  `json:"category"`
}

type SignalDetail struct {
	Idx        int     `db:"idx" json:"idx"`
	Symbol     string  `db:"symbol" json:"symbol"`
	Date       string  `db:"date" json:"date"`
	Open       float64 `db:"open" json:"open"`
	High       float64 `db:"high" json:"high"`
	Low        float64 `db:"low" json:"low"`
	Close      float64 `db:"close" json:"close"`
	Volume     int64   `db:"volume" json:"volume"`
	BuyLimit   float64 `db:"buylimit" json:"buylimit"`
	Entry      int     `db:"entry" json:"entry"`
	MinLow4    float64 `db:"minlow4" json:"minlow4"`
	MaxHigh4   float64 `db:"maxhigh4" json:"maxhigh4"`
	MaxHigh10  float64 `db:"maxhigh10" json:"maxhigh10"`
	Win3       int     `db:"Win3" json:"win3"`
	Win5       int     `db:"Win5" json:"win5"`
	Win2010d   int     `db:"Win20_10d" json:"win20_10d"`
	MaxGain10d float64 `db:"max_gain_10d" json:"max_gain_10d"`
	HighGapPct float64 `db:"highgappct" json:"highgappct"`
	LowGapPct  float64 `db:"lowgappct" json:"lowgappct"`
}

type Bar struct {
	Date   string  `db:"Date" json:"date"`
	Open   float64 `db:"open" json:"open"`
	High   float64 `db:"high" json:"high"`
	Low    float64 `db:"low" json:"low"`
	Close  float64 `db:"close" json:"close"`
	Volume int64   `db:"volume" json:"volume"`
}

type DatabaseInfo struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	SizeMB      string `json:"size_mb"`
	BarsCount   int    `json:"bars_count"`
	SymbolCount int    `json:"symbol_count"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
}

func getCategory(db *sqlx.DB, symbol string) string {
	var count int
	_ = db.Get(&count, "SELECT count(*) FROM leveraged_etf WHERE symbol = ?", symbol)
	if count > 0 {
		return "Leveraged ETF"
	}
	return "Stock / Equity"
}

func handleSummary(w http.ResponseWriter, r *http.Request) {
	db, err := sqlx.Open("sqlite3", masterDb)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer db.Close()

	settingsDB, _ := sqlx.Open("sqlite3", settingsDb)
	if settingsDB != nil {
		defer settingsDB.Close()
	}

	var rows []SummaryRow
	query := `
		SELECT symbol, entries, sum_win3, sum_win5, wins_20_10d, win20_10d_rate,
		       COALESCE(avg_max_gain_10d, 0) as avg_max_gain_10d,
		       COALESCE(max_max_gain_10d, 0) as max_max_gain_10d,
		       COALESCE(avg_highgappct, 0) as avg_highgappct
		FROM wc_summary
		WHERE entries > 0
		ORDER BY win20_10d_rate DESC, entries DESC;
	`
	err = db.Select(&rows, query)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	for i := range rows {
		if settingsDB != nil {
			rows[i].Category = getCategory(settingsDB, rows[i].Symbol)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rows)
}

func handleSymbolDetails(w http.ResponseWriter, r *http.Request) {
	symbol := r.URL.Query().Get("symbol")
	if symbol == "" {
		http.Error(w, "missing symbol param", 400)
		return
	}

	db, err := sqlx.Open("sqlite3", masterDb)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer db.Close()

	var bars []Bar
	_ = db.Select(&bars, `
		SELECT substr(Date, 1, 10) as Date, open, high, low, close, volume
		FROM backtest_start
		WHERE symbol = ?
		ORDER BY Date ASC;
	`, symbol)

	var signals []SignalDetail
	_ = db.Select(&signals, `
		SELECT idx, symbol, date, open, high, low, close, volume, buylimit, entry,
		       minlow4, maxhigh4, maxhigh10, Win3, Win5, Win20_10d, max_gain_10d,
		       highgappct, lowgappct
		FROM wc_backtest_details
		WHERE symbol = ? AND entry = 1
		ORDER BY date ASC;
	`, symbol)

	resp := map[string]interface{}{
		"symbol":  symbol,
		"bars":    bars,
		"signals": signals,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleDatabases(w http.ResponseWriter, r *http.Request) {
	dbs := []string{
		masterDb,
		settingsDb,
		filepath.Join(wcDir, "leveraged_backtest.db"),
		filepath.Join(wcDir, "test_history.db"),
	}

	var res []DatabaseInfo
	for _, p := range dbs {
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		db, err := sqlx.Open("sqlite3", p)
		if err != nil {
			continue
		}

		dbInfo := DatabaseInfo{
			Name:   filepath.Base(p),
			Path:   p,
			SizeMB: fmt.Sprintf("%.2f MB", float64(info.Size())/(1024*1024)),
		}

		_ = db.Get(&dbInfo.BarsCount, "SELECT count(*) FROM backtest_start;")
		_ = db.Get(&dbInfo.SymbolCount, "SELECT count(distinct symbol) FROM backtest_start;")
		_ = db.Get(&dbInfo.StartDate, "SELECT min(substr(Date, 1, 10)) FROM backtest_start;")
		_ = db.Get(&dbInfo.EndDate, "SELECT max(substr(Date, 1, 10)) FROM backtest_start;")

		db.Close()
		res = append(res, dbInfo)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 405)
		return
	}

	var req struct {
		Database string `json:"database"`
		Query    string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	dbPath := masterDb
	if req.Database == "settings.db" {
		dbPath = settingsDb
	}

	qTrim := strings.TrimSpace(strings.ToUpper(req.Query))
	if !strings.HasPrefix(qTrim, "SELECT") && !strings.HasPrefix(qTrim, "WITH") && !strings.HasPrefix(qTrim, "EXPLAIN") && !strings.HasPrefix(qTrim, "PRAGMA") {
		http.Error(w, "Only SELECT / read-only queries are permitted via the UI console", 400)
		return
	}

	db, err := sqlx.Open("sqlite3", dbPath)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer db.Close()

	rows, err := db.Queryx(req.Query)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var results []map[string]interface{}
	for rows.Next() {
		m := make(map[string]interface{})
		_ = rows.MapScan(m)
		for k, v := range m {
			if b, ok := v.([]byte); ok {
				m[k] = string(b)
			}
		}
		results = append(results, m)
	}

	resp := map[string]interface{}{
		"columns": cols,
		"rows":    results,
		"count":   len(results),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleTriggerBacktest(w http.ResponseWriter, r *http.Request) {
	taskMutex.Lock()
	if runningTask != "" {
		taskMutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "already_running", "message": "A backtest task is already in progress."})
		return
	}
	runningTask = "backtest"
	taskOutput = "Starting backtest...\n"
	taskMutex.Unlock()

	go func() {
		cmd := exec.Command("/Users/darianhickman/Documents/wc_2022/backtest_all.sh")
		out, err := cmd.CombinedOutput()
		taskMutex.Lock()
		runningTask = ""
		if err != nil {
			taskOutput = fmt.Sprintf("Error: %v\nOutput:\n%s", err, string(out))
		} else {
			taskOutput = string(out)
		}
		taskMutex.Unlock()
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "started", "message": "Backtest started successfully in background."})
}

func handleTaskStatus(w http.ResponseWriter, r *http.Request) {
	taskMutex.Lock()
	defer taskMutex.Unlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"running": runningTask != "",
		"task":    runningTask,
		"output":  taskOutput,
	})
}

func main() {
	port := flag.String("port", "8085", "Port to bind UI server")
	flag.Parse()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/ui" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if len(indexHTML) > 0 {
			w.Write(indexHTML)
		} else {
			// Fallback to disk read
			content, _ := os.ReadFile(filepath.Join(wcDir, "web", "index.html"))
			w.Write(content)
		}
	})

	http.HandleFunc("/api/summary", handleSummary)
	http.HandleFunc("/api/symbol", handleSymbolDetails)
	http.HandleFunc("/api/databases", handleDatabases)
	http.HandleFunc("/api/query", handleQuery)
	http.HandleFunc("/api/backtest/run", handleTriggerBacktest)
	http.HandleFunc("/api/backtest/status", handleTaskStatus)

	fmt.Printf("🚀 Whitings Creek Trading Strategy & Cache Review UI running at: http://localhost:%s\n", *port)
	log.Fatal(http.ListenAndServe(":"+*port, nil))
}
