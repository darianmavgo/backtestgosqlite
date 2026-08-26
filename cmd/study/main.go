package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/datasource"
	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	"github.com/darianmavgo/backtestgosqlite/internal/strategy"
	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
)

type TradeRecord struct {
	Symbol          string
	EntryIdx        int
	EntryDate       string
	EntryPrice      float64
	ExitIdx         int
	ExitDate        string
	ExitPrice       float64
	HoldDays        int
	Shares          int
	InvestedCapital float64
	GrossPnL        float64
	NetPnL          float64
	ReturnPct       float64
	IsWin           int
	MAE             float64
	MFE             float64
}

func simulateHoldingPeriod(
	barsBySymbol map[string][]models.Bar,
	symbols []string,
	holdingDays int,
	capitalPerTrade float64,
) ([]TradeRecord, int) {
	var allTrades []TradeRecord
	totalSignals := 0

	for _, sym := range symbols {
		bars, ok := barsBySymbol[sym]
		if !ok || len(bars) < 30 {
			continue
		}

		rsi5 := strategy.CalcRSI(bars, 5)
		sma20 := strategy.CalcSMA(bars, 20)

		inPositionUntil := -1

		for i := 20; i < len(bars); i++ {
			b := bars[i]
			r := rsi5[i]
			sma := sma20[i]

			// Buy Signal: Short-term pullback dip (RSI(5) < 32 & Close < SMA20, or consecutive down days with oversold RSI)
			isSignal := (r < 32.0 && b.Close < sma) || (i >= 2 && b.Close < bars[i-1].Close && bars[i-1].Close < bars[i-2].Close && r < 35.0)

			if !isSignal {
				continue
			}

			totalSignals++

			if i <= inPositionUntil {
				continue // already in position
			}

			exitIdx := i + holdingDays
			if exitIdx >= len(bars) {
				continue
			}

			entryPrice := b.Close
			exitPrice := bars[exitIdx].Close
			shares := int(capitalPerTrade / entryPrice)
			if shares <= 0 {
				shares = 1
			}
			invested := float64(shares) * entryPrice

			grossPnL := float64(shares) * (exitPrice - entryPrice)
			netPnL := grossPnL
			returnPct := (exitPrice - entryPrice) / entryPrice

			minLow := entryPrice
			maxHigh := entryPrice
			for d := i + 1; d <= exitIdx; d++ {
				if bars[d].Low < minLow {
					minLow = bars[d].Low
				}
				if bars[d].High > maxHigh {
					maxHigh = bars[d].High
				}
			}
			maePct := (minLow - entryPrice) / entryPrice
			mfePct := (maxHigh - entryPrice) / entryPrice

			isWin := 0
			if returnPct > 0 {
				isWin = 1
			}

			trade := TradeRecord{
				Symbol:          sym,
				EntryIdx:        b.Idx,
				EntryDate:       b.Date,
				EntryPrice:      entryPrice,
				ExitIdx:         bars[exitIdx].Idx,
				ExitDate:        bars[exitIdx].Date,
				ExitPrice:       exitPrice,
				HoldDays:        holdingDays,
				Shares:          shares,
				InvestedCapital: invested,
				GrossPnL:        grossPnL,
				NetPnL:          netPnL,
				ReturnPct:       returnPct,
				IsWin:           isWin,
				MAE:             maePct,
				MFE:             mfePct,
			}
			allTrades = append(allTrades, trade)
			inPositionUntil = exitIdx
		}
	}
	return allTrades, totalSignals
}

type modelsBar = datasource.FetchRequest // alias if needed, or using models.Bar

func main() {
	dbPath := flag.String("db", "data/sp500_etfs_study.db", "SQLite database path")
	symbolsFlag := flag.String("symbols", "SPY,IVV,VOO,RSP,SSO", "Comma-separated list of top S&P 500 ETFs")
	years := flag.Int("years", 5, "Number of years of historical data")
	holdingDays := flag.Int("days", 4, "Number of days in position before exit (e.g. 4)")
	capitalPerTrade := flag.Float64("capital", 10000.0, "Capital allocated per trade ($)")
	downloadFlag := flag.Bool("download", false, "Force re-download historical data from Yahoo")
	flag.Parse()

	db, err := storage.OpenSQLite(*dbPath)
	if err != nil {
		log.Fatalf("Failed to open SQLite database %s: %v", *dbPath, err)
	}
	defer db.Close()

	if err := storage.EnsureBarTable(db, "backtest_start"); err != nil {
		log.Fatalf("Failed to ensure backtest_start table: %v", err)
	}
	schemaFile := "sql/strategies/etf_study/calc_study_pipeline.sql"
	if err := storage.ExecuteSQLFile(db, schemaFile); err != nil {
		log.Printf("Note on executing schema: %v", err)
	}

	symbols := strings.Split(*symbolsFlag, ",")
	for i := range symbols {
		symbols[i] = strings.TrimSpace(strings.ToUpper(symbols[i]))
	}

	// 1. Check or download historical data
	ctx := context.Background()
	client := &http.Client{Timeout: 15 * time.Second}
	yahoo := datasource.NewYahooDataSource(client)

	for _, sym := range symbols {
		var count int
		_ = db.Get(&count, "SELECT COUNT(*) FROM backtest_start WHERE symbol = ?", sym)
		if count == 0 || *downloadFlag {
			fmt.Printf("📥 Fetching %d years of historical data for %s from Yahoo Finance...\n", *years, sym)
			start := time.Now().UTC().AddDate(-*years, 0, 0)
			end := time.Now().UTC()
			bars, err := yahoo.Fetch(ctx, datasource.FetchRequest{
				Symbol:    sym,
				StartDate: start,
				EndDate:   end,
				Timeframe: "1d",
			})
			if err != nil || len(bars) == 0 {
				log.Printf("⚠️ Warning: could not download %s: %v", sym, err)
				continue
			}
			if err := storage.UpsertBars(db, "backtest_start", bars); err != nil {
				log.Printf("⚠️ Error upserting %s: %v", sym, err)
			} else {
				fmt.Printf("✅ Saved %d historical bars for %s\n", len(bars), sym)
			}
		}
	}

	// 2. Fetch bars by symbol
	barsBySymbol, _, err := storage.FetchBars(db, "backtest_start", symbols, "", "")
	if err != nil {
		log.Fatalf("Failed to fetch bars: %v", err)
	}

	// Clean out previous study runs
	_, _ = db.Exec("DELETE FROM study_buy_signals;")
	_, _ = db.Exec("DELETE FROM study_trades;")
	_, _ = db.Exec("DELETE FROM study_win_rates;")
	_, _ = db.Exec("DELETE FROM study_horizon_comparison;")

	fmt.Printf("\n🔍 EXECUTING 4-DAY POSITION EXIT STUDY ON TOP 5 S&P 500 ETFs...\n")
	fmt.Printf("═══════════════════════════════════════════════════════════════════════════════════════\n")

	// 3. Primary Run with target holdingDays (4 days)
	allTrades, totalSignals := simulateHoldingPeriod(barsBySymbol, symbols, *holdingDays, *capitalPerTrade)

	// Save signals and trades into SQLite
	for _, sym := range symbols {
		bars := barsBySymbol[sym]
		if len(bars) < 30 {
			continue
		}
		rsi5 := strategy.CalcRSI(bars, 5)
		sma20 := strategy.CalcSMA(bars, 20)

		for i := 20; i < len(bars); i++ {
			b := bars[i]
			r := rsi5[i]
			sma := sma20[i]
			isSignal := (r < 32.0 && b.Close < sma) || (i >= 2 && b.Close < bars[i-1].Close && bars[i-1].Close < bars[i-2].Close && r < 35.0)
			if isSignal {
				_, _ = db.Exec(`
					INSERT INTO study_buy_signals (idx, symbol, date, open, high, low, close, volume, rsi5, sma20, signal_type)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`, b.Idx, sym, b.Date, b.Open, b.High, b.Low, b.Close, b.Volume, r, sma, "RSI5_OVERSOLD_PULLBACK")
			}
		}
	}

	for _, t := range allTrades {
		_, _ = db.Exec(`
			INSERT INTO study_trades (
				symbol, entry_idx, entry_date, entry_price, exit_idx, exit_date, exit_price,
				hold_days, shares, invested_capital, gross_pnl, net_pnl, return_pct, is_win,
				mae_pct, mfe_pct, exit_reason
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, t.Symbol, t.EntryIdx, t.EntryDate, t.EntryPrice, t.ExitIdx, t.ExitDate, t.ExitPrice,
			t.HoldDays, t.Shares, t.InvestedCapital, t.GrossPnL, t.NetPnL, t.ReturnPct, t.IsWin,
			t.MAE, t.MFE, fmt.Sprintf("%d_DAY_TIME_EXIT", *holdingDays))
	}

	// 4. Compute Win Rates and Stats by Symbol
	type WinRateRow struct {
		Symbol        string
		TotalTrades   int
		Wins          int
		Losses        int
		WinRate       float64
		ProfitFactor  float64
		PayoffRatio   float64
		AvgWinAmount  float64
		AvgLossAmount float64
		AvgReturnPct  float64
		NetProfit     float64
		TotalInvested float64
		AvgMAE        float64
		AvgMFE        float64
	}

	var winRateSummary []WinRateRow
	tradesBySym := make(map[string][]TradeRecord)
	for _, t := range allTrades {
		tradesBySym[t.Symbol] = append(tradesBySym[t.Symbol], t)
	}

	for _, sym := range symbols {
		tList := tradesBySym[sym]
		if len(tList) == 0 {
			continue
		}

		wins := 0
		losses := 0
		grossGains := 0.0
		grossLosses := 0.0
		netProfit := 0.0
		totalInvested := 0.0
		sumReturn := 0.0
		sumMAE := 0.0
		sumMFE := 0.0

		for _, t := range tList {
			totalInvested += t.InvestedCapital
			netProfit += t.NetPnL
			sumReturn += t.ReturnPct
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

		totalTrades := len(tList)
		winRate := float64(wins) / float64(totalTrades)
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

		avgReturn := sumReturn / float64(totalTrades)
		avgMAE := sumMAE / float64(totalTrades)
		avgMFE := sumMFE / float64(totalTrades)

		row := WinRateRow{
			Symbol:        sym,
			TotalTrades:   totalTrades,
			Wins:          wins,
			Losses:        losses,
			WinRate:       winRate,
			ProfitFactor:  profitFactor,
			PayoffRatio:   payoffRatio,
			AvgWinAmount:  avgWin,
			AvgLossAmount: avgLoss,
			AvgReturnPct:  avgReturn,
			NetProfit:     netProfit,
			TotalInvested: totalInvested,
			AvgMAE:        avgMAE,
			AvgMFE:        avgMFE,
		}
		winRateSummary = append(winRateSummary, row)

		// Save to SQLite study_win_rates
		_, _ = db.Exec(`
			INSERT OR REPLACE INTO study_win_rates (
				symbol, total_trades, winning_trades, losing_trades, win_rate, win_rate_pct,
				profit_factor, payoff_ratio, avg_win_amount, avg_loss_amount, avg_trade_return_pct,
				net_profit, total_invested, avg_mae_pct, avg_mfe_pct
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, row.Symbol, row.TotalTrades, row.Wins, row.Losses, row.WinRate, fmt.Sprintf("%.2f%%", row.WinRate*100),
			row.ProfitFactor, row.PayoffRatio, row.AvgWinAmount, row.AvgLossAmount, row.AvgReturnPct,
			row.NetProfit, row.TotalInvested, row.AvgMAE, row.AvgMFE)
	}

	// 5. Render Tables
	fmt.Printf("\n📊 S&P 500 TOP 5 ETFs - 4-DAY POSITION EXIT WIN RATE STUDY TABLE\n")
	fmt.Printf("Holding Window: Exactly %d Trading Days (Unconditional Time Exit)\n\n", *holdingDays)

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{
		"ETF Symbol", "Asset Description", "Trades", "Wins / Losses", "Win Rate %", "Profit Factor", "Avg Return/Trade", "Net Profit ($)", "Avg MFE", "Avg MAE",
	})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	descriptions := map[string]string{
		"SPY":  "SPDR S&P 500 ETF Trust",
		"IVV":  "iShares Core S&P 500 ETF",
		"VOO":  "Vanguard S&P 500 ETF",
		"RSP":  "Invesco Equal-Weight S&P 500",
		"QQQ":  "Invesco QQQ Trust (Nasdaq-100)",
		"SSO":  "ProShares Ultra S&P 500 2x",
		"UPRO": "ProShares UltraPro S&P 500 3x",
		"SPYG": "SPDR S&P 500 Growth ETF",
		"SPYV": "SPDR S&P 500 Value ETF",
	}

	totTrades := 0
	totWins := 0
	totLosses := 0
	totNetProfit := 0.0
	totInvested := 0.0

	for _, r := range winRateSummary {
		totTrades += r.TotalTrades
		totWins += r.Wins
		totLosses += r.Losses
		totNetProfit += r.NetProfit
		totInvested += r.TotalInvested

		desc := descriptions[r.Symbol]
		if desc == "" {
			desc = "S&P 500 ETF"
		}

		table.Append([]string{
			r.Symbol,
			desc,
			fmt.Sprintf("%d", r.TotalTrades),
			fmt.Sprintf("%d / %d", r.Wins, r.Losses),
			fmt.Sprintf("%.2f%%", r.WinRate*100),
			fmt.Sprintf("%.2f", r.ProfitFactor),
			fmt.Sprintf("%+.2f%%", r.AvgReturnPct*100),
			fmt.Sprintf("$%+.2f", r.NetProfit),
			fmt.Sprintf("+%.2f%%", r.AvgMFE*100),
			fmt.Sprintf("%.2f%%", r.AvgMAE*100),
		})
	}

	universeWinRate := 0.0
	if totTrades > 0 {
		universeWinRate = float64(totWins) / float64(totTrades)
	}

	table.SetFooter([]string{
		"PORTFOLIO TOTAL",
		"Top 5 S&P 500 Universe",
		fmt.Sprintf("%d", totTrades),
		fmt.Sprintf("%d / %d", totWins, totLosses),
		fmt.Sprintf("%.2f%%", universeWinRate*100),
		"—",
		"—",
		fmt.Sprintf("$%+.2f", totNetProfit),
		"—",
		"—",
	})

	table.Render()

	// Write Combined Portfolio row into study_win_rates table
	_, _ = db.Exec(`
		INSERT OR REPLACE INTO study_win_rates (
			symbol, total_trades, winning_trades, losing_trades, win_rate, win_rate_pct,
			profit_factor, payoff_ratio, avg_win_amount, avg_loss_amount, avg_trade_return_pct,
			net_profit, total_invested, avg_mae_pct, avg_mfe_pct
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, "ALL_COMBINED", totTrades, totWins, totLosses, universeWinRate, fmt.Sprintf("%.2f%%", universeWinRate*100),
		0, 0, 0, 0, 0, totNetProfit, totInvested, 0, 0)

	// 6. Horizon Sensitivity Study (1d, 2d, 3d, 4d, 5d, 7d, 10d)
	fmt.Printf("\n⏱️ HOLDING HORIZON SENSITIVITY COMPARISON (1 to 10 DAYS HOLD):\n")
	horizonTable := tablewriter.NewWriter(os.Stdout)
	horizonTable.SetHeader([]string{"Hold Horizon", "Total Trades", "Wins / Losses", "Win Rate %", "Profit Factor", "Avg Return/Trade", "Total Net Profit", "Avg MFE", "Avg MAE"})
	horizonTable.SetBorder(true)

	horizons := []int{1, 2, 3, 4, 5, 7, 10}
	for _, h := range horizons {
		hTrades, _ := simulateHoldingPeriod(barsBySymbol, symbols, h, *capitalPerTrade)
		hWins, hLosses := 0, 0
		hGains, hLossTotal, hNetProfit, sumRet, sumMFE, sumMAE := 0.0, 0.0, 0.0, 0.0, 0.0, 0.0
		for _, t := range hTrades {
			hNetProfit += t.NetPnL
			sumRet += t.ReturnPct
			sumMFE += t.MFE
			sumMAE += t.MAE
			if t.NetPnL > 0 {
				hWins++
				hGains += t.NetPnL
			} else {
				hLosses++
				hLossTotal += math.Abs(t.NetPnL)
			}
		}
		hWinRate := 0.0
		if len(hTrades) > 0 {
			hWinRate = float64(hWins) / float64(len(hTrades))
		}
		hPF := 0.0
		if hLossTotal > 0 {
			hPF = hGains / hLossTotal
		}
		avgRet := 0.0
		avgMFE := 0.0
		avgMAE := 0.0
		if len(hTrades) > 0 {
			avgRet = sumRet / float64(len(hTrades))
			avgMFE = sumMFE / float64(len(hTrades))
			avgMAE = sumMAE / float64(len(hTrades))
		}

		horizonTable.Append([]string{
			fmt.Sprintf("%d Days Hold", h),
			fmt.Sprintf("%d", len(hTrades)),
			fmt.Sprintf("%d / %d", hWins, hLosses),
			fmt.Sprintf("%.2f%%", hWinRate*100),
			fmt.Sprintf("%.2f", hPF),
			fmt.Sprintf("%+.2f%%", avgRet*100),
			fmt.Sprintf("$%+.2f", hNetProfit),
			fmt.Sprintf("+%.2f%%", avgMFE*100),
			fmt.Sprintf("%.2f%%", avgMAE*100),
		})

		_, _ = db.Exec(`
			INSERT OR REPLACE INTO study_horizon_comparison (
				hold_days, total_trades, winning_trades, losing_trades, win_rate, win_rate_pct,
				profit_factor, avg_trade_return_pct, total_net_profit, avg_mae_pct, avg_mfe_pct
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, h, len(hTrades), hWins, hLosses, hWinRate, fmt.Sprintf("%.2f%%", hWinRate*100),
			hPF, avgRet, hNetProfit, avgMAE, avgMFE)
	}
	horizonTable.Render()

	// 7. Sample Recent Trades Table
	fmt.Printf("\n🔍 SAMPLE EXECUTED 4-DAY POSITION EXITS (FROM SQLITE `study_trades`):\n")
	tradeTable := tablewriter.NewWriter(os.Stdout)
	tradeTable.SetHeader([]string{"Symbol", "Entry Date", "Entry Price", "Exit Date (Day 4)", "Exit Price", "Hold Days", "Return %", "Net PnL", "Win/Loss"})
	tradeTable.SetBorder(true)

	displayTrades := allTrades
	if len(displayTrades) > 10 {
		displayTrades = displayTrades[len(displayTrades)-10:]
	}
	for _, t := range displayTrades {
		winStr := "🟢 WIN"
		if t.IsWin == 0 {
			winStr = "🔴 LOSS"
		}
		tradeTable.Append([]string{
			t.Symbol,
			t.EntryDate,
			fmt.Sprintf("$%.2f", t.EntryPrice),
			t.ExitDate,
			fmt.Sprintf("$%.2f", t.ExitPrice),
			fmt.Sprintf("%d days", t.HoldDays),
			fmt.Sprintf("%+.2f%%", t.ReturnPct*100),
			fmt.Sprintf("$%+.2f", t.NetPnL),
			winStr,
		})
	}
	tradeTable.Render()

	fmt.Printf("\n✨ Study complete! All SQLite tables are populated in: %s\n", *dbPath)
	fmt.Printf("   - Historical Bars:    `backtest_start` (%d bars across symbols)\n", len(barsBySymbol))
	fmt.Printf("   - Buy Signals:        `study_buy_signals` (%d signals recorded)\n", totalSignals)
	fmt.Printf("   - 4-Day Trades:       `study_trades` (%d trades executed)\n", len(allTrades))
	fmt.Printf("   - Win Rate Table:     `study_win_rates` (symbol-level & portfolio statistics)\n")
	fmt.Printf("   - Horizon Comparison: `study_horizon_comparison` (1d to 10d sensitivity analysis)\n\n")
}
