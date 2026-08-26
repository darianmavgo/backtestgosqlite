package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"os"

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
	Volume   int64   `db:"volume"`
}

type VOOStreakRow struct {
	Date       string  `db:"date"`
	Close      float64 `db:"close"`
	PrevClose  float64 `db:"prev_close"`
	IsDown     int     `db:"is_down"`
	StreakID   int     `db:"streak_id"`
	StreakDays int     `db:"streak_days"`
}

func main() {
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "Path to SQLite database")
	initialCapital := flag.Float64("capital", 100000.0, "Starting portfolio cash ($)")
	tradeAllocation := flag.Float64("trade-cap", 20000.0, "Capital allocated per trade ($)")
	targetPct := flag.Float64("target", 1.20, "Take profit target multiplier (1.20 = +20%)")
	holdingDays := flag.Int("hold", 4, "Maximum days to hold position (4 days)")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open DB %s: %v", *dbPath, err)
	}
	defer db.Close()

	// Ensure slice table is calculated
	schemaFile := "sql/strategies/etf_study/calc_study_pipeline.sql"
	_ = storage.ExecuteSQLFile(db, schemaFile)

	// Create tables for this specific backtest
	createTablesSQL := `
		DROP TABLE IF EXISTS voo_qqq_signals;
		CREATE TABLE voo_qqq_signals (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			date TEXT,
			voo_close REAL,
			voo_streak_days INTEGER,
			qqq_entry_price REAL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		DROP TABLE IF EXISTS voo_qqq_trades;
		CREATE TABLE voo_qqq_trades (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			signal_date TEXT,
			entry_date TEXT,
			entry_price REAL,
			exit_date TEXT,
			exit_price REAL,
			exit_reason TEXT,
			hold_days INTEGER,
			shares INTEGER,
			invested_capital REAL,
			gross_pnl REAL,
			net_pnl REAL,
			return_pct REAL,
			is_win INTEGER,
			mae_pct REAL,
			mfe_pct REAL,
			target_hit INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);

		DROP TABLE IF EXISTS voo_qqq_summary;
		CREATE TABLE voo_qqq_summary (
			strategy TEXT PRIMARY KEY,
			total_trades INTEGER,
			winning_trades INTEGER,
			losing_trades INTEGER,
			win_rate_pct TEXT,
			profit_factor REAL,
			payoff_ratio REAL,
			net_profit REAL,
			total_return_pct TEXT,
			cagr_pct TEXT,
			max_drawdown_pct TEXT,
			avg_trade_return_pct TEXT,
			target_20pct_hits INTEGER,
			time_exits INTEGER,
			avg_holding_days REAL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`
	_, err = db.Exec(createTablesSQL)
	if err != nil {
		log.Fatalf("Failed to initialize backtest tables: %v", err)
	}

	// 1. Fetch chronological QQQ daily bars
	var qqqBars []BarData
	err = db.Select(&qqqBars, `
		SELECT substr(Date, 1, 10) AS Date, open, high, low, close, volume
		FROM backtest_start
		WHERE symbol = 'QQQ'
		ORDER BY substr(Date, 1, 10) ASC;
	`)
	if err != nil || len(qqqBars) == 0 {
		log.Fatalf("Failed to load QQQ bars: %v", err)
	}

	// Index QQQ bars by date & array index
	qqqDateToIndex := make(map[string]int)
	for i, b := range qqqBars {
		qqqDateToIndex[b.Date] = i
	}

	// 2. Fetch VOO daily moves and 4-day decline triggers
	// A 4-day decline signal fires on day 4 of any continuous down streak (or each day >= 4)
	var vooStreakDays []VOOStreakRow
	err = db.Select(&vooStreakDays, `
		SELECT 
			date,
			close,
			COALESCE(prev_close, close) AS prev_close,
			COALESCE(is_down, 0) AS is_down,
			COALESCE(streak_id, 0) AS streak_id,
			COALESCE(streak_days, 0) AS streak_days
		FROM is_down_slice
		WHERE symbol = 'VOO'
		ORDER BY date ASC;
	`)
	if err != nil {
		log.Fatalf("Failed to load VOO streak slice: %v", err)
	}

	// Detect distinct 4-day decline entry triggers
	// Specifically: trigger fires when VOO completes 4 consecutive down days
	vooSignalMap := make(map[string]float64)
	for i := 4; i < len(vooStreakDays); i++ {
		row := vooStreakDays[i]
		// Check if past 4 days were strictly consecutive declines
		isConsecutive4 := (vooStreakDays[i].Close < vooStreakDays[i-1].Close &&
			vooStreakDays[i-1].Close < vooStreakDays[i-2].Close &&
			vooStreakDays[i-2].Close < vooStreakDays[i-3].Close &&
			vooStreakDays[i-3].Close < vooStreakDays[i-4].Close)

		if isConsecutive4 {
			vooSignalMap[row.Date] = row.Close
			_, _ = db.Exec(`
				INSERT INTO voo_qqq_signals (date, voo_close, voo_streak_days, qqq_entry_price)
				VALUES (?, ?, ?, (SELECT close FROM backtest_start WHERE symbol='QQQ' AND substr(Date,1,10)=?))
			`, row.Date, row.Close, row.StreakDays, row.Date)
		}
	}

	fmt.Printf("\n🚀 RUNNING CROSS-ASSET STRATEGY BACKTEST:\n")
	fmt.Printf("   • Signal Trigger: VOO 4-Day Consecutive Decline\n")
	fmt.Printf("   • Asset Bought:   QQQ (Nasdaq-100 ETF)\n")
	fmt.Printf("   • Exit Rule:      4 Trading Days in Position OR +20%% Take Profit (Whichever First)\n")
	fmt.Printf("   • Stop Loss:      NONE (Holds through drawdowns)\n")
	fmt.Printf("   • Initial Cash:   $%.2f (Allocation per trade: $%.2f)\n\n", *initialCapital, *tradeAllocation)

	type BacktestTrade struct {
		SignalDate      string
		EntryDate       string
		EntryPrice      float64
		ExitDate        string
		ExitPrice       float64
		ExitReason      string
		HoldDays        int
		Shares          int
		InvestedCapital float64
		GrossPnL        float64
		NetPnL          float64
		ReturnPct       float64
		IsWin           int
		MAE             float64
		MFE             float64
		TargetHit       int
	}

	var executedTrades []BacktestTrade
	inPositionUntil := -1
	targetHits := 0
	timeExits := 0

	for i := 0; i < len(qqqBars); i++ {
		b := qqqBars[i]
		if _, hasSignal := vooSignalMap[b.Date]; !hasSignal {
			continue
		}

		// Prevent overlapping trade on same capital block
		if i <= inPositionUntil {
			continue
		}

		entryPrice := b.Close
		shares := int(*tradeAllocation / entryPrice)
		if shares <= 0 {
			shares = 1
		}
		invested := float64(shares) * entryPrice

		targetPrice := entryPrice * *targetPct

		exitIdx := i + *holdingDays
		if exitIdx >= len(qqqBars) {
			exitIdx = len(qqqBars) - 1
		}

		actualExitIdx := exitIdx
		actualExitPrice := qqqBars[actualExitIdx].Close
		actualExitReason := "TIME_UP_4DAYS"
		targetHit := 0

		minLow := entryPrice
		maxHigh := entryPrice

		// Walk day-by-day forward through the holding period
		for d := i + 1; d <= exitIdx; d++ {
			dayBar := qqqBars[d]
			if dayBar.Low < minLow {
				minLow = dayBar.Low
			}
			if dayBar.High > maxHigh {
				maxHigh = dayBar.High
			}

			// Check +20% Take Profit Target (High >= TargetPrice)
			if dayBar.High >= targetPrice {
				actualExitIdx = d
				actualExitPrice = targetPrice
				if dayBar.Open > targetPrice {
					actualExitPrice = dayBar.Open // gap up
				}
				actualExitReason = "PROFIT_TARGET_20PCT"
				targetHit = 1
				targetHits++
				break
			}
		}

		if targetHit == 0 {
			timeExits++
		}

		holdDuration := actualExitIdx - i
		grossPnL := float64(shares) * (actualExitPrice - entryPrice)
		netPnL := grossPnL
		returnPct := (actualExitPrice - entryPrice) / entryPrice
		maePct := (minLow - entryPrice) / entryPrice
		mfePct := (maxHigh - entryPrice) / entryPrice

		isWin := 0
		if returnPct > 0 {
			isWin = 1
		}

		trade := BacktestTrade{
			SignalDate:      b.Date,
			EntryDate:       b.Date,
			EntryPrice:      entryPrice,
			ExitDate:        qqqBars[actualExitIdx].Date,
			ExitPrice:       actualExitPrice,
			ExitReason:      actualExitReason,
			HoldDays:        holdDuration,
			Shares:          shares,
			InvestedCapital: invested,
			GrossPnL:        grossPnL,
			NetPnL:          netPnL,
			ReturnPct:       returnPct,
			IsWin:           isWin,
			MAE:             maePct,
			MFE:             mfePct,
			TargetHit:       targetHit,
		}
		executedTrades = append(executedTrades, trade)
		inPositionUntil = actualExitIdx

		// Persist trade to SQLite
		_, _ = db.Exec(`
			INSERT INTO voo_qqq_trades (
				signal_date, entry_date, entry_price, exit_date, exit_price, exit_reason,
				hold_days, shares, invested_capital, gross_pnl, net_pnl, return_pct, is_win,
				mae_pct, mfe_pct, target_hit
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, trade.SignalDate, trade.EntryDate, trade.EntryPrice, trade.ExitDate, trade.ExitPrice, trade.ExitReason,
			trade.HoldDays, trade.Shares, trade.InvestedCapital, trade.GrossPnL, trade.NetPnL, trade.ReturnPct, trade.IsWin,
			trade.MAE, trade.MFE, trade.TargetHit)
	}

	// 3. Compute Institutional Portfolio Analytics
	totalTrades := len(executedTrades)
	wins := 0
	losses := 0
	grossGains := 0.0
	grossLosses := 0.0
	netProfit := 0.0
	sumReturn := 0.0
	sumHoldDays := 0
	sumMAE := 0.0
	sumMFE := 0.0

	for _, t := range executedTrades {
		netProfit += t.NetPnL
		sumReturn += t.ReturnPct
		sumHoldDays += t.HoldDays
		sumMAE += t.MAE
		sumMFE += t.MFE
		if t.NetPnL > 0 {
			wins++
			grossGains += t.NetPnL
		} else {
			losses++
			grossLosses += math.Abs(t.NetPnL)
		}
	}

	winRate := 0.0
	if totalTrades > 0 {
		winRate = float64(wins) / float64(totalTrades)
	}

	profitFactor := 0.0
	if grossLosses > 0 {
		profitFactor = grossGains / grossLosses
	} else if grossGains > 0 {
		profitFactor = 99.99
	}

	avgWin := 0.0
	if wins > 0 {
		avgWin = grossGains / float64(wins)
	}
	avgLoss := 0.0
	if losses > 0 {
		avgLoss = grossLosses / float64(losses)
	}

	payoffRatio := 0.0
	if avgLoss > 0 {
		payoffRatio = avgWin / avgLoss
	}

	avgReturn := 0.0
	avgHold := 0.0
	avgMAE := 0.0
	avgMFE := 0.0
	if totalTrades > 0 {
		avgReturn = sumReturn / float64(totalTrades)
		avgHold = float64(sumHoldDays) / float64(totalTrades)
		avgMAE = sumMAE / float64(totalTrades)
		avgMFE = sumMFE / float64(totalTrades)
	}

	// Calculate Equity Curve and Drawdown
	runningEquity := *initialCapital
	peakEquity := *initialCapital
	maxDrawdownDollars := 0.0
	maxDrawdownPct := 0.0

	for _, t := range executedTrades {
		runningEquity += t.NetPnL
		if runningEquity > peakEquity {
			peakEquity = runningEquity
		}
		ddDollars := peakEquity - runningEquity
		ddPct := ddDollars / peakEquity
		if ddDollars > maxDrawdownDollars {
			maxDrawdownDollars = ddDollars
		}
		if ddPct > maxDrawdownPct {
			maxDrawdownPct = ddPct
		}
	}

	totalReturnPct := netProfit / *initialCapital
	years := 5.0
	cagr := math.Pow(1.0+totalReturnPct, 1.0/years) - 1.0

	// Save summary row to SQLite
	_, _ = db.Exec(`
		INSERT OR REPLACE INTO voo_qqq_summary (
			strategy, total_trades, winning_trades, losing_trades, win_rate_pct,
			profit_factor, payoff_ratio, net_profit, total_return_pct, cagr_pct,
			max_drawdown_pct, avg_trade_return_pct, target_20pct_hits, time_exits, avg_holding_days
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "VOO_4D_SIGNAL_BUY_QQQ", totalTrades, wins, losses, fmt.Sprintf("%.2f%%", winRate*100),
		profitFactor, payoffRatio, netProfit, fmt.Sprintf("%.2f%%", totalReturnPct*100),
		fmt.Sprintf("%.2f%%", cagr*100), fmt.Sprintf("%.2f%%", maxDrawdownPct*100),
		fmt.Sprintf("%+.2f%%", avgReturn*100), targetHits, timeExits, avgHold)

	// 4. Print Institutional Performance Tear Sheet
	fmt.Printf("========================================================================================\n")
	fmt.Printf("📊 QUANTITATIVE TEAR SHEET: VOO 4-DAY DECLINE ➔ BUY QQQ (4D HOLD / +20%% TP)\n")
	fmt.Printf("========================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Performance Metric", "Strategy Result", "Benchmark / Context"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	table.Append([]string{"Signal Trigger", "VOO 4-Day Decline", "Buy when VOO drops 4 consecutive days"})
	table.Append([]string{"Asset Traded", "QQQ (Nasdaq-100 ETF)", "Bought at Day 4 Close"})
	table.Append([]string{"Exit Model", "4 Days Hold OR +20% TP", "Whichever barrier triggers first"})
	table.Append([]string{"Stop Loss", "NONE (0.00%)", "Holds through drawdowns"})
	table.Append([]string{"Total Completed Trades", fmt.Sprintf("%d", totalTrades), fmt.Sprintf("%d Wins / %d Losses", wins, losses)})
	table.Append([]string{"Trade Win Rate", fmt.Sprintf("%.2f%%", winRate*100), "Percentage of profitable trades"})
	table.Append([]string{"Profit Factor", fmt.Sprintf("%.2f", profitFactor), "Gross Gains / Gross Losses"})
	table.Append([]string{"Win / Loss Payoff Ratio", fmt.Sprintf("%.2f", payoffRatio), "Average Win $ / Average Loss $"})
	table.Append([]string{"Average Win", fmt.Sprintf("$%.2f", avgWin), "Per winning trade"})
	table.Append([]string{"Average Loss", fmt.Sprintf("$%.2f", avgLoss), "Per losing trade"})
	table.Append([]string{"Average Return / Trade", fmt.Sprintf("%+.2f%%", avgReturn*100), "Mean trade percentage return"})
	table.Append([]string{"Net Realized Profit", fmt.Sprintf("$%+.2f", netProfit), fmt.Sprintf("%.2f%% on $%.0f capital", totalReturnPct*100, *initialCapital)})
	table.Append([]string{"CAGR (Annualized)", fmt.Sprintf("%.2f%%", cagr*100), "Compound Annual Growth Rate"})
	table.Append([]string{"Max Drawdown (MDD)", fmt.Sprintf("%.2f%%", maxDrawdownPct*100), fmt.Sprintf("Peak-to-trough decline: -$%.2f", maxDrawdownDollars)})
	table.Append([]string{"Average MAE (Drawdown)", fmt.Sprintf("%.2f%%", avgMAE*100), "Max Adverse Excursion during trade"})
	table.Append([]string{"Average MFE (Runup)", fmt.Sprintf("+%.2f%%", avgMFE*100), "Max Favorable Excursion during trade"})
	table.Append([]string{"Average Holding Period", fmt.Sprintf("%.1f days", avgHold), fmt.Sprintf("Time Exits: %d | 20%% TP Hits: %d", timeExits, targetHits)})

	table.Render()

	// 5. Print All Executed Trades
	fmt.Printf("\n📜 FULL EXECUTED TRADE LOG (FROM SQLITE `voo_qqq_trades`):\n")
	tradeTable := tablewriter.NewWriter(os.Stdout)
	tradeTable.SetHeader([]string{"#", "VOO Signal Date", "QQQ Entry", "Exit Date", "Exit Price", "Hold", "Reason", "Return %", "Net PnL", "Win/Loss"})
	tradeTable.SetBorder(true)

	for idx, t := range executedTrades {
		winStr := "🟢 WIN"
		if t.IsWin == 0 {
			winStr = "🔴 LOSS"
		}
		tradeTable.Append([]string{
			fmt.Sprintf("%d", idx+1),
			t.SignalDate,
			fmt.Sprintf("$%.2f", t.EntryPrice),
			t.ExitDate,
			fmt.Sprintf("$%.2f", t.ExitPrice),
			fmt.Sprintf("%dd", t.HoldDays),
			t.ExitReason,
			fmt.Sprintf("%+.2f%%", t.ReturnPct*100),
			fmt.Sprintf("$%+.2f", t.NetPnL),
			winStr,
		})
	}
	tradeTable.Render()

	fmt.Printf("\n✨ Backtest complete! All results saved to SQLite in: %s\n", *dbPath)
	fmt.Printf("   • Signal Log:     `voo_qqq_signals` (%d signals recorded)\n", len(vooSignalMap))
	fmt.Printf("   • Executed Trades: `voo_qqq_trades` (%d trades recorded)\n", len(executedTrades))
	fmt.Printf("   • Summary Stats:  `voo_qqq_summary`\n\n")
}
