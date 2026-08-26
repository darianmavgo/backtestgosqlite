package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
)

type BarData struct {
	Date   string  `db:"Date"`
	Open   float64 `db:"open"`
	High   float64 `db:"high"`
	Low    float64 `db:"low"`
	Close  float64 `db:"close"`
	Volume int64   `db:"volume"`
}

type SignalBar struct {
	Date      string  `db:"date"`
	Open      float64 `db:"open"`
	High      float64 `db:"high"`
	Low       float64 `db:"low"`
	Close     float64 `db:"close"`
	PrevClose float64 `db:"prev_close"`
	SMA50     float64 `db:"sma50"`
	SMA200    float64 `db:"sma200"`
	IsDown    int     `db:"is_down"`
}

type ParamSet struct {
	DeclineStreakDays int     // e.g. 2, 3, 4, 5, 6
	HoldDays          int     // e.g. 1, 2, 3, 4, 5, 6, 8, 10, 15
	TakeProfitPct     float64 // e.g. 0.05, 0.08, 0.10, 0.15, 0.20, 0.30, 0.0 (None)
	StopLossPct       float64 // e.g. 0.0 (None), 0.03, 0.05, 0.08, 0.10
	RequireSMA200     bool    // VOO >= 200 SMA
	RequireGreenDay   bool    // First green bounce day
}

type GridResult struct {
	Params           ParamSet
	TotalTrades      int
	Wins             int
	Losses           int
	WinRatePct       float64
	ProfitFactor     float64
	PayoffRatio      float64
	NetProfit        float64
	ReturnPct        float64
	CAGRPct          float64
	MaxDrawdownPct   float64
	CalmarRatio      float64
	SharpeRatio      float64
	AvgReturnPct     float64
	AvgHoldDays      float64
	TargetHits       int
	StopHits         int
	TimeExits        int
}

func main() {
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	signalSymbolFlag := flag.String("signal-symbol", "VOO", "Signal trigger symbol")
	tradeSymbolFlag := flag.String("trade-symbol", "UPRO", "Trade execution symbol (e.g. UPRO, QQQ, TQQQ, SPY)")
	initialCapital := flag.Float64("capital", 100000.0, "Starting portfolio cash ($)")
	tradeAllocation := flag.Float64("trade-cap", 20000.0, "Capital allocated per trade ($)")
	topN := flag.Int("top", 10, "Top N results to display")
	flag.Parse()

	signalSym := strings.ToUpper(*signalSymbolFlag)
	tradeSym := strings.ToUpper(*tradeSymbolFlag)

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB %s: %v", *dbPath, err)
	}
	defer db.Close()

	// 1. Fetch chronological tradeSymbol daily bars
	var tradeBars []BarData
	err = db.Select(&tradeBars, `
		SELECT substr(Date, 1, 10) AS Date, open, high, low, close, volume
		FROM backtest_start
		WHERE symbol = ?
		ORDER BY substr(Date, 1, 10) ASC;
	`, tradeSym)
	if err != nil || len(tradeBars) == 0 {
		log.Fatalf("Failed to load %s bars: %v", tradeSym, err)
	}

	tradeDateToIndex := make(map[string]int)
	for i, b := range tradeBars {
		tradeDateToIndex[b.Date] = i
	}

	// 2. Fetch signalSymbol (VOO) daily bars with moving averages
	var signalBars []SignalBar
	err = db.Select(&signalBars, `
		SELECT 
			substr(Date, 1, 10) AS date,
			open, high, low, close,
			COALESCE(LAG(close, 1) OVER (ORDER BY substr(Date, 1, 10)), close) AS prev_close,
			AVG(close) OVER (ORDER BY substr(Date, 1, 10) ROWS BETWEEN 49 PRECEDING AND CURRENT ROW) AS sma50,
			AVG(close) OVER (ORDER BY substr(Date, 1, 10) ROWS BETWEEN 199 PRECEDING AND CURRENT ROW) AS sma200,
			CASE WHEN close < LAG(close, 1) OVER (ORDER BY substr(Date, 1, 10)) THEN 1 ELSE 0 END AS is_down
		FROM backtest_start
		WHERE symbol = ?
		ORDER BY substr(Date, 1, 10) ASC;
	`, signalSym)
	if err != nil || len(signalBars) == 0 {
		log.Fatalf("Failed to load %s bars: %v", signalSym, err)
	}

	// 3. Define Grid Search Parameter Space
	declineDaysList := []int{2, 3, 4, 5, 6}
	holdDaysList := []int{1, 2, 3, 4, 5, 6, 8, 10, 15}
	takeProfitList := []float64{0.0, 0.05, 0.08, 0.10, 0.15, 0.20, 0.25, 0.30} // 0.0 = None
	stopLossList := []float64{0.0, 0.03, 0.05, 0.07, 0.10}                      // 0.0 = None
	sma200Filters := []bool{false, true}
	greenDayFilters := []bool{false, true}

	totalCombinations := len(declineDaysList) * len(holdDaysList) * len(takeProfitList) * len(stopLossList) * len(sma200Filters) * len(greenDayFilters)

	fmt.Printf("\n========================================================================================\n")
	fmt.Printf("🔬 QUANTITATIVE PARAMETER GRID SEARCH OPTIMIZER\n")
	fmt.Printf("========================================================================================\n")
	fmt.Printf("   • Signal Trigger:     %s (S&P 500 ETF)\n", signalSym)
	fmt.Printf("   • Asset Traded:       %s (3x Leveraged S&P 500 ETF)\n", tradeSym)
	fmt.Printf("   • Parameter Dimensions:\n")
	fmt.Printf("     - Decline Streaks:  %v (%d variants)\n", declineDaysList, len(declineDaysList))
	fmt.Printf("     - Holding Days:     %v (%d variants)\n", holdDaysList, len(holdDaysList))
	fmt.Printf("     - Take Profit %%:    %v (%d variants)\n", takeProfitList, len(takeProfitList))
	fmt.Printf("     - Stop Loss %%:      %v (%d variants)\n", stopLossList, len(stopLossList))
	fmt.Printf("     - SMA200 Gate:      %v\n", sma200Filters)
	fmt.Printf("     - Green Reversal:   %v\n", greenDayFilters)
	fmt.Printf("   • Total Parameter Combinations to Test: %d backtests\n\n", totalCombinations)

	startTime := time.Now()

	// 4. Parallel Multi-Threaded Grid Search Simulation
	var allResults []GridResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	paramChan := make(chan ParamSet, 1000)

	// Worker Pool
	numWorkers := 16
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range paramChan {
				res := runSingleSimulation(signalBars, tradeBars, tradeDateToIndex, p, *initialCapital, *tradeAllocation)
				if res.TotalTrades >= 5 { // Minimum statistical significance
					mu.Lock()
					allResults = append(allResults, res)
					mu.Unlock()
				}
			}
		}()
	}

	// Feed parameters
	for _, d := range declineDaysList {
		for _, h := range holdDaysList {
			for _, tp := range takeProfitList {
				for _, sl := range stopLossList {
					for _, sma := range sma200Filters {
						for _, gr := range greenDayFilters {
							paramChan <- ParamSet{
								DeclineStreakDays: d,
								HoldDays:          h,
								TakeProfitPct:     tp,
								StopLossPct:       sl,
								RequireSMA200:     sma,
								RequireGreenDay:   gr,
							}
						}
					}
				}
			}
		}
	}
	close(paramChan)
	wg.Wait()

	elapsed := time.Since(startTime)
	fmt.Printf("⚡ Completed %d backtests in %v (%.0f tests/sec)\n\n", totalCombinations, elapsed, float64(totalCombinations)/elapsed.Seconds())

	// 5. Store Grid Results into SQLite
	_, _ = db.Exec(`
		DROP TABLE IF EXISTS optimization_grid_results;
		CREATE TABLE optimization_grid_results (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			signal_symbol TEXT,
			trade_symbol TEXT,
			decline_days INTEGER,
			hold_days INTEGER,
			take_profit_pct REAL,
			stop_loss_pct REAL,
			require_sma200 INTEGER,
			require_green_day INTEGER,
			total_trades INTEGER,
			win_rate_pct REAL,
			profit_factor REAL,
			payoff_ratio REAL,
			net_profit REAL,
			total_return_pct REAL,
			cagr_pct REAL,
			max_drawdown_pct REAL,
			calmar_ratio REAL,
			sharpe_ratio REAL,
			avg_trade_return_pct REAL,
			target_hits INTEGER,
			stop_hits INTEGER,
			time_exits INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`
		INSERT INTO optimization_grid_results (
			signal_symbol, trade_symbol, decline_days, hold_days, take_profit_pct, stop_loss_pct,
			require_sma200, require_green_day, total_trades, win_rate_pct, profit_factor, payoff_ratio,
			net_profit, total_return_pct, cagr_pct, max_drawdown_pct, calmar_ratio, sharpe_ratio,
			avg_trade_return_pct, target_hits, stop_hits, time_exits
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)

	for _, r := range allResults {
		smaInt := 0
		if r.Params.RequireSMA200 {
			smaInt = 1
		}
		grInt := 0
		if r.Params.RequireGreenDay {
			grInt = 1
		}
		_, _ = stmt.Exec(
			signalSym, tradeSym, r.Params.DeclineStreakDays, r.Params.HoldDays, r.Params.TakeProfitPct, r.Params.StopLossPct,
			smaInt, grInt, r.TotalTrades, r.WinRatePct, r.ProfitFactor, r.PayoffRatio,
			r.NetProfit, r.ReturnPct, r.CAGRPct, r.MaxDrawdownPct, r.CalmarRatio, r.SharpeRatio,
			r.AvgReturnPct, r.TargetHits, r.StopHits, r.TimeExits,
		)
	}
	_ = stmt.Close()
	_ = tx.Commit()

	// 6. Present Top 10 Configurations by Net Profit & Profit Factor
	// Sort by Net Profit DESC
	sort.Slice(allResults, func(i, j int) bool {
		if allResults[i].NetProfit == allResults[j].NetProfit {
			return allResults[i].ProfitFactor > allResults[j].ProfitFactor
		}
		return allResults[i].NetProfit > allResults[j].NetProfit
	})

	fmt.Printf("🏆 TOP %d OPTIMAL PARAMETER CONFIGURATIONS (RANKED BY NET PROFIT & PROFIT FACTOR):\n", *topN)
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Rank", "Decline", "Hold", "TP %", "SL %", "SMA200", "Green", "Trades", "Win Rate", "Profit Factor", "Net Profit ($)", "Max DD", "CAGR"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for i := 0; i < len(allResults) && i < *topN; i++ {
		r := allResults[i]
		tpStr := "None"
		if r.Params.TakeProfitPct > 0 {
			tpStr = fmt.Sprintf("+%.0f%%", r.Params.TakeProfitPct*100)
		}
		slStr := "None"
		if r.Params.StopLossPct > 0 {
			slStr = fmt.Sprintf("-%.0f%%", r.Params.StopLossPct*100)
		}
		smaStr := "No"
		if r.Params.RequireSMA200 {
			smaStr = "✅ Yes"
		}
		grStr := "No"
		if r.Params.RequireGreenDay {
			grStr = "✅ Yes"
		}

		table.Append([]string{
			fmt.Sprintf("#%d", i+1),
			fmt.Sprintf("%d Days", r.Params.DeclineStreakDays),
			fmt.Sprintf("%d Days", r.Params.HoldDays),
			tpStr,
			slStr,
			smaStr,
			grStr,
			fmt.Sprintf("%d", r.TotalTrades),
			fmt.Sprintf("%.1f%%", r.WinRatePct),
			fmt.Sprintf("%.2f", r.ProfitFactor),
			fmt.Sprintf("$%+.2f", r.NetProfit),
			fmt.Sprintf("%.2f%%", r.MaxDrawdownPct),
			fmt.Sprintf("%.2f%%", r.CAGRPct),
		})
	}
	table.Render()

	// 7. Sort by Highest Win Rate (Minimum 12 Trades)
	var highWinRate []GridResult
	for _, r := range allResults {
		if r.TotalTrades >= 12 {
			highWinRate = append(highWinRate, r)
		}
	}
	sort.Slice(highWinRate, func(i, j int) bool {
		return highWinRate[i].WinRatePct > highWinRate[j].WinRatePct
	})

	fmt.Printf("\n🎯 TOP 5 HIGHEST WIN RATE CONFIGURATIONS (MIN 12 TRADES):\n")
	winTable := tablewriter.NewWriter(os.Stdout)
	winTable.SetHeader([]string{"Rank", "Decline", "Hold", "TP %", "SL %", "SMA200", "Trades", "Win Rate", "Profit Factor", "Net Profit ($)", "Max DD"})
	winTable.SetBorder(true)

	for i := 0; i < len(highWinRate) && i < 5; i++ {
		r := highWinRate[i]
		tpStr := "None"
		if r.Params.TakeProfitPct > 0 {
			tpStr = fmt.Sprintf("+%.0f%%", r.Params.TakeProfitPct*100)
		}
		slStr := "None"
		if r.Params.StopLossPct > 0 {
			slStr = fmt.Sprintf("-%.0f%%", r.Params.StopLossPct*100)
		}
		smaStr := "No"
		if r.Params.RequireSMA200 {
			smaStr = "✅ Yes"
		}

		winTable.Append([]string{
			fmt.Sprintf("#%d", i+1),
			fmt.Sprintf("%d Days", r.Params.DeclineStreakDays),
			fmt.Sprintf("%d Days", r.Params.HoldDays),
			tpStr,
			slStr,
			smaStr,
			fmt.Sprintf("%d", r.TotalTrades),
			fmt.Sprintf("🔥 %.1f%%", r.WinRatePct),
			fmt.Sprintf("%.2f", r.ProfitFactor),
			fmt.Sprintf("$%+.2f", r.NetProfit),
			fmt.Sprintf("%.2f%%", r.MaxDrawdownPct),
		})
	}
	winTable.Render()

	fmt.Printf("\n✨ Optimization complete! All %d grid search combinations saved to SQLite in:\n", len(allResults))
	fmt.Printf("   • Table: `optimization_grid_results` in %s\n\n", *dbPath)
}

func runSingleSimulation(
	signalBars []SignalBar,
	tradeBars []BarData,
	tradeDateToIndex map[string]int,
	params ParamSet,
	initialCapital, tradeAllocation float64,
) GridResult {
	inPositionUntil := -1
	var tradePnLs []float64
	var tradeReturns []float64
	var tradeHoldDays []int

	wins := 0
	losses := 0
	grossGains := 0.0
	grossLosses := 0.0
	targetHits := 0
	stopHits := 0
	timeExits := 0

	for i := params.DeclineStreakDays; i < len(signalBars); i++ {
		// 1. Check Decline Streak of length N
		isStreak := true
		for s := 0; s < params.DeclineStreakDays; s++ {
			if signalBars[i-s].Close >= signalBars[i-s-1].Close {
				isStreak = false
				break
			}
		}
		if !isStreak {
			continue
		}

		// 2. Check 200 SMA Gate
		curr := signalBars[i]
		if params.RequireSMA200 && curr.Close < curr.SMA200 {
			continue
		}

		// 3. Check Green Reversal Day Confirmation
		entryDateIndex := i
		if params.RequireGreenDay {
			confirmed := false
			for look := i; look < i+5 && look < len(signalBars); look++ {
				cand := signalBars[look]
				if cand.Close > cand.Open || cand.Close > cand.PrevClose {
					entryDateIndex = look
					confirmed = true
					break
				}
			}
			if !confirmed {
				continue
			}
		}

		if entryDateIndex >= len(signalBars) {
			continue
		}

		entrySignalBar := signalBars[entryDateIndex]
		tradeIdx, ok := tradeDateToIndex[entrySignalBar.Date]
		if !ok {
			continue
		}

		if tradeIdx <= inPositionUntil {
			continue
		}

		entryTradeBar := tradeBars[tradeIdx]
		entryPrice := entryTradeBar.Close
		shares := int(tradeAllocation / entryPrice)
		if shares <= 0 {
			shares = 1
		}

		targetPrice := 999999.0
		if params.TakeProfitPct > 0 {
			targetPrice = entryPrice * (1.0 + params.TakeProfitPct)
		}

		stopPrice := 0.0
		if params.StopLossPct > 0 {
			stopPrice = entryPrice * (1.0 - params.StopLossPct)
		}

		exitIdx := tradeIdx + params.HoldDays
		if exitIdx >= len(tradeBars) {
			exitIdx = len(tradeBars) - 1
		}

		actualExitIdx := exitIdx
		actualExitPrice := tradeBars[actualExitIdx].Close
		targetHit := false
		stopHit := false

		for d := tradeIdx + 1; d <= exitIdx; d++ {
			dayBar := tradeBars[d]

			// Check Stop Loss First
			if params.StopLossPct > 0 && dayBar.Low <= stopPrice {
				actualExitIdx = d
				actualExitPrice = stopPrice
				if dayBar.Open < stopPrice {
					actualExitPrice = dayBar.Open // gap down
				}
				stopHit = true
				break
			}

			// Check Take Profit
			if params.TakeProfitPct > 0 && dayBar.High >= targetPrice {
				actualExitIdx = d
				actualExitPrice = targetPrice
				if dayBar.Open > targetPrice {
					actualExitPrice = dayBar.Open // gap up
				}
				targetHit = true
				break
			}
		}

		if targetHit {
			targetHits++
		} else if stopHit {
			stopHits++
		} else {
			timeExits++
		}

		holdDuration := actualExitIdx - tradeIdx
		if holdDuration < 1 {
			holdDuration = 1
		}
		pnl := float64(shares) * (actualExitPrice - entryPrice)
		ret := (actualExitPrice - entryPrice) / entryPrice

		if pnl > 0 {
			wins++
			grossGains += pnl
		} else {
			losses++
			grossLosses += math.Abs(pnl)
		}

		tradePnLs = append(tradePnLs, pnl)
		tradeReturns = append(tradeReturns, ret)
		tradeHoldDays = append(tradeHoldDays, holdDuration)
		inPositionUntil = actualExitIdx
	}

	totalTrades := len(tradePnLs)
	if totalTrades == 0 {
		return GridResult{Params: params}
	}

	netProfit := 0.0
	sumRet := 0.0
	sumHold := 0
	for i, p := range tradePnLs {
		netProfit += p
		sumRet += tradeReturns[i]
		sumHold += tradeHoldDays[i]
	}

	winRate := float64(wins) / float64(totalTrades) * 100.0
	pf := 0.0
	if grossLosses > 0 {
		pf = grossGains / grossLosses
	} else if grossGains > 0 {
		pf = 99.99
	}

	avgWin := 0.0
	if wins > 0 {
		avgWin = grossGains / float64(wins)
	}
	avgLoss := 0.0
	if losses > 0 {
		avgLoss = grossLosses / float64(losses)
	}

	payoff := 0.0
	if avgLoss > 0 {
		payoff = avgWin / avgLoss
	}

	// Drawdown calculation
	running := initialCapital
	peak := initialCapital
	maxDD := 0.0
	for _, p := range tradePnLs {
		running += p
		if running > peak {
			peak = running
		}
		dd := (peak - running) / peak
		if dd > maxDD {
			maxDD = dd
		}
	}

	totalRet := netProfit / initialCapital
	years := 5.0
	cagr := (math.Pow(1.0+math.Max(0.0, totalRet), 1.0/years) - 1.0) * 100.0

	calmar := 0.0
	if maxDD > 0 {
		calmar = (cagr / 100.0) / maxDD
	}

	return GridResult{
		Params:         params,
		TotalTrades:    totalTrades,
		Wins:           wins,
		Losses:         losses,
		WinRatePct:     winRate,
		ProfitFactor:   pf,
		PayoffRatio:    payoff,
		NetProfit:      netProfit,
		ReturnPct:      totalRet * 100.0,
		CAGRPct:        cagr,
		MaxDrawdownPct: maxDD * 100.0,
		CalmarRatio:    calmar,
		AvgReturnPct:   (sumRet / float64(totalTrades)) * 100.0,
		AvgHoldDays:    float64(sumHold) / float64(totalTrades),
		TargetHits:     targetHits,
		StopHits:       stopHits,
		TimeExits:      timeExits,
	}
}
