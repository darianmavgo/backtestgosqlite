// cmd/ibkr_bot — Live EOD Market Scanner & Interactive Brokers Trade Execution Manager.
//
// Usage examples:
//
//	# 1. Run 3:50 PM daily market scan with $180,000 account NAV:
//	go run cmd/ibkr_bot/main.go -scan -capital 180000
//
//	# 2. Check current open position status:
//	go run cmd/ibkr_bot/main.go -status
//
//	# 3. Log an executed entry:
//	go run cmd/ibkr_bot/main.go -enter -symbol TECL -shares 420 -price 278.50
//
//	# 4. Log an executed exit (e.g. profit target hit):
//	go run cmd/ibkr_bot/main.go -close -price 292.42 -reason "TARGET_+5%"
//
//	# 5. View live trade performance history:
//	go run cmd/ibkr_bot/main.go -history
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/broker"
	"github.com/darianmavgo/backtestgosqlite/internal/live"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
)

func main() {
	dbPath := flag.String("db", "data/ibkr_live_state.db", "Path to SQLite live state tracking database")
	doScan := flag.Bool("scan", false, "Execute EOD market scan & generate trading signals")
	doStatus := flag.Bool("status", false, "Show active live position and risk parameters")
	doHistory := flag.Bool("history", false, "View completed live trade history")

	// Order Management Flags
	doEnter := flag.Bool("enter", false, "Log new position entry into live database")
	doClose := flag.Bool("close", false, "Log position exit into live database")
	symFlag := flag.String("symbol", "TECL", "Asset symbol (TECL or SPXU)")
	sharesFlag := flag.Int("shares", 0, "Executed share quantity")
	priceFlag := flag.Float64("price", 0.0, "Execution fill price ($)")
	reasonFlag := flag.String("reason", "TARGET_HIT", "Exit reason ('TARGET_HIT', 'STOP_HIT', 'TIME_LIMIT')")

	// Sizing Flags
	capital := flag.Float64("capital", 180000.0, "Interactive Brokers Account Net Liquidation Value ($)")
	allocRatio := flag.Float64("alloc", 0.65, "Dynamic capital allocation percentage (0.65 = 65%)")
	flag.Parse()

	db, err := live.InitLiveDB(*dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize live state DB: %v", err)
	}
	defer db.Close()

	if *doStatus {
		showActiveStatus(db)
		return
	}

	if *doHistory {
		showLiveHistory(db)
		return
	}

	if *doEnter {
		if *sharesFlag <= 0 || *priceFlag <= 0 {
			log.Fatalf("Please provide valid -shares and -price to record entry.")
		}
		ticket, err := broker.BuildBracketTicket(*symFlag, *priceFlag, *capital, *allocRatio)
		if err != nil {
			log.Fatalf("Error generating ticket: %v", err)
		}
		nowStr := time.Now().Format("2006-01-02")
		exitDate := time.Now().AddDate(0, 0, ticket.MaxHoldDays+4).Format("2006-01-02")
		err = live.RecordEntry(db, *symFlag, ticket.Direction, *sharesFlag, *priceFlag, ticket.TargetPrice, ticket.StopPrice, ticket.MaxHoldDays, nowStr, exitDate)
		if err != nil {
			log.Fatalf("Failed to record entry: %v", err)
		}
		fmt.Printf("✅ Successfully recorded live %s entry in %s: %d shares @ $%.2f\n", *symFlag, *dbPath, *sharesFlag, *priceFlag)
		showActiveStatus(db)
		return
	}

	if *doClose {
		pos, _ := live.GetOpenPosition(db)
		if pos == nil {
			fmt.Println("ℹ️ No active open position to close.")
			return
		}
		if *priceFlag <= 0 {
			log.Fatalf("Please provide valid -price for closing the trade.")
		}
		nowStr := time.Now().Format("2006-01-02")
		err := live.RecordExit(db, pos.ID, nowStr, *priceFlag, *reasonFlag)
		if err != nil {
			log.Fatalf("Failed to record exit: %v", err)
		}
		fmt.Printf("✅ Successfully closed position #%d (%s) at $%.2f (%s)\n", pos.ID, pos.Symbol, *priceFlag, *reasonFlag)
		showLiveHistory(db)
		return
	}

	// Default Action or explicit -scan
	_ = doScan
	runDailyScan(db, *capital, *allocRatio)
}

func runDailyScan(db *sqlx.DB, capital, allocRatio float64) {
	fmt.Println("=======================================================================================================================")
	fmt.Printf("⏰ INTERACTIVE BROKERS EOD MARKET SCANNER (All-Weather Dual Combo | Account NAV: $%.2f)\n", capital)
	fmt.Println("=======================================================================================================================")

	activePos, _ := live.GetOpenPosition(db)

	fmt.Println("📡 Fetching latest real-time daily candles for VOO from Yahoo Finance...")
	vooBars, err := live.FetchLiveBars("VOO", 260)
	if err != nil {
		log.Fatalf("Failed to fetch live VOO bars: %v", err)
	}

	scanRes := live.EvaluateDailyMarket(vooBars, activePos)
	_ = live.LogScanAudit(db, scanRes)

	// Display Scan Card
	fmt.Printf("\n📊 MARKET SCAN REPORT (Date: %s):\n", scanRes.Date)
	fmt.Printf("   • VOO Last Close: $%.2f\n", scanRes.VOOClose)
	fmt.Printf("   • VOO 200-Day SMA: $%.2f (Regime: %s)\n", scanRes.VOOSMA200, formatRegime(scanRes.IsBullRegime))
	fmt.Printf("   • Consecutive Streak: %d-Day %s\n", scanRes.StreakDays, scanRes.StreakType)
	fmt.Printf("   • Action Signal: %s\n", formatAction(scanRes.Action))
	fmt.Printf("   • Signal Rationale: %s\n\n", scanRes.ActionReason)

	if scanRes.Action == "BUY_TECL" {
		fmt.Println("📡 Fetching latest live price for TECL...")
		teclBars, err := live.FetchLiveBars("TECL", 10)
		if err != nil {
			log.Fatalf("Failed to fetch TECL live price: %v", err)
		}
		teclPrice := teclBars[len(teclBars)-1].Close

		ticket, err := broker.BuildBracketTicket("TECL", teclPrice, capital, allocRatio)
		if err != nil {
			log.Fatalf("Error building ticket: %v", err)
		}

		fmt.Println(ticket.TWSInstructions)
		fmt.Println("\n🤖 CLIENT PORTAL REST API JSON PAYLOAD (For Automated Execution):")
		fmt.Println(ticket.ClientPortalAPI)
		fmt.Println("\n💡 To record this trade once filled, run:")
		fmt.Printf("   go run cmd/ibkr_bot/main.go -enter -symbol TECL -shares %d -price %.2f\n\n", ticket.Shares, teclPrice)

	} else if scanRes.Action == "BUY_SPXU" {
		fmt.Println("📡 Fetching latest live price for SPXU...")
		spxuBars, err := live.FetchLiveBars("SPXU", 10)
		if err != nil {
			log.Fatalf("Failed to fetch SPXU live price: %v", err)
		}
		spxuPrice := spxuBars[len(spxuBars)-1].Close

		ticket, err := broker.BuildBracketTicket("SPXU", spxuPrice, capital, allocRatio)
		if err != nil {
			log.Fatalf("Error building ticket: %v", err)
		}

		fmt.Println(ticket.TWSInstructions)
		fmt.Println("\n🤖 CLIENT PORTAL REST API JSON PAYLOAD (For Automated Execution):")
		fmt.Println(ticket.ClientPortalAPI)
		fmt.Println("\n💡 To record this trade once filled, run:")
		fmt.Printf("   go run cmd/ibkr_bot/main.go -enter -symbol SPXU -shares %d -price %.2f\n\n", ticket.Shares, spxuPrice)

	} else if scanRes.Action == "HOLD_ACTIVE_POSITION" {
		showActiveStatus(db)
	} else {
		fmt.Println("💵 ACTION: IDLE CASH COMPOUNDING")
		fmt.Printf("   • 100%% of your $%.2f capital remains in cash / SGOV earning IBKR ~4.83%% APY.\n", capital)
		fmt.Println("   • Zero market risk. Next scan scheduled tomorrow at 3:50 PM EST.")
	}
}

func showActiveStatus(db *sqlx.DB) {
	pos, err := live.GetOpenPosition(db)
	if err != nil || pos == nil {
		fmt.Println("\nℹ️ No active position currently open. 100% Cash / T-Bills earning ~4.8% APY.")
		return
	}

	fmt.Printf("\n📋 ACTIVE LIVE POSITION (Position ID #%d):\n", pos.ID)
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Asset", "Direction", "Shares", "Entry Price", "Entry Date", "Target (+Profit)", "Disaster Stop", "Max Exit Date", "Status"})
	table.SetBorder(true)

	table.Append([]string{
		pos.Symbol,
		pos.Direction,
		fmt.Sprintf("%d", pos.Shares),
		fmt.Sprintf("$%.2f", pos.EntryPrice),
		pos.EntryDate,
		fmt.Sprintf("🎯 $%.2f", pos.TargetPrice),
		fmt.Sprintf("🛑 $%.2f", pos.StopPrice),
		pos.MaxExitDate,
		"🟢 " + pos.Status,
	})
	table.Render()
	fmt.Printf("\n💡 To close this trade, run: go run cmd/ibkr_bot/main.go -close -price <fill_price> -reason <TARGET_HIT/STOP_HIT>\n\n")
}

func showLiveHistory(db *sqlx.DB) {
	type HistTrade struct {
		ID         int     `db:"id"`
		Symbol     string  `db:"symbol"`
		Direction  string  `db:"direction"`
		EntryDate  string  `db:"entry_date"`
		ExitDate   string  `db:"exit_date"`
		EntryPrice float64 `db:"entry_price"`
		ExitPrice  float64 `db:"exit_price"`
		Shares     int     `db:"shares"`
		NetPnL     float64 `db:"net_pnl"`
		ReturnPct  float64 `db:"return_pct"`
		ExitReason string  `db:"exit_reason"`
	}

	var history []HistTrade
	_ = db.Select(&history, `SELECT id, symbol, direction, entry_date, exit_date, entry_price, exit_price, shares, net_pnl, return_pct, exit_reason FROM live_trade_history ORDER BY id DESC;`)

	if len(history) == 0 {
		fmt.Println("\nℹ️ No closed live trade history found yet.")
		return
	}

	fmt.Printf("\n📜 COMPLETED LIVE TRADE HISTORY (%d Trades):\n", len(history))
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"#", "Asset", "Direction", "Trade Window", "Entry Price", "Exit Price", "Shares", "Return %", "Net PnL ($)", "Exit Reason"})
	table.SetBorder(true)

	totalPnL := 0.0
	wins := 0
	for _, t := range history {
		totalPnL += t.NetPnL
		if t.NetPnL > 0 {
			wins++
		}
		table.Append([]string{
			fmt.Sprintf("#%d", t.ID),
			t.Symbol,
			t.Direction,
			fmt.Sprintf("%s ➔ %s", t.EntryDate, t.ExitDate),
			fmt.Sprintf("$%.2f", t.EntryPrice),
			fmt.Sprintf("$%.2f", t.ExitPrice),
			fmt.Sprintf("%d", t.Shares),
			fmt.Sprintf("%+.2f%%", t.ReturnPct*100),
			fmt.Sprintf("$%+.2f", t.NetPnL),
			t.ExitReason,
		})
	}
	table.Render()

	winRate := float64(wins) / float64(len(history)) * 100.0
	fmt.Printf("\n📊 Total Realized Profit: $%.2f | Win Rate: %.1f%% (%d Wins / %d Losses)\n\n", totalPnL, winRate, wins, len(history)-wins)
}

func formatRegime(isBull bool) string {
	if isBull {
		return "🟢 Bull Market (VOO ≥ 200 SMA)"
	}
	return "🔴 Bear Market (VOO < 200 SMA)"
}

func formatAction(action string) string {
	switch action {
	case "BUY_TECL":
		return "🟢 BUY TECL (Long Dip Setup)"
	case "BUY_SPXU":
		return "🔴 BUY SPXU (Bear Rally Fade Setup)"
	case "HOLD_ACTIVE_POSITION":
		return "⏳ HOLD ACTIVE POSITION (Let Bracket Run)"
	default:
		return "💵 HOLD 100% CASH (Earn 4.8% APY)"
	}
}
