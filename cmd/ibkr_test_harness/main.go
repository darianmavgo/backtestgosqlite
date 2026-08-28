// cmd/ibkr_test_harness — Interactive Brokers Test Harness for Complex Orders ($10 Fractional Bracket).
//
// Usage examples:
//
//	# 1. Generate the ultimate $10 complex bracket test ticket for TECL:
//	go run cmd/ibkr_test_harness/main.go -symbol TECL -amount 10
//
//	# 2. Test with SOXL and Trailing Stop:
//	go run cmd/ibkr_test_harness/main.go -symbol SOXL -amount 10 -trail -trail-pct 0.04
//
//	# 3. Simulate execution into live SQLite state tracking:
//	go run cmd/ibkr_test_harness/main.go -symbol TECL -amount 10 -record
//
//	# 4. Test live API submission to IBKR Client Portal Gateway (if running on localhost:5000):
//	go run cmd/ibkr_test_harness/main.go -symbol TECL -amount 10 -api -gateway https://localhost:5000
package main

import (
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/darianmavgo/backtestgosqlite/internal/broker"
	"github.com/darianmavgo/backtestgosqlite/internal/live"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	symFlag := flag.String("symbol", "TECL", "Asset symbol for test order (e.g. TECL, SOXL, SPY, AAPL)")
	amountFlag := flag.Float64("amount", 10.00, "Exact cash dollar amount to purchase ($)")
	tpFlag := flag.Float64("tp", 0.05, "Take-profit percentage (0.05 = +5.0%)")
	slFlag := flag.Float64("sl", 0.05, "Stop-loss percentage (0.05 = -5.0%)")
	trailFlag := flag.Bool("trail", true, "Use dynamic Trailing Stop instead of fixed Stop Loss")
	trailPctFlag := flag.Float64("trail-pct", 0.04, "Trailing stop percentage (0.04 = 4.0% trail from high)")
	outsideRTHFlag := flag.Bool("extended", true, "Allow execution Outside Regular Trading Hours (Pre/Post-market)")
	adaptiveFlag := flag.Bool("adaptive", true, "Use IBKR Adaptive Smart-Routing Algo")
	accountIDFlag := flag.String("account", "DU1234567", "IBKR Account ID (Paper or Live)")
	recordFlag := flag.Bool("record", false, "Record test position into data/ibkr_live_state.db")
	apiFlag := flag.Bool("api", false, "Attempt live submission to IBKR Client Portal Gateway")
	gatewayURL := flag.String("gateway", "https://localhost:5000", "IBKR Client Portal Gateway base URL")
	flag.Parse()

	fmt.Println("=======================================================================================================================")
	fmt.Printf("🧪 INTERACTIVE BROKERS COMPLEX ORDER TEST HARNESS ($%.2f USD BUY ON %s)\n", *amountFlag, *symFlag)
	fmt.Println("=======================================================================================================================")

	// 1. Fetch live market price for the target asset
	fmt.Printf("📡 Fetching latest real-time market price for %s...\n", *symFlag)
	bars, err := live.FetchLiveBars(*symFlag, 5)
	if err != nil {
		log.Fatalf("Failed to fetch live price for %s: %v", *symFlag, err)
	}
	latestPrice := bars[len(bars)-1].Close
	latestDate := bars[len(bars)-1].Date
	fmt.Printf("   • %s Current Price: $%.2f (Date: %s)\n\n", *symFlag, latestPrice, latestDate)

	// 2. Build the ultimate complex order specification
	orderCfg := broker.ComplexOrderConfig{
		Symbol:          *symFlag,
		DollarAmount:    *amountFlag,
		EstPrice:        latestPrice,
		TakeProfitPct:   *tpFlag,
		StopLossPct:     *slFlag,
		UseTrailingStop: *trailFlag,
		TrailingStopPct: *trailPctFlag,
		OutsideRTH:      *outsideRTHFlag,
		UseAdaptiveAlgo: *adaptiveFlag,
		AlgoPriority:    "Normal",
		MaxHoldingDays:  8,
		AccountID:       *accountIDFlag,
	}

	order, err := broker.BuildComplexOrder(orderCfg)
	if err != nil {
		log.Fatalf("Failed to build complex order: %v", err)
	}

	// 3. Display the Complete Multi-Leg Bracket Guide
	fmt.Println(order.TWSManualGuide)

	// 4. Display the TWS Socket Specification (Python / IBAPI / Socket)
	fmt.Println("\n" + order.TWSSocketSpec)

	// 5. Display the Client Portal REST API JSON Payload
	fmt.Println("\n🌐 CLIENT PORTAL WEB API MULTI-LEG BRACKET JSON PAYLOAD:")
	fmt.Println(order.ClientPortalJSON)

	// 6. Direct TWS / IB Gateway API Socket Transmission
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

	// 7. Optional: Submit to Live Client Portal Gateway if flag is set
	if *apiFlag {
		fmt.Printf("\n🚀 Attempting connection to IBKR Client Portal Gateway at %s/v1/api/iserver/account...\n", *gatewayURL)
		testIBKRGatewayConnection(*gatewayURL, *accountIDFlag, order.ClientPortalJSON)
	}

	// 7. Optional: Record in SQLite live state tracking
	if *recordFlag {
		db, err := live.InitLiveDB("data/ibkr_live_state.db")
		if err != nil {
			log.Fatalf("Failed to open live DB: %v", err)
		}
		defer db.Close()

		nowStr := time.Now().Format("2006-01-02")
		exitDate := time.Now().AddDate(0, 0, 12).Format("2006-01-02")
		sharesInt := int(order.FractionalShares)
		if sharesInt < 1 {
			sharesInt = 1 // SQLite integer share representation
		}

		err = live.RecordEntry(db, *symFlag, "TEST_COMPLEX_BUY", sharesInt, latestPrice, order.TargetPrice, order.StopLossPrice, 8, nowStr, exitDate)
		if err != nil {
			log.Fatalf("Failed to record entry: %v", err)
		}
		fmt.Println("\n💾 Recorded $10 test position in data/ibkr_live_state.db successfully!")
	}

	fmt.Println("\n✅ Test harness execution completed successfully.")
}

func testIBKRGatewayConnection(baseURL, accountID, payloadJSON string) {
	// IBKR Client Portal local gateway uses self-signed SSL certs
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr, Timeout: 5 * time.Second}

	// Check status
	resp, err := client.Get(baseURL + "/v1/api/iserver/auth/status")
	if err != nil {
		fmt.Printf("⚠️ IBKR Gateway not reachable at %s: %v\n", baseURL, err)
		fmt.Println("ℹ️ To use direct API submission, start the IBKR Client Portal Gateway (bin/run.sh) and authenticate.")
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("📡 Gateway Auth Status Response: %s\n", string(body))

	// Preview / Submit Order
	orderURL := fmt.Sprintf("%s/v1/api/iserver/account/%s/orders", baseURL, accountID)
	req, _ := http.NewRequest("POST", orderURL, bytes.NewBuffer([]byte(payloadJSON)))
	req.Header.Set("Content-Type", "application/json")

	orderResp, err := client.Do(req)
	if err != nil {
		fmt.Printf("⚠️ Order submission failed: %v\n", err)
		return
	}
	defer orderResp.Body.Close()
	orderBody, _ := io.ReadAll(orderResp.Body)
	fmt.Printf("📡 Order Submission Response: %s\n", string(orderBody))
}
