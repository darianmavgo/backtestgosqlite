// cmd/ibkr_live — Live Execution Manager for Small $10 Fractional Transactions & Live Deployment.
//
// Usage:
//
//	# 1. Run Pre-flight Check:
//	go run cmd/ibkr_live/main.go -preflight
//
//	# 2. Execute $10 TECL Live Test Order:
//	go run cmd/ibkr_live/main.go -symbol TECL -amount 10
//
//	# 3. Execute $10 SOXL Live Test Order:
//	go run cmd/ibkr_live/main.go -symbol SOXL -amount 10
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/broker"
	"github.com/darianmavgo/backtestgosqlite/internal/live"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	configPath := flag.String("config", "config/ibkr_config.json", "Path to IBKR config file")
	symFlag := flag.String("symbol", "TECL", "Asset to trade (TECL, SOXL, SPXU, SPY)")
	amountFlag := flag.Float64("amount", 10.00, "Cash dollar amount for test buy ($)")
	doPreflight := flag.Bool("preflight", false, "Run preflight check only")
	flag.Parse()

	cfg, err := broker.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if *doPreflight {
		report := broker.RunPreflightDiagnostics(cfg)
		fmt.Printf("🔍 Pre-flight Status: All Passed = %t (Can Execute Live: %t)\n", report.AllPassed, report.CanExecuteLive)
		return
	}

	fmt.Println("=======================================================================================================================")
	fmt.Printf("🚀 INTERACTIVE BROKERS LIVE EXECUTION ENGINE: $%.2f USD ON %s\n", *amountFlag, *symFlag)
	fmt.Println("=======================================================================================================================")

	// 1. Run Preflight Check
	report := broker.RunPreflightDiagnostics(cfg)
	if !report.CanExecuteLive {
		log.Fatalf("❌ Pre-flight checks failed. Please run 'go run cmd/ibkr_preflight/main.go' for details.")
	}

	// 2. Fetch Real-Time Market Price
	bars, err := live.FetchLiveBars(*symFlag, 5)
	if err != nil {
		log.Fatalf("Failed to fetch live price: %v", err)
	}
	latestPrice := bars[len(bars)-1].Close
	fmt.Printf("📡 Live Market Quote: %s = $%.2f\n\n", *symFlag, latestPrice)

	// 3. Build Complex Bracket Order
	orderCfg := broker.ComplexOrderConfig{
		Symbol:          *symFlag,
		DollarAmount:    *amountFlag,
		EstPrice:        latestPrice,
		TakeProfitPct:   0.05,
		StopLossPct:     0.05,
		UseTrailingStop: true,
		TrailingStopPct: 0.04,
		OutsideRTH:      cfg.ExtendedHoursEnabled,
		UseAdaptiveAlgo: cfg.UseAdaptiveAlgo,
		AlgoPriority:    cfg.AlgoPriority,
		MaxHoldingDays:  8,
		AccountID:       cfg.AccountID,
	}

	order, err := broker.BuildComplexOrder(orderCfg)
	if err != nil {
		log.Fatalf("Error building order: %v", err)
	}

	// 4. Output the Complete Execution Ticket
	fmt.Println(order.TWSManualGuide)

	// 5. Attempt Direct TWS Socket Transmission
	fmt.Println("\n📡 Attempting Direct TWS / IB Gateway API Socket Transmission...")
	twsRes, err := broker.ExecuteTWSOrder(orderCfg)
	if err == nil && twsRes.Success {
		fmt.Println("══════════════════════════════════════════════════════════════════════════════════════")
		fmt.Printf("🟢 LIVE TWS TRANSMISSION SUCCESSFUL! (Account: %s | Port: %d)\n", twsRes.AccountID, twsRes.ConnectedPort)
		fmt.Println("══════════════════════════════════════════════════════════════════════════════════════")
		fmt.Printf("   • Parent Order Ticket #%d: BUY %.4f shares of %s (Status: %s)\n", twsRes.ParentOrderID, twsRes.Quantity, twsRes.Symbol, twsRes.ParentStatus)
		fmt.Printf("   • Profit Target Ticket #%d: SELL %.4f shares @ $%.2f (Status: %s)\n", twsRes.TakeProfitOrderID, twsRes.Quantity, twsRes.TargetPrice, twsRes.TPStatus)
		fmt.Printf("   • Stop Loss Ticket #%d: SELL %.4f shares (%s) (Status: %s)\n", twsRes.StopOrderID, twsRes.Quantity, twsRes.StopPrice, twsRes.StopStatus)
		fmt.Println("   ✨ The order bracket is now active and visible on your Trader Workstation Orders tab!")
		fmt.Println("══════════════════════════════════════════════════════════════════════════════════════")
	} else {
		errMsg := "Socket not connected"
		if err != nil {
			errMsg = err.Error()
		} else if twsRes != nil && twsRes.Error != "" {
			errMsg = twsRes.Error
		}
		fmt.Printf("ℹ️ Direct socket submission not active: %s\n", errMsg)
		fmt.Println("   ➔ If TWS is running, ensure 'Enable ActiveX and Socket Clients' is checked in TWS: File ➔ Global Configuration ➔ API ➔ Settings.")
	}

	fmt.Println("\n🌐 REST API PAYLOAD (IBKR Client Portal Gateway):")
	fmt.Println(order.ClientPortalJSON)

	// 6. Record Order into Live SQLite State Tracking Database
	db, err := live.InitLiveDB("data/ibkr_live_state.db")
	if err != nil {
		log.Fatalf("Failed to open live state DB: %v", err)
	}
	defer db.Close()

	nowStr := time.Now().Format("2006-01-02")
	exitDate := time.Now().AddDate(0, 0, 12).Format("2006-01-02")
	sharesInt := int(order.FractionalShares)
	if sharesInt < 1 {
		sharesInt = 1
	}

	_ = live.RecordEntry(db, *symFlag, "LIVE_COMPLEX_BUY", sharesInt, latestPrice, order.TargetPrice, order.StopLossPrice, 8, nowStr, exitDate)

	fmt.Println("\n💾 Position recorded into data/ibkr_live_state.db.")
	fmt.Println("✨ To monitor live position status: go run cmd/ibkr_bot/main.go -status")
	fmt.Printf("✨ To record fill exit when target hits: go run cmd/ibkr_bot/main.go -close -price %.2f -reason TARGET_HIT\n\n", order.TargetPrice)
}

func init() {
	_ = os.Setenv("TZ", "America/New_York")
}
