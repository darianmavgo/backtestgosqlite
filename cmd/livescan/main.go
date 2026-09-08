package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

type liveSignalAction struct {
	Status           string  `json:"status"` // "ACTIONABLE_NOW" or "RECENT_SETUP"
	Timing           string  `json:"timing"` // "TODAY (Close) / TOMORROW (Open)"
	StrategyID       string  `json:"strategy_id"`
	StrategyName     string  `json:"strategy_name"`
	Symbol           string  `json:"symbol"`
	SignalDate       string  `json:"signal_date"`
	Direction        string  `json:"direction"`
	OrderType        string  `json:"order_type"`
	EntryPrice       float64 `json:"entry_price"`
	TargetPrice      float64 `json:"target_price"`
	TargetPct        float64 `json:"target_pct"`
	StopLossPrice    float64 `json:"stop_loss_price"`
	StopLossPct      float64 `json:"stop_loss_pct"`
	HoldDays         int     `json:"hold_days"`
	AllocationDollar float64 `json:"allocation_dollar"`
	EstimatedShares  int     `json:"estimated_shares"`
	Regime           string  `json:"regime,omitempty"`
}

func listStrategies() {
	fmt.Printf("\n========================================================================================================================\n")
	fmt.Printf("📋 REGISTERED TRADING STRATEGIES AVAILABLE FOR LIVE SCAN\n")
	fmt.Printf("========================================================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"ID", "Type", "Strategy Name", "Default Target", "Default Stop", "Hold", "Description"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, s := range strategy.List() {
		cfg := s.DefaultConfig()
		sType := "Go"
		if strings.HasSuffix(s.ID(), "-sql") {
			sType = "SQL Pipeline"
		}
		table.Append([]string{
			s.ID(),
			sType,
			s.Name(),
			fmt.Sprintf("+%.1f%%", (cfg.TargetPct-1)*100),
			fmt.Sprintf("-%.1f%%", (1-cfg.StopLossPct)*100),
			fmt.Sprintf("%dd", cfg.HoldingWindow),
			s.Description(),
		})
	}
	table.Render()
	fmt.Printf("\nRun live scan with: ./bin/livescan -strategy <ID> [-symbol <SYM>]\n\n")
}

func buildConfig(s strategy.Strategy, stopLoss, profitTarget float64, holdWindow, maxPositions int) strategy.StrategyConfig {
	cfg := s.DefaultConfig()
	if stopLoss > 0 {
		cfg.StopLossPct = stopLoss
	}
	if profitTarget > 0 {
		cfg.TargetPct = profitTarget
	}
	if holdWindow > 0 {
		cfg.HoldingWindow = holdWindow
	}
	if maxPositions > 0 {
		cfg.PositionCap = maxPositions
	}
	return cfg
}

// parseArgsWithPositional separates positional arguments from flags so that
// flags can be placed anywhere (e.g. ./bin/livescan bb-capitulation -lookback 10).
func parseArgsWithPositional() []string {
	var flagArgs []string
	var posArgs []string

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
			// Known flags that do not take a subsequent argument
			boolFlags := map[string]bool{
				"-list": true, "--list": true,
				"-json": true, "--json": true,
				"-save": true, "--save": true,
			}
			if !strings.Contains(arg, "=") && !boolFlags[arg] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flagArgs = append(flagArgs, args[i])
			}
		} else {
			posArgs = append(posArgs, arg)
		}
	}

	_ = flag.CommandLine.Parse(flagArgs)
	return posArgs
}

func main() {
	defaultMarketDb := "data/market_history.db"
	if _, err := os.Stat(defaultMarketDb); os.IsNotExist(err) {
		defaultMarketDb = "data/leveraged_backtest.db"
	}

	targetDb := flag.String("db", defaultMarketDb, "Path to source SQLite DB containing historical market bars")
	tableName := flag.String("table", "backtest_start", "Table name containing historical bars")
	strategyType := flag.String("strategy", "", "Strategy ID to scan, comma-separated list, or 'all' (e.g. bb-capitulation,trend-bb,rsi2)")
	symbolFilter := flag.String("symbol", "", "Optional: Filter scan to specific symbol(s) (e.g. SOXL, SOXL,TECL,AAPL)")
	capital := flag.Float64("capital", 100000.0, "Portfolio capital for calculating position sizing")
	barsLimit := flag.Int("bars", 250, "Number of recent historical bars per symbol to load for indicator calculations")
	lookbackDays := flag.Int("lookback", 3, "Number of recent trading days to display signals for")
	maxPositions := flag.Int("max-positions", 0, "Optional override: Maximum concurrent open positions allowed")
	stopLoss := flag.Float64("stoploss", 0.0, "Optional override: Stop-loss floor multiplier (e.g. 0.93 for -7%)")
	profitTarget := flag.Float64("target", 0.0, "Optional override: Take-profit multiplier (e.g. 1.18 for +18%)")
	holdWindow := flag.Int("hold", 0, "Optional override: Max holding days window")
	listFlag := flag.Bool("list", false, "List all registered Go and SQL strategies")
	saveFlag := flag.Bool("save", false, "Persist scanned signals to SQLite database (default: reports/livescan_signals.db)")
	saveDbPath := flag.String("save-db", "", "Optional custom path to SQLite DB to persist scanned signals")
	jsonOutput := flag.Bool("json", false, "Output actionable signals as JSON")

	posArgs := parseArgsWithPositional()

	// Auto-discover any SQL pipeline strategies in sql/strategies/
	strategy.AutoRegisterSQLStrategies(".", *targetDb)

	if *listFlag {
		listStrategies()
		return
	}

	// Resolve strategies and symbols from flags or positional arguments
	stratArg := strings.TrimSpace(*strategyType)
	if stratArg == "" && len(posArgs) > 0 {
		stratArg = posArgs[0]
		// If a second positional argument is provided and symbolFilter wasn't set, use it as symbol
		if *symbolFilter == "" && len(posArgs) > 1 && !strings.HasPrefix(posArgs[1], "-") {
			*symbolFilter = posArgs[1]
		}
	}
	if stratArg == "" {
		stratArg = "bb-capitulation"
	}

	var selectedStrategies []strategy.Strategy
	if strings.ToLower(stratArg) == "all" {
		selectedStrategies = strategy.List()
	} else {
		tokens := strings.FieldsFunc(stratArg, func(r rune) bool {
			return r == ',' || r == ' '
		})
		for _, token := range tokens {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			s, exists := strategy.Get(token)
			if !exists {
				log.Fatalf("Strategy '%s' not found in registry. Run with -list to view available strategies.", token)
			}
			selectedStrategies = append(selectedStrategies, s)
		}
	}

	if len(selectedStrategies) == 0 {
		log.Fatalf("No valid strategies selected. Run with -list to view available strategies.")
	}

	// Configure DB path for any SQLPipelineStrategy
	for _, s := range selectedStrategies {
		if sqlStrat, ok := s.(*strategy.SQLPipelineStrategy); ok {
			sqlStrat.SetDBPath(*targetDb)
		}
	}

	// Open DB connection
	db, err := storage.OpenSQLite(*targetDb)
	if err != nil {
		log.Fatalf("Failed to open market DB %s: %v", *targetDb, err)
	}
	defer db.Close()

	// Resolve symbols filter
	var requestedSymbols []string
	if *symbolFilter != "" {
		parts := strings.Split(*symbolFilter, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(strings.ToUpper(p))
			if trimmed != "" {
				requestedSymbols = append(requestedSymbols, trimmed)
			}
		}
	}

	// Fetch recent bars (e.g. past 250 bars per symbol)
	barsBySymbol, sortedDates, err := storage.FetchRecentBars(db, *tableName, requestedSymbols, *barsLimit)
	if err != nil {
		log.Fatalf("Error loading recent bars for live scan: %v", err)
	}
	if len(barsBySymbol) == 0 || len(sortedDates) == 0 {
		log.Fatalf("No historical bar data found in %s (%s). Run ./bin/download first to cache data.", *targetDb, *tableName)
	}

	latestDate := sortedDates[len(sortedDates)-1]
	previousDate := ""
	if len(sortedDates) >= 2 {
		previousDate = sortedDates[len(sortedDates)-2]
	}

	// Determine lookback cutoff date
	cutoffDays := *lookbackDays
	if cutoffDays < 1 {
		cutoffDays = 1
	}
	cutoffIdx := len(sortedDates) - cutoffDays
	if cutoffIdx < 0 {
		cutoffIdx = 0
	}
	cutoffDate := sortedDates[cutoffIdx]

	if !*jsonOutput {
		fmt.Printf("\n========================================================================================\n")
		fmt.Printf("⚡ LIVE SIGNAL SCANNER\n")
		fmt.Printf("📅 LATEST MARKET CLOSE : %s (Previous: %s)\n", latestDate, previousDate)
		fmt.Printf("📊 UNIVERSE MONITORED  : %d symbols (%d bars loaded per symbol from %s)\n",
			len(barsBySymbol), *barsLimit, *targetDb)
		fmt.Printf("💰 PORTFOLIO CAPITAL   : $%.2f\n", *capital)
		fmt.Printf("========================================================================================\n")
	}

	type stratScanResult struct {
		strat   strategy.Strategy
		cfg     strategy.StrategyConfig
		signals []models.Signal
	}

	resultsChan := make(chan stratScanResult, len(selectedStrategies))
	var wg sync.WaitGroup

	for _, s := range selectedStrategies {
		wg.Add(1)
		go func(strat strategy.Strategy) {
			defer wg.Done()
			cfg := buildConfig(strat, *stopLoss, *profitTarget, *holdWindow, *maxPositions)
			sigs := strat.GenerateSignals(barsBySymbol)
			resultsChan <- stratScanResult{strat: strat, cfg: cfg, signals: sigs}
		}(s)
	}

	wg.Wait()
	close(resultsChan)

	var actionableToday []liveSignalAction
	var recentSignals []liveSignalAction
	allLiveSignalsToSave := []models.Signal{}

	for res := range resultsChan {
		strat := res.strat
		cfg := res.cfg
		signals := res.signals

		// Calculate allocation sizing per trade
		allocPct := cfg.AllocationPct
		if allocPct <= 0 && cfg.PositionCap > 0 {
			allocPct = 1.0 / float64(cfg.PositionCap)
		}
		if allocPct <= 0 {
			allocPct = 0.20 // Default 20%
		}
		posCapital := *capital * allocPct

		for _, sig := range signals {
			if sig.Date < cutoffDate {
				continue
			}

			entryPrice := sig.BuyLimit
			if entryPrice == 0 {
				entryPrice = sig.Close
			}
			if entryPrice == 0 {
				continue
			}

			direction := sig.Direction
			if direction == "" {
				direction = "LONG"
			}
			orderType := sig.OrderType
			if orderType == "" {
				orderType = "limit"
			}

			targetMultiplier := cfg.TargetPct
			if sig.TakeProfit > 0 {
				targetMultiplier = sig.TakeProfit
			}
			targetPrice := entryPrice * targetMultiplier
			targetPct := (targetMultiplier - 1.0) * 100

			stopMultiplier := cfg.StopLossPct
			if sig.StopLoss > 0 {
				stopMultiplier = sig.StopLoss
			}
			stopPrice := entryPrice * stopMultiplier
			stopPct := (1.0 - stopMultiplier) * 100

			holdDays := cfg.HoldingWindow

			shares := int(posCapital / entryPrice)
			if shares < 1 {
				shares = 1
			}

			isActionable := (sig.Date == latestDate)
			status := "RECENT_SETUP"
			timing := fmt.Sprintf("Triggered %s (Setup active if limit unfilled)", sig.Date)
			if isActionable {
				status = "ACTIONABLE_NOW"
				timing = "TODAY (Market Close) / TOMORROW (Market Open)"
			}

			act := liveSignalAction{
				Status:           status,
				Timing:           timing,
				StrategyID:       strat.ID(),
				StrategyName:     strat.Name(),
				Symbol:           sig.Symbol,
				SignalDate:       sig.Date,
				Direction:        direction,
				OrderType:        strings.ToUpper(orderType),
				EntryPrice:       entryPrice,
				TargetPrice:      targetPrice,
				TargetPct:        targetPct,
				StopLossPrice:    stopPrice,
				StopLossPct:      stopPct,
				HoldDays:         holdDays,
				AllocationDollar: posCapital,
				EstimatedShares:  shares,
				Regime:           sig.Regime,
			}

			if isActionable {
				actionableToday = append(actionableToday, act)
			} else {
				recentSignals = append(recentSignals, act)
			}

			allLiveSignalsToSave = append(allLiveSignalsToSave, sig)
		}
	}

	// Sort results cleanly
	sort.Slice(actionableToday, func(i, j int) bool {
		if actionableToday[i].Symbol == actionableToday[j].Symbol {
			return actionableToday[i].StrategyID < actionableToday[j].StrategyID
		}
		return actionableToday[i].Symbol < actionableToday[j].Symbol
	})

	sort.Slice(recentSignals, func(i, j int) bool {
		if recentSignals[i].SignalDate == recentSignals[j].SignalDate {
			return recentSignals[i].Symbol < recentSignals[j].Symbol
		}
		return recentSignals[i].SignalDate > recentSignals[j].SignalDate
	})

	// Output as JSON if requested
	if *jsonOutput {
		payload := map[string]interface{}{
			"latest_market_date": latestDate,
			"previous_date":     previousDate,
			"symbols_monitored": len(barsBySymbol),
			"capital":           *capital,
			"actionable_today":  actionableToday,
			"recent_setups":     recentSignals,
		}
		bytes, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			log.Fatalf("Failed to marshal JSON output: %v", err)
		}
		fmt.Println(string(bytes))
		return
	}

	// Render Actionable Signals for Today/Tomorrow
	if len(actionableToday) > 0 {
		fmt.Printf("\n🚨 %d ACTIONABLE ENTRY SIGNALS DETECTED FOR TODAY / TOMORROW:\n", len(actionableToday))
		fmt.Printf("   Market bar %s closed with confirmation criteria met. Enter position now or on next market open.\n\n", latestDate)

		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"Symbol", "Strategy", "Action", "Order", "Trigger $", "Target (+%)", "Stop-Loss (-%)", "Hold", "Est. Shares", "Position $"})
		table.SetBorder(true)
		table.SetAutoWrapText(false)

		for _, a := range actionableToday {
			actionStr := fmt.Sprintf("ENTER %s", a.Direction)
			targetStr := fmt.Sprintf("$%.2f (+%.1f%%)", a.TargetPrice, a.TargetPct)
			stopStr := fmt.Sprintf("$%.2f (-%.1f%%)", a.StopLossPrice, a.StopLossPct)
			sharesStr := fmt.Sprintf("%d shs", a.EstimatedShares)
			allocStr := fmt.Sprintf("$%.2f", a.AllocationDollar)
			holdStr := fmt.Sprintf("%dd", a.HoldDays)

			table.Append([]string{
				a.Symbol,
				a.StrategyID,
				actionStr,
				a.OrderType,
				fmt.Sprintf("$%.2f", a.EntryPrice),
				targetStr,
				stopStr,
				holdStr,
				sharesStr,
				allocStr,
			})
		}
		table.Render()

		fmt.Printf("\n📌 ACTIONABLE EXECUTION ORDERS (NEXT TRADING SESSION):\n")
		for _, a := range actionableToday {
			fmt.Printf("   ➔ [%-5s] %s order: Enter %s at $%.2f for ~%d shares (~$%.2f). Target: $%.2f (+%.1f%%) | Stop: $%.2f (-%.1f%%) | Max Hold: %dd\n",
				a.Symbol, a.OrderType, a.Direction, a.EntryPrice, a.EstimatedShares, a.AllocationDollar, a.TargetPrice, a.TargetPct, a.StopLossPrice, a.StopLossPct, a.HoldDays)
		}
	} else {
		fmt.Printf("\n⚪ NO ACTIVE ENTRY SIGNALS DETECTED FOR TODAY / TOMORROW (%s)\n", latestDate)
		fmt.Printf("   All scanned symbols traded within standard bands or did not meet reversal confirmation criteria.\n")
	}

	// Render Recent Historical Setups for Context
	if len(recentSignals) > 0 {
		fmt.Printf("\n⏳ RECENT PREVIOUS SIGNALS (PAST %d DAYS):\n", cutoffDays)
		rTable := tablewriter.NewWriter(os.Stdout)
		rTable.SetHeader([]string{"Date", "Symbol", "Strategy", "Action", "Trigger $", "Target", "Stop", "Status"})
		rTable.SetBorder(true)
		rTable.SetAutoWrapText(false)

		for _, r := range recentSignals {
			rTable.Append([]string{
				r.SignalDate,
				r.Symbol,
				r.StrategyID,
				fmt.Sprintf("ENTER %s", r.Direction),
				fmt.Sprintf("$%.2f", r.EntryPrice),
				fmt.Sprintf("$%.2f (+%.1f%%)", r.TargetPrice, r.TargetPct),
				fmt.Sprintf("$%.2f (-%.1f%%)", r.StopLossPrice, r.StopLossPct),
				"Prior Bar Triggered",
			})
		}
		rTable.Render()
	}

	// Save signals to SQLite if requested
	finalSavePath := ""
	if *saveDbPath != "" {
		finalSavePath = *saveDbPath
	} else if *saveFlag {
		finalSavePath = "reports/livescan_signals.db"
	}

	if finalSavePath != "" {
		_ = os.MkdirAll(filepath.Dir(finalSavePath), 0755)
		saveDB, err := storage.OpenSQLite(finalSavePath)
		if err != nil {
			log.Printf("Warning: Failed to open save database %s: %v", finalSavePath, err)
		} else {
			defer saveDB.Close()
			for _, s := range selectedStrategies {
				_ = storage.SaveSignals(saveDB, s.ID(), allLiveSignalsToSave)
			}
			fmt.Printf("\n💾 Saved %d live signals to SQLite: %s (table: signals)\n", len(allLiveSignalsToSave), finalSavePath)
		}
	}
	fmt.Println()
}
