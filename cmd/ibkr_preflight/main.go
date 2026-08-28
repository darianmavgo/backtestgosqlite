// cmd/ibkr_preflight — Pre-launch diagnostic and credential validator for Interactive Brokers.
//
// Usage:
//
//	go run cmd/ibkr_preflight/main.go
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/darianmavgo/backtestgosqlite/internal/broker"
	"github.com/olekukonko/tablewriter"
)

func main() {
	configPath := flag.String("config", "config/ibkr_config.json", "Path to IBKR configuration file")
	flag.Parse()

	cfg, err := broker.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Println("=======================================================================================================================")
	fmt.Println("🔍 INTERACTIVE BROKERS PRE-LAUNCH DIAGNOSTICS & CREDENTIAL PRE-FLIGHT CHECK")
	fmt.Println("=======================================================================================================================")

	report := broker.RunPreflightDiagnostics(cfg)

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Category", "Pre-Flight Check", "Status", "Details & Diagnostics"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	passCount := 0
	warnCount := 0
	failCount := 0

	for _, item := range report.Items {
		badge := "🟢 PASS"
		if item.Status == "WARN" {
			badge = "🟡 WARN"
			warnCount++
		} else if item.Status == "FAIL" {
			badge = "🔴 FAIL"
			failCount++
		} else {
			passCount++
		}

		table.Append([]string{
			item.Category,
			item.Name,
			badge,
			item.Details,
		})
	}
	table.Render()

	// Print Summary & Actionable Remediation Guides
	fmt.Printf("\n📊 PRE-FLIGHT SUMMARY: %d Passed | %d Warnings | %d Failed\n\n", passCount, warnCount, failCount)

	hasRemediations := false
	for _, item := range report.Items {
		if item.Remediation != "" {
			if !hasRemediations {
				fmt.Println("📋 ACTIONABLE REMEDIATION STEPS:")
				hasRemediations = true
			}
			fmt.Printf("   • [%s - %s]:\n     ➔ %s\n\n", item.Category, item.Name, item.Remediation)
		}
	}

	fmt.Println("═══════════════════════════════════════════════════════════════════════════════════════")
	if failCount == 0 {
		fmt.Println("🚀 READINESS STATUS: SYSTEM READY FOR $10 LIVE TRANSACTION TESTING")
		fmt.Println("   • Manual Execution / Assisted Ticket Mode: 100% Ready (Generate ticket with cmd/ibkr_test_harness)")
		fmt.Println("   • Real-Time Market Scanner Mode: 100% Ready (Run scan with cmd/ibkr_bot)")
		if warnCount > 0 {
			fmt.Println("   • Direct Daemon / REST API Mode: Follow the remediation steps above to connect Client Portal Gateway or TWS.")
		}
	} else {
		fmt.Println("⚠️ READINESS STATUS: SOME CRITICAL CHECKS FAILED. Please resolve errors before live trading.")
	}
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════════════")
}
