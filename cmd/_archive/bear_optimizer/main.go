package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
)

type BarData struct {
	Date     string  `db:"Date"`
	Open     float64 `db:"open"`
	High     float64 `db:"high"`
	Low      float64 `db:"low"`
	Close    float64 `db:"close"`
	AdjClose float64 `db:"Adj Close"`
	Volume   int64   `db:"volume"`
}

type SignalBar struct {
	Date   string  `db:"date"`
	Close  float64 `db:"close"`
	SMA200 float64 `db:"sma200"`
	SMA50  float64 `db:"sma50"`
}

type BearOptResult struct {
	ETF           string
	RallyDays     int
	HoldDays      int
	TakeProfitPct float64
	StopLossPct   float64
	RegimeFilter  string
	EndingCap     float64
	NetProfit     float64
	TotalReturn   float64
	CAGR          float64
	MTMMaxDD      float64
	CalmarRatio   float64
	TotalTrades   int
	WinRate       float64
	ProfitFactor  float64
	AvgHoldDays   float64
}

func main() {
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	initialCapital := flag.Float64("capital", 100000.0, "Starting cash ($)")
	allocRatio := flag.Float64("alloc", 0.65, "Dynamic allocation ratio (65%)")
	cashYieldAnn := flag.Float64("yield", 0.045, "Annual cash yield on idle reserves (4.5%)")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	// Ensure bear optimization results table exists
	_, _ = db.Exec(`
		CREATE TABLE IF NOT EXISTS bear_optimization_grid_results (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			symbol TEXT,
			rally_days INTEGER,
			hold_days INTEGER,
			take_profit_pct REAL,
			stop_loss_pct REAL,
			regime_filter TEXT,
			allocation_pct REAL,
			total_trades INTEGER,
			win_rate_pct REAL,
			profit_factor REAL,
			net_profit REAL,
			cagr_pct REAL,
			max_drawdown_pct REAL,
			calmar_ratio REAL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)

	// 1. Fetch VOO bars and Moving Averages
	var vooBars []BarData
	_ = db.Select(&vooBars, `SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = 'VOO' ORDER BY substr(Date, 1, 10) ASC;`)

	var sigBars []SignalBar
	_ = db.Select(&sigBars, `
		SELECT 
			substr(Date, 1, 10) AS date,
			close,
			AVG(close) OVER (ORDER BY substr(Date, 1, 10) ROWS BETWEEN 199 PRECEDING AND CURRENT ROW) AS sma200,
			AVG(close) OVER (ORDER BY substr(Date, 1, 10) ROWS BETWEEN 49 PRECEDING AND CURRENT ROW) AS sma50
		FROM backtest_start
		WHERE symbol = 'VOO'
		ORDER BY substr(Date, 1, 10) ASC;
	`)

	sigMap := make(map[string]SignalBar)
	for _, s := range sigBars {
		sigMap[s.Date] = s
	}

	// 2. Fetch Inverse ETFs: SQQQ, SPXU, SOXS
	symbols := []string{"SQQQ", "SPXU", "SOXS"}
	etfBarsMap := make(map[string][]BarData)

	for _, sym := range symbols {
		var bars []BarData
		_ = db.Select(&bars, fmt.Sprintf(`SELECT substr(Date, 1, 10) AS Date, open, high, low, close, "Adj Close", volume FROM backtest_start WHERE symbol = '%s' ORDER BY substr(Date, 1, 10) ASC;`, sym))
		etfBarsMap[sym] = bars
	}

	// 3. Grid Search Space
	rallyDaysList := []int{2, 3, 4, 5}
	holdDaysList := []int{1, 2, 3, 4, 5, 6, 8, 10, 12, 15}
	tpList := []float64{0.0, 0.02, 0.03, 0.04, 0.05, 0.06, 0.08, 0.10, 0.12, 0.15, 0.20}
	slList := []float64{0.0, 0.03, 0.05, 0.07, 0.10, 0.15}
	regimeFilters := []string{"VOO < SMA200", "VOO < SMA50", "All Regimes"}

	totalPermutations := len(symbols) * len(rallyDaysList) * len(holdDaysList) * len(tpList) * len(slList) * len(regimeFilters)

	fmt.Printf("\n=======================================================================================================================\n")
	fmt.Printf("🐻 QUANTITATIVE PARAMETER GRID SEARCH OPTIMIZER: BEAR SHORT STRATEGIES\n")
	fmt.Printf("   Testing %d Permutations in Parallel across SQQQ, SPXU, and SOXS...\n", totalPermutations)
	fmt.Printf("=======================================================================================================================\n\n")

	startTime := time.Now()

	type Task struct {
		symbol string
		rDays  int
		hDays  int
		tp     float64
		sl     float64
		filter string
	}

	taskChan := make(chan Task, 5000)
	var results []BearOptResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	numWorkers := 16
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range taskChan {
				bars := etfBarsMap[t.symbol]
				res := runBearSim(t.symbol, t.rDays, t.hDays, t.tp, t.sl, t.filter, vooBars, bars, sigMap, *initialCapital, *allocRatio, *cashYieldAnn)
				if res.TotalTrades >= 5 {
					mu.Lock()
					results = append(results, res)
					mu.Unlock()
				}
			}
		}()
	}

	for _, sym := range symbols {
		for _, r := range rallyDaysList {
			for _, h := range holdDaysList {
				for _, tp := range tpList {
					for _, sl := range slList {
						for _, filter := range regimeFilters {
							taskChan <- Task{symbol: sym, rDays: r, hDays: h, tp: tp, sl: sl, filter: filter}
						}
					}
				}
			}
		}
	}
	close(taskChan)
	wg.Wait()

	fmt.Printf("⚡ Successfully simulated %d valid parameter configurations in %v!\n\n", len(results), time.Since(startTime))

	// Save to DB in batch
	tx, _ := db.Beginx()
	insertStmt, _ := tx.Preparex(`
		INSERT INTO bear_optimization_grid_results (
			symbol, rally_days, hold_days, take_profit_pct, stop_loss_pct, regime_filter,
			allocation_pct, total_trades, win_rate_pct, profit_factor, net_profit, cagr_pct,
			max_drawdown_pct, calmar_ratio
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
	`)
	for _, r := range results {
		_, _ = insertStmt.Exec(
			r.ETF, r.RallyDays, r.HoldDays, r.TakeProfitPct, r.StopLossPct, r.RegimeFilter,
			*allocRatio, r.TotalTrades, r.WinRate, r.ProfitFactor, r.NetProfit, r.CAGR,
			r.MTMMaxDD, r.CalmarRatio,
		)
	}
	insertStmt.Close()
	_ = tx.Commit()

	// 1. Top 10 by Net Realized Profit
	sort.Slice(results, func(i, j int) bool {
		return results[i].NetProfit > results[j].NetProfit
	})

	fmt.Printf("🏆 TOP 10 BEAR STRATEGIES RANKED BY NET PROFIT ($100k Base, 65%% Allocation + 4.5%% T-Bills):\n")
	table1 := tablewriter.NewWriter(os.Stdout)
	table1.SetHeader([]string{"Rank", "ETF", "Rally", "Hold", "Take Profit", "Stop Loss", "Regime Gate", "5-Yr Profit ($)", "Ending Cap", "CAGR", "Max MTM DD", "Win Rate", "Trades", "Calmar"})
	table1.SetBorder(true)
	table1.SetAutoWrapText(false)

	for i := 0; i < len(results) && i < 10; i++ {
		r := results[i]
		tpStr := "None"
		if r.TakeProfitPct > 0 {
			tpStr = fmt.Sprintf("+%.0f%%", r.TakeProfitPct*100)
		}
		slStr := "None"
		if r.StopLossPct > 0 {
			slStr = fmt.Sprintf("-%.0f%%", r.StopLossPct*100)
		}

		table1.Append([]string{
			fmt.Sprintf("#%d", i+1),
			r.ETF,
			fmt.Sprintf("%d Days", r.RallyDays),
			fmt.Sprintf("%d Days", r.HoldDays),
			tpStr,
			slStr,
			r.RegimeFilter,
			fmt.Sprintf("+$%.2f", r.NetProfit),
			fmt.Sprintf("💰 $%.2f", r.EndingCap),
			fmt.Sprintf("%.2f%%/yr", r.CAGR),
			fmt.Sprintf("🔴 %.2f%%", r.MTMMaxDD),
			fmt.Sprintf("🔥 %.1f%%", r.WinRate),
			fmt.Sprintf("%d", r.TotalTrades),
			fmt.Sprintf("⭐ %.2f", r.CalmarRatio),
		})
	}
	table1.Render()

	// 2. Top 10 by Calmar Ratio (Risk-Adjusted Return)
	sort.Slice(results, func(i, j int) bool {
		return results[i].CalmarRatio > results[j].CalmarRatio
	})

	fmt.Printf("\n⭐ TOP 10 BEAR STRATEGIES RANKED BY CALMAR RATIO (CAGR / MAX DRAWDOWN):\n")
	table2 := tablewriter.NewWriter(os.Stdout)
	table2.SetHeader([]string{"Rank", "ETF", "Rally", "Hold", "Take Profit", "Stop Loss", "Regime Gate", "🔥 Calmar", "Max MTM DD", "CAGR", "Win Rate", "5-Yr Profit ($)", "Trades"})
	table2.SetBorder(true)
	table2.SetAutoWrapText(false)

	for i := 0; i < len(results) && i < 10; i++ {
		r := results[i]
		tpStr := "None"
		if r.TakeProfitPct > 0 {
			tpStr = fmt.Sprintf("+%.0f%%", r.TakeProfitPct*100)
		}
		slStr := "None"
		if r.StopLossPct > 0 {
			slStr = fmt.Sprintf("-%.0f%%", r.StopLossPct*100)
		}

		table2.Append([]string{
			fmt.Sprintf("#%d", i+1),
			r.ETF,
			fmt.Sprintf("%d Days", r.RallyDays),
			fmt.Sprintf("%d Days", r.HoldDays),
			tpStr,
			slStr,
			r.RegimeFilter,
			fmt.Sprintf("⭐ %.2f", r.CalmarRatio),
			fmt.Sprintf("🛡️ %.2f%%", r.MTMMaxDD),
			fmt.Sprintf("%.2f%%/yr", r.CAGR),
			fmt.Sprintf("🔥 %.1f%%", r.WinRate),
			fmt.Sprintf("+$%.2f", r.NetProfit),
			fmt.Sprintf("%d", r.TotalTrades),
		})
	}
	table2.Render()
	fmt.Println()
}

func runBearSim(symbol string, rallyDays, holdDays int, tpPct, slPct float64, filter string, vooBars, tradeBars []BarData, sigMap map[string]SignalBar, startCap, allocPct, cashYieldAnn float64) BearOptResult {
	cash := startCap
	mtmPeak := startCap
	maxMtmDD := 0.0

	dailyYieldRate := math.Pow(1.0+cashYieldAnn, 1.0/252.0) - 1.0

	inPosUntil := -1
	activeShares := 0
	activeEntryPrice := 0.0
	inPos := false

	wins := 0
	losses := 0
	grossGains := 0.0
	grossLosses := 0.0
	totalTrades := 0
	totalHoldDays := 0

	tradeMap := make(map[string]BarData)
	for _, b := range tradeBars {
		tradeMap[b.Date] = b
	}

	for i := rallyDays; i < len(vooBars); i++ {
		date := vooBars[i].Date
		bar, hasBar := tradeMap[date]
		if !hasBar {
			continue
		}

		if cash > 0 && !inPos {
			cash += cash * dailyYieldRate
		}

		// Check consecutive UP streak on VOO
		isRally := true
		for s := 0; s < rallyDays; s++ {
			if vooBars[i-s].Close <= vooBars[i-s-1].Close {
				isRally = false
				break
			}
		}

		// Apply Regime Filter
		if isRally {
			if filter == "VOO < SMA200" && vooBars[i].Close >= sigMap[date].SMA200 {
				isRally = false
			} else if filter == "VOO < SMA50" && vooBars[i].Close >= sigMap[date].SMA50 {
				isRally = false
			}
		}

		// Entry
		if isRally && i > inPosUntil && !inPos {
			entryPrice := bar.Close
			posCap := cash * allocPct
			shares := int(posCap / entryPrice)

			if shares > 0 {
				activeShares = shares
				activeEntryPrice = entryPrice
				cash -= float64(activeShares) * activeEntryPrice
				inPos = true

				exitIdx := i + holdDays
				if exitIdx >= len(vooBars) {
					exitIdx = len(vooBars) - 1
				}

				actualExitIdx := exitIdx
				actualExitPrice := 0.0
				if bExit, ok := tradeMap[vooBars[actualExitIdx].Date]; ok {
					actualExitPrice = bExit.Close
				} else {
					actualExitPrice = activeEntryPrice
				}

				targetPrice := 999999.0
				if tpPct > 0 {
					targetPrice = activeEntryPrice * (1.0 + tpPct)
				}
				stopPrice := 0.0
				if slPct > 0 {
					stopPrice = activeEntryPrice * (1.0 - slPct)
				}

				for d := i + 1; d <= exitIdx; d++ {
					dDate := vooBars[d].Date
					dBar, ok := tradeMap[dDate]
					if !ok {
						continue
					}

					// Stop Loss hit
					if slPct > 0 && dBar.Low <= stopPrice {
						actualExitIdx = d
						actualExitPrice = stopPrice
						if dBar.Open < stopPrice {
							actualExitPrice = dBar.Open
						}
						break
					}

					// Take Profit hit
					if tpPct > 0 && dBar.High >= targetPrice {
						actualExitIdx = d
						actualExitPrice = targetPrice
						if dBar.Open > targetPrice {
							actualExitPrice = dBar.Open
						}
						break
					}
				}

				holdDur := actualExitIdx - i
				if holdDur < 1 {
					holdDur = 1
				}
				totalHoldDays += holdDur

				pnl := float64(activeShares) * (actualExitPrice - activeEntryPrice)
				if pnl > 0 {
					wins++
					grossGains += pnl
				} else {
					losses++
					grossLosses += math.Abs(pnl)
				}
				totalTrades++
				inPosUntil = actualExitIdx
			}
		}

		// Exit
		if inPos && i == inPosUntil {
			actualExitPrice := bar.Close
			targetPrice := activeEntryPrice * (1.0 + tpPct)
			stopPrice := activeEntryPrice * (1.0 - slPct)

			if slPct > 0 && bar.Low <= stopPrice {
				actualExitPrice = stopPrice
			} else if tpPct > 0 && bar.High >= targetPrice {
				actualExitPrice = targetPrice
			}

			realized := float64(activeShares) * actualExitPrice
			cash += realized
			inPos = false
			activeShares = 0
		}

		posVal := 0.0
		if inPos {
			posVal = float64(activeShares) * bar.Close
		}
		mtmEquity := cash + posVal
		if mtmEquity > mtmPeak {
			mtmPeak = mtmEquity
		}
		dd := (mtmPeak - mtmEquity) / mtmPeak * 100.0
		if dd > maxMtmDD {
			maxMtmDD = dd
		}
	}

	posVal := 0.0
	if inPos {
		lastBar := tradeBars[len(tradeBars)-1]
		posVal = float64(activeShares) * lastBar.Close
	}
	finalEquity := cash + posVal
	pnl := finalEquity - startCap
	totRet := pnl / startCap * 100.0
	cagr := (math.Pow(math.Max(0.01, finalEquity/startCap), 1.0/5.0) - 1.0) * 100.0

	winRate := 0.0
	if totalTrades > 0 {
		winRate = float64(wins) / float64(totalTrades) * 100.0
	}
	pf := 0.0
	if grossLosses > 0 {
		pf = grossGains / grossLosses
	} else if grossGains > 0 {
		pf = 99.0
	}
	avgHold := 0.0
	if totalTrades > 0 {
		avgHold = float64(totalHoldDays) / float64(totalTrades)
	}
	calmar := 0.0
	if maxMtmDD > 0 {
		calmar = cagr / maxMtmDD
	}

	return BearOptResult{
		ETF:           symbol,
		RallyDays:     rallyDays,
		HoldDays:      holdDays,
		TakeProfitPct: tpPct,
		StopLossPct:   slPct,
		RegimeFilter:  filter,
		EndingCap:     finalEquity,
		NetProfit:     pnl,
		TotalReturn:   totRet,
		CAGR:          cagr,
		MTMMaxDD:      maxMtmDD,
		CalmarRatio:   calmar,
		TotalTrades:   totalTrades,
		WinRate:       winRate,
		ProfitFactor:  pf,
		AvgHoldDays:   avgHold,
	}
}
