package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
	"github.com/darianmavgo/backtestgosqlite/internal/analytics"
	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/darianmavgo/backtestgosqlite/internal/storage"
)

type DBTransaction struct {
	Date            string      `db:"date"`
	Account         string      `db:"account"`
	Description     string      `db:"description"`
	TransactionType string      `db:"transaction_type"`
	Symbol          string      `db:"symbol"`
	Quantity        interface{} `db:"quantity"`
	Price           interface{} `db:"price"`
	GrossAmount     interface{} `db:"gross_amount"`
	Commission      interface{} `db:"commission"`
	NetAmount       interface{} `db:"net_amount"`
}

type ParsedTx struct {
	Date            string
	Account         string
	Description     string
	TransactionType string
	Symbol          string
	Quantity        float64
	Price           float64
	GrossAmount     float64
	Commission      float64
	NetAmount       float64
	Time            time.Time
}

type BuyLot struct {
	Date       string
	Price      float64
	Shares     float64
	Commission float64
}

func parseMoney(s string) float64 {
	cleaned := strings.ReplaceAll(s, ",", "")
	cleaned = strings.ReplaceAll(cleaned, " USD", "")
	cleaned = strings.ReplaceAll(cleaned, "$", "")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" || cleaned == "-" {
		return 0
	}
	val, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0
	}
	return val
}

func parseAnyFloat(v interface{}) float64 {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int64:
		return float64(val)
	case int:
		return float64(val)
	case string:
		return parseMoney(val)
	case []byte:
		return parseMoney(string(val))
	default:
		return parseMoney(fmt.Sprintf("%v", v))
	}
}

func normalizeDate(d string) string {
	d = strings.TrimSpace(d)
	if d == "2025-11-" {
		return "2025-11-04"
	}
	if len(d) > 10 {
		return d[:10]
	}
	return d
}

func main() {
	dbPath := flag.String("db", "/Users/darianhickman/Documents/Income/transactions.db", "Path to IBKR transactions SQLite DB")
	priceDBPath := flag.String("prices", "data/ibkr_history.db", "Path to historical stock prices SQLite DB")
	startDateFlag := flag.String("start", "2025-10-01", "Start date filter (YYYY-MM-DD)")
	exportDir := flag.String("export", "/Users/darianhickman/Documents/Income", "Path to export reports, CSVs and metrics")
	htmlOutput := flag.String("html", "reports/ibkr_portfolio_report.html", "Path to export HTML report")
	flag.Parse()

	db, err := sqlx.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatalf("Failed to open transactions DB %s: %v", *dbPath, err)
	}
	defer db.Close()

	var rawTxs []DBTransaction
	query := "SELECT date, account, description, transaction_type, symbol, quantity, price, gross_amount, commission, net_amount FROM table0"
	if err := db.Select(&rawTxs, query); err != nil {
		log.Fatalf("Failed to query table0: %v", err)
	}

	var txs []ParsedTx
	for _, r := range rawTxs {
		dStr := normalizeDate(r.Date)
		if *startDateFlag != "" && dStr < *startDateFlag {
			continue
		}

		t, _ := time.Parse("2006-01-02", dStr)
		txType := strings.TrimSpace(r.TransactionType)
		if txType == "" && strings.Contains(r.Description, "Deposit") {
			txType = "Deposit"
		}

		p := parseAnyFloat(r.Price)
		gross := parseAnyFloat(r.GrossAmount)
		net := parseAnyFloat(r.NetAmount)
		comm := parseAnyFloat(r.Commission)
		qty := parseAnyFloat(r.Quantity)
		if comm > 0 {
			comm = -comm
		}

		sym := strings.ToUpper(strings.TrimSpace(r.Symbol))
		if sym == "-" {
			sym = ""
		}

		txs = append(txs, ParsedTx{
			Date:            dStr,
			Account:         r.Account,
			Description:     r.Description,
			TransactionType: txType,
			Symbol:          sym,
			Quantity:        qty,
			Price:           p,
			GrossAmount:     gross,
			Commission:      comm,
			NetAmount:       net,
			Time:            t,
		})
	}

	// Sort chronologically (oldest to newest)
	sort.Slice(txs, func(i, j int) bool {
		if txs[i].Date != txs[j].Date {
			return txs[i].Date < txs[j].Date
		}
		// Buys before sells on same date
		if txs[i].TransactionType == "Buy" && txs[j].TransactionType == "Sell" {
			return true
		}
		return false
	})

	fmt.Printf("\n========================================================================================\n")
	fmt.Printf("🏦 INTERACTIVE BROKERS ACCOUNT PORTFOLIO ANALYZER\n")
	fmt.Printf("📂 TRANSACTIONS DATABASE: %s\n", *dbPath)
	fmt.Printf("📅 ANALYSIS PERIOD:       %s ➔ %s (%d transactions parsed)\n",
		txs[0].Date, txs[len(txs)-1].Date, len(txs))
	fmt.Printf("========================================================================================\n\n")

	// Load historical price data for mark-to-market
	var priceDB *sqlx.DB
	barsBySymbol := make(map[string]map[string]float64)
	var allTradingDates []string
	if _, err := os.Stat(*priceDBPath); err == nil {
		priceDB, _ = storage.OpenSQLite(*priceDBPath)
		if priceDB != nil {
			defer priceDB.Close()
			barsMap, dates, _ := storage.FetchAllBarsChronological(priceDB)
			allTradingDates = dates
			for sym, bars := range barsMap {
				barsBySymbol[sym] = make(map[string]float64)
				for _, b := range bars {
					barsBySymbol[sym][b.Date] = b.Close
				}
			}
		}
	}

	// 1. Process FIFO trade accounting
	buyQueues := make(map[string][]BuyLot)
	var closedTrades []models.Trade
	tradeID := 0

	var totalDeposits float64
	var totalWithdrawals float64
	var totalDividends float64
	var totalInterestFees float64
	var initialDeposit float64
	firstDepositSeen := false

	runningCash := 0.0

	for _, tx := range txs {
		runningCash += tx.NetAmount

		switch tx.TransactionType {
		case "Deposit":
			if !firstDepositSeen {
				initialDeposit = tx.NetAmount
				firstDepositSeen = true
			}
			totalDeposits += tx.NetAmount
		case "Withdrawal":
			totalWithdrawals += math.Abs(tx.NetAmount)
		case "Dividend":
			totalDividends += tx.NetAmount
		case "Debit Interest", "Other Fee":
			totalInterestFees += math.Abs(tx.NetAmount)
		case "Buy":
			buyQueues[tx.Symbol] = append(buyQueues[tx.Symbol], BuyLot{
				Date:       tx.Date,
				Price:      tx.Price,
				Shares:     tx.Quantity,
				Commission: math.Abs(tx.Commission),
			})
		case "Sell":
			sellSharesToMatch := math.Abs(tx.Quantity)
			sellPrice := tx.Price
			sellComm := math.Abs(tx.Commission)

			for sellSharesToMatch > 0 && len(buyQueues[tx.Symbol]) > 0 {
				lot := &buyQueues[tx.Symbol][0]
				sharesMatched := math.Min(sellSharesToMatch, lot.Shares)

				tradeID++
				entryPrice := lot.Price
				investedCap := sharesMatched * entryPrice
				exitProceeds := sharesMatched * sellPrice

				// Allocate commissions proportionally
				buyCommPortion := 0.0
				if lot.Shares > 0 {
					buyCommPortion = (sharesMatched / lot.Shares) * lot.Commission
				}
				sellCommPortion := (sharesMatched / math.Abs(tx.Quantity)) * sellComm

				grossPnL := exitProceeds - investedCap
				totalComm := buyCommPortion + sellCommPortion
				netPnL := grossPnL - totalComm
				retPct := 0.0
				if investedCap > 0 {
					retPct = netPnL / investedCap
				}

				// Calculate hold days
				tEntry, _ := time.Parse("2006-01-02", lot.Date)
				tExit, _ := time.Parse("2006-01-02", tx.Date)
				holdDays := int(tExit.Sub(tEntry).Hours() / 24)
				if holdDays < 0 {
					holdDays = 0
				}

				closedTrades = append(closedTrades, models.Trade{
					ID:              tradeID,
					Symbol:          tx.Symbol,
					EntryDate:       lot.Date,
					EntryPrice:      entryPrice,
					ExitDate:        tx.Date,
					ExitPrice:       sellPrice,
					Shares:          int(sharesMatched),
					InvestedCapital: investedCap,
					GrossPnL:        grossPnL,
					NetPnL:          netPnL,
					ReturnPct:       retPct,
					HoldDays:        holdDays,
					CommissionPaid:  totalComm,
					ExitReason:      models.ExitReason("PROFIT_TAKING"),
				})

				sellSharesToMatch -= sharesMatched
				lot.Shares -= sharesMatched
				if lot.Shares <= 0.0001 {
					buyQueues[tx.Symbol] = buyQueues[tx.Symbol][1:]
				}
			}
		}
	}

	// 2. Open Positions remaining at end of period
	openPositionsValue := 0.0
	openPositionsCost := 0.0
	var openPositions []models.Position
	lastDate := txs[len(txs)-1].Date

	for sym, queue := range buyQueues {
		for _, lot := range queue {
			if lot.Shares <= 0.0001 {
				continue
			}
			currPrice := lot.Price
			if pMap, ok := barsBySymbol[sym]; ok {
				if cp, hasPrice := pMap[lastDate]; hasPrice && cp > 0 {
					currPrice = cp
				}
			}
			posVal := lot.Shares * currPrice
			posCost := lot.Shares * lot.Price
			openPositionsValue += posVal
			openPositionsCost += posCost

			openPositions = append(openPositions, models.Position{
				Symbol:        sym,
				Shares:        int(lot.Shares),
				EntryPrice:    lot.Price,
				EntryDate:     lot.Date,
				CurrentPrice:  currPrice,
				UnrealizedPnL: posVal - posCost,
			})
		}
	}

	endingEquity := runningCash + openPositionsValue
	netInvestedCapital := totalDeposits - totalWithdrawals
	netRealizedProfit := 0.0
	for _, t := range closedTrades {
		netRealizedProfit += t.NetPnL
	}

	// 3. Build Daily Equity Curve and Calculate Drawdowns
	// Map all cash flow events by date
	dailyEvents := make(map[string][]ParsedTx)
	for _, tx := range txs {
		dailyEvents[tx.Date] = append(dailyEvents[tx.Date], tx)
	}

	// Compile unique chronological calendar dates
	dateMap := make(map[string]bool)
	for _, tx := range txs {
		dateMap[tx.Date] = true
	}
	for _, d := range allTradingDates {
		if d >= txs[0].Date && d <= lastDate {
			dateMap[d] = true
		}
	}
	var sortedDates []string
	for d := range dateMap {
		sortedDates = append(sortedDates, d)
	}
	sort.Strings(sortedDates)

	var equityCurve []models.DailyEquityPoint
	currentCash := 0.0
	currentHoldingInventory := make(map[string]float64)
	peakEquity := initialDeposit
	maxDrawdownDollars := 0.0
	maxDrawdownPct := 0.0
	var mddPeakDate, mddTroughDate string
	var mddPeakEq, mddTroughEq float64
	currentDrawdownStart := sortedDates[0]
	currentDrawdownDays := 0
	maxDrawdownDuration := 0

	for _, d := range sortedDates {
		// Process any cash flows / trades on this date
		if dayTxs, ok := dailyEvents[d]; ok {
			for _, tx := range dayTxs {
				currentCash += tx.NetAmount
				if tx.TransactionType == "Buy" {
					currentHoldingInventory[tx.Symbol] += tx.Quantity
				} else if tx.TransactionType == "Sell" {
					currentHoldingInventory[tx.Symbol] -= math.Abs(tx.Quantity)
					if currentHoldingInventory[tx.Symbol] <= 0.0001 {
						delete(currentHoldingInventory, tx.Symbol)
					}
				}
			}
		}

		// Calculate market value of inventory on date d
		dayPosVal := 0.0
		for sym, qty := range currentHoldingInventory {
			p := 0.0
			if pMap, ok := barsBySymbol[sym]; ok {
				if cp, ok := pMap[d]; ok {
					p = cp
				}
			}
			if p == 0 {
				// Fallback to recent trade price
				for i := len(txs) - 1; i >= 0; i-- {
					if txs[i].Symbol == sym && txs[i].Date <= d && txs[i].Price > 0 {
						p = txs[i].Price
						break
					}
				}
			}
			dayPosVal += qty * p
		}

		dayEquity := currentCash + dayPosVal
		if dayEquity > peakEquity {
			peakEquity = dayEquity
			currentDrawdownStart = d
			currentDrawdownDays = 0
		} else {
			currentDrawdownDays++
			if currentDrawdownDays > maxDrawdownDuration {
				maxDrawdownDuration = currentDrawdownDays
			}
		}

		ddDollars := peakEquity - dayEquity
		ddPct := 0.0
		if peakEquity > 0 {
			ddPct = ddDollars / peakEquity
		}

		if ddPct > maxDrawdownPct {
			maxDrawdownPct = ddPct
			maxDrawdownDollars = ddDollars
			mddPeakDate = currentDrawdownStart
			mddTroughDate = d
			mddPeakEq = peakEquity
			mddTroughEq = dayEquity
		}

		equityCurve = append(equityCurve, models.DailyEquityPoint{
			Date:           d,
			Cash:           currentCash,
			PositionsValue: dayPosVal,
			TotalEquity:    dayEquity,
			DrawdownPct:    ddPct,
			OpenPositions:  len(currentHoldingInventory),
		})
	}

	// 4. Compute Quantitative Performance Metrics
	totalTradingDays := len(equityCurve)
	totalYears := float64(totalTradingDays) / 252.0
	if totalYears <= 0 {
		totalYears = float64(len(txs)) / 252.0
	}

	// Time-Weighted / Capital Gain
	totalReturnPct := (endingEquity - netInvestedCapital) / netInvestedCapital
	if initialDeposit > 0 && netInvestedCapital <= 0 {
		totalReturnPct = netRealizedProfit / initialDeposit
	}
	cagr := math.Pow(1.0+math.Max(-0.99, totalReturnPct), 1.0/math.Max(0.1, totalYears)) - 1.0

	// Trade Stats
	winningTrades := 0
	losingTrades := 0
	grossWins := 0.0
	grossLosses := 0.0
	totalHoldDays := 0

	for _, t := range closedTrades {
		totalHoldDays += t.HoldDays
		if t.NetPnL > 0 {
			winningTrades++
			grossWins += t.NetPnL
		} else {
			losingTrades++
			grossLosses += math.Abs(t.NetPnL)
		}
	}

	totalTradesCount := len(closedTrades)
	winRate := 0.0
	if totalTradesCount > 0 {
		winRate = float64(winningTrades) / float64(totalTradesCount)
	}

	profitFactor := 0.0
	if grossLosses > 0 {
		profitFactor = grossWins / grossLosses
	} else if grossWins > 0 {
		profitFactor = 99.0
	}

	avgWin := 0.0
	if winningTrades > 0 {
		avgWin = grossWins / float64(winningTrades)
	}
	avgLoss := 0.0
	if losingTrades > 0 {
		avgLoss = grossLosses / float64(losingTrades)
	}
	avgHoldingDays := 0.0
	if totalTradesCount > 0 {
		avgHoldingDays = float64(totalHoldDays) / float64(totalTradesCount)
	}

	// Compute Daily Return Volatility, Sharpe, Sortino
	var dailyReturns []float64
	for i := 1; i < len(equityCurve); i++ {
		prevEq := equityCurve[i-1].TotalEquity
		currEq := equityCurve[i].TotalEquity
		if prevEq > 0 {
			dailyReturns = append(dailyReturns, (currEq-prevEq)/prevEq)
		}
	}

	meanDailyRet := 0.0
	for _, r := range dailyReturns {
		meanDailyRet += r
	}
	if len(dailyReturns) > 0 {
		meanDailyRet /= float64(len(dailyReturns))
	}

	varSum := 0.0
	downsideVarSum := 0.0
	for _, r := range dailyReturns {
		diff := r - meanDailyRet
		varSum += diff * diff
		if r < 0 {
			downsideVarSum += r * r
		}
	}

	dailyVol := 0.0
	if len(dailyReturns) > 1 {
		dailyVol = math.Sqrt(varSum / float64(len(dailyReturns)-1))
	}
	annualizedVol := dailyVol * math.Sqrt(252.0)

	downsideDev := 0.0
	if len(dailyReturns) > 1 {
		downsideDev = math.Sqrt(downsideVarSum / float64(len(dailyReturns)-1))
	}
	annualizedDownsideDev := downsideDev * math.Sqrt(252.0)

	sharpeRatio := 0.0
	if annualizedVol > 0 {
		sharpeRatio = (cagr) / annualizedVol
	}

	sortinoRatio := 0.0
	if annualizedDownsideDev > 0 {
		sortinoRatio = (cagr) / annualizedDownsideDev
	}

	calmarRatio := 0.0
	if maxDrawdownPct > 0 {
		calmarRatio = cagr / maxDrawdownPct
	}

	rep := models.PerformanceReport{
		StartDate:               txs[0].Date,
		EndDate:                 lastDate,
		TotalTradingDays:        totalTradingDays,
		TotalCalendarYears:      totalYears,
		InitialCapital:          initialDeposit,
		FinalEquity:             endingEquity,
		NetProfit:               netRealizedProfit,
		TotalReturnPct:          totalReturnPct,
		CAGR:                    cagr,
		SharpeRatio:             sharpeRatio,
		SortinoRatio:            sortinoRatio,
		CalmarRatio:             calmarRatio,
		MaxDrawdownPct:          maxDrawdownPct,
		MaxDrawdownDollars:      maxDrawdownDollars,
		MaxDrawdownPeakEquity:   mddPeakEq,
		MaxDrawdownTroughEquity: mddTroughEq,
		MaxDrawdownPeakDate:     mddPeakDate,
		MaxDrawdownTroughDate:   mddTroughDate,
		MaxDrawdownDuration:     maxDrawdownDuration,
		TotalTrades:             totalTradesCount,
		WinningTrades:           winningTrades,
		LosingTrades:            losingTrades,
		WinRate:                 winRate,
		ProfitFactor:            profitFactor,
		AvgWinAmount:            avgWin,
		AvgLossAmount:           avgLoss,
		PayoffRatio:             avgWin / math.Max(0.01, avgLoss),
		AvgHoldingDays:          avgHoldingDays,
		TotalCommissionPaid:     totalInterestFees,
	}

	// 5. Render CLI Tear Sheet
	fmt.Printf("📊 QUANTITATIVE PORTFOLIO TEAR SHEET: INTERACTIVE BROKERS LIVE ACCOUNT\n")
	fmt.Printf("📅 TIME WINDOW: %s ➔ %s (%.2f Years | %d Trading Days)\n",
		txs[0].Date, lastDate, totalYears, totalTradingDays)
	fmt.Printf("========================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Metric", "Value", "Institutional Context / Details"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	table.Append([]string{"Initial Capital (First Deposit)", fmt.Sprintf("$%.2f", initialDeposit), "Starting account cash"})
	table.Append([]string{"Cumulative Deposits", fmt.Sprintf("$%.2f", totalDeposits), "Total capital deposited"})
	table.Append([]string{"Cumulative Withdrawals", fmt.Sprintf("$%.2f", totalWithdrawals), "Total capital withdrawn"})
	table.Append([]string{"Net Invested Capital", fmt.Sprintf("$%.2f", netInvestedCapital), "Deposits minus Withdrawals"})
	table.Append([]string{"Ending Total Equity", fmt.Sprintf("$%.2f", endingEquity), fmt.Sprintf("Cash: $%.2f + Open: $%.2f", runningCash, openPositionsValue)})
	table.Append([]string{"Net Realized Profit", fmt.Sprintf("$%.2f", netRealizedProfit), fmt.Sprintf("%.2f%% on net invested capital", totalReturnPct*100)})
	table.Append([]string{"Total Return %", fmt.Sprintf("%.2f%%", totalReturnPct*100), "Time-weighted cumulative gain"})
	table.Append([]string{"CAGR (Annualized Return)", fmt.Sprintf("%.2f%%", cagr*100), "Compound Annual Growth Rate"})
	table.Append([]string{"Sharpe Ratio (Annualized)", fmt.Sprintf("%.2f", sharpeRatio), "Risk-adjusted return vs. 0% Rf"})
	table.Append([]string{"Sortino Ratio (Annualized)", fmt.Sprintf("%.2f", sortinoRatio), "Downside volatility adjusted"})
	table.Append([]string{"Calmar Ratio", fmt.Sprintf("%.2f", calmarRatio), "CAGR / Max Drawdown"})
	table.Append([]string{"🔴 MAX DRAWDOWN (MDD %)", fmt.Sprintf("%.2f%%", maxDrawdownPct*100), "Worst peak-to-trough account decline"})
	table.Append([]string{"🔴 MAX DRAWDOWN ($ LOSS)", fmt.Sprintf("-$%.2f", maxDrawdownDollars), fmt.Sprintf("Peak: $%.2f ➔ Trough: $%.2f", mddPeakEq, mddTroughEq)})
	table.Append([]string{"🔴 MAX DRAWDOWN DATES", fmt.Sprintf("%s ➔ %s", mddPeakDate, mddTroughDate), fmt.Sprintf("Longest drawdown duration: %d days", maxDrawdownDuration)})
	table.Append([]string{"Total Completed Trades", fmt.Sprintf("%d", totalTradesCount), fmt.Sprintf("%d Wins / %d Losses", winningTrades, losingTrades)})
	table.Append([]string{"Trade Win Rate", fmt.Sprintf("%.2f%%", winRate*100), "Percentage of closed trades in profit"})
	table.Append([]string{"Profit Factor", fmt.Sprintf("%.2f", profitFactor), "Gross Profits / Gross Losses"})
	table.Append([]string{"Win / Loss Payoff Ratio", fmt.Sprintf("%.2f", avgWin/math.Max(0.01, avgLoss)), "Avg Win $ / Avg Loss $"})
	table.Append([]string{"Average Win", fmt.Sprintf("$%.2f", avgWin), "Per winning round-trip trade"})
	table.Append([]string{"Average Loss", fmt.Sprintf("$%.2f", avgLoss), "Per losing round-trip trade"})
	table.Append([]string{"Average Holding Period", fmt.Sprintf("%.1f days", avgHoldingDays), "Average holding horizon"})
	table.Append([]string{"Total Dividends Received", fmt.Sprintf("$%.2f", totalDividends), "Cash dividends credited"})
	table.Append([]string{"Total Interest & Broker Fees", fmt.Sprintf("$%.2f", totalInterestFees), "Financing / debit interest deducted"})

	table.Render()

	// 6. Recent Trades CLI Sample
	if len(closedTrades) > 0 {
		fmt.Printf("\nMost Recent Completed Trades (Sample of last 20 of %d trades):\n", len(closedTrades))
		tTable := tablewriter.NewWriter(os.Stdout)
		tTable.SetHeader([]string{"ID", "Symbol", "Entry Date", "Entry $", "Exit Date", "Exit $", "Shares", "Hold Days", "Net PnL", "Return %"})
		tTable.SetBorder(true)

		startIdx := 0
		if len(closedTrades) > 20 {
			startIdx = len(closedTrades) - 20
		}
		for _, t := range closedTrades[startIdx:] {
			tTable.Append([]string{
				fmt.Sprintf("%d", t.ID),
				t.Symbol,
				t.EntryDate,
				fmt.Sprintf("$%.2f", t.EntryPrice),
				t.ExitDate,
				fmt.Sprintf("$%.2f", t.ExitPrice),
				fmt.Sprintf("%d", t.Shares),
				fmt.Sprintf("%dd", t.HoldDays),
				fmt.Sprintf("$%.2f", t.NetPnL),
				fmt.Sprintf("%.2f%%", t.ReturnPct*100),
			})
		}
		tTable.Render()
	}

	// 7. Open Positions CLI
	if len(openPositions) > 0 {
		fmt.Printf("\nCurrently Open Positions (as of %s):\n", lastDate)
		posTable := tablewriter.NewWriter(os.Stdout)
		posTable.SetHeader([]string{"Symbol", "Shares", "Entry Date", "Entry Price", "Current Price", "Unrealized PnL"})
		posTable.SetBorder(true)
		for _, p := range openPositions {
			posTable.Append([]string{
				p.Symbol,
				fmt.Sprintf("%d", p.Shares),
				p.EntryDate,
				fmt.Sprintf("$%.2f", p.EntryPrice),
				fmt.Sprintf("$%.2f", p.CurrentPrice),
				fmt.Sprintf("$%.2f", p.UnrealizedPnL),
			})
		}
		posTable.Render()
	}

	// 8. EXPORT FILES TO INCOME FOLDER
	if *exportDir != "" {
		_ = os.MkdirAll(*exportDir, 0755)
		fmt.Printf("\n📁 EXPORTING METRICS & REPORTS TO: %s\n", *exportDir)

		// A. Summary Metrics CSV
		summaryCSVPath := filepath.Join(*exportDir, "ibkr_summary_metrics.csv")
		if f, err := os.Create(summaryCSVPath); err == nil {
			w := csv.NewWriter(f)
			_ = w.Write([]string{"Metric", "Value", "Context"})
			_ = w.Write([]string{"Initial Capital", fmt.Sprintf("%.2f", initialDeposit), "Starting account cash"})
			_ = w.Write([]string{"Cumulative Deposits", fmt.Sprintf("%.2f", totalDeposits), "Total capital deposited"})
			_ = w.Write([]string{"Cumulative Withdrawals", fmt.Sprintf("%.2f", totalWithdrawals), "Total capital withdrawn"})
			_ = w.Write([]string{"Net Invested Capital", fmt.Sprintf("%.2f", netInvestedCapital), "Deposits minus Withdrawals"})
			_ = w.Write([]string{"Ending Total Equity", fmt.Sprintf("%.2f", endingEquity), "Cash plus open positions"})
			_ = w.Write([]string{"Net Realized Profit", fmt.Sprintf("%.2f", netRealizedProfit), "Realized trade gains minus commissions"})
			_ = w.Write([]string{"Total Return Pct", fmt.Sprintf("%.4f", totalReturnPct), "Net return on capital base"})
			_ = w.Write([]string{"CAGR", fmt.Sprintf("%.4f", cagr), "Compound Annual Growth Rate"})
			_ = w.Write([]string{"Sharpe Ratio", fmt.Sprintf("%.4f", sharpeRatio), "Risk-adjusted return vs 0% Rf"})
			_ = w.Write([]string{"Sortino Ratio", fmt.Sprintf("%.4f", sortinoRatio), "Downside volatility adjusted"})
			_ = w.Write([]string{"Calmar Ratio", fmt.Sprintf("%.4f", calmarRatio), "CAGR / Max Drawdown"})
			_ = w.Write([]string{"Max Drawdown Pct", fmt.Sprintf("%.4f", maxDrawdownPct), "Worst peak-to-trough decline"})
			_ = w.Write([]string{"Max Drawdown Dollars", fmt.Sprintf("%.2f", maxDrawdownDollars), "Largest dollar drop"})
			_ = w.Write([]string{"Total Completed Trades", fmt.Sprintf("%d", totalTradesCount), "Round trip trades"})
			_ = w.Write([]string{"Winning Trades", fmt.Sprintf("%d", winningTrades), "Profitable closed trades"})
			_ = w.Write([]string{"Losing Trades", fmt.Sprintf("%d", losingTrades), "Losing closed trades"})
			_ = w.Write([]string{"Trade Win Rate", fmt.Sprintf("%.4f", winRate), "Percentage of winning trades"})
			_ = w.Write([]string{"Profit Factor", fmt.Sprintf("%.4f", profitFactor), "Gross profits / Gross losses"})
			_ = w.Write([]string{"Win Loss Payoff Ratio", fmt.Sprintf("%.4f", avgWin/math.Max(0.01, avgLoss)), "Avg Win / Avg Loss"})
			_ = w.Write([]string{"Average Win", fmt.Sprintf("%.2f", avgWin), "Per winning trade"})
			_ = w.Write([]string{"Average Loss", fmt.Sprintf("%.2f", avgLoss), "Per losing trade"})
			_ = w.Write([]string{"Average Holding Period", fmt.Sprintf("%.1f", avgHoldingDays), "Days"})
			_ = w.Write([]string{"Total Dividends Received", fmt.Sprintf("%.2f", totalDividends), "Cash dividends"})
			_ = w.Write([]string{"Total Interest and Fees", fmt.Sprintf("%.2f", totalInterestFees), "Financing / debit fees"})
			w.Flush()
			f.Close()
			fmt.Printf("   ✅ Exported: %s\n", summaryCSVPath)
		}

		// B. Closed Trades CSV
		tradesCSVPath := filepath.Join(*exportDir, "ibkr_closed_trades.csv")
		if f, err := os.Create(tradesCSVPath); err == nil {
			w := csv.NewWriter(f)
			_ = w.Write([]string{"Trade_ID", "Symbol", "Entry_Date", "Entry_Price", "Exit_Date", "Exit_Price", "Shares", "Invested_Capital", "Gross_PnL", "Net_PnL", "Return_Pct", "Hold_Days", "Commission_Paid", "Outcome"})
			for _, t := range closedTrades {
				outcome := "WIN"
				if t.NetPnL <= 0 {
					outcome = "LOSS"
				}
				_ = w.Write([]string{
					strconv.Itoa(t.ID),
					t.Symbol,
					t.EntryDate,
					fmt.Sprintf("%.2f", t.EntryPrice),
					t.ExitDate,
					fmt.Sprintf("%.2f", t.ExitPrice),
					strconv.Itoa(t.Shares),
					fmt.Sprintf("%.2f", t.InvestedCapital),
					fmt.Sprintf("%.2f", t.GrossPnL),
					fmt.Sprintf("%.2f", t.NetPnL),
					fmt.Sprintf("%.4f", t.ReturnPct),
					strconv.Itoa(t.HoldDays),
					fmt.Sprintf("%.2f", t.CommissionPaid),
					outcome,
				})
			}
			w.Flush()
			f.Close()
			fmt.Printf("   ✅ Exported: %s (%d trades)\n", tradesCSVPath, len(closedTrades))
		}

		// C. Daily Equity Curve CSV
		eqCSVPath := filepath.Join(*exportDir, "ibkr_daily_equity_curve.csv")
		if f, err := os.Create(eqCSVPath); err == nil {
			w := csv.NewWriter(f)
			_ = w.Write([]string{"Date", "Cash", "Positions_Value", "Total_Equity", "Drawdown_Pct", "Open_Positions_Count"})
			for _, pt := range equityCurve {
				_ = w.Write([]string{
					pt.Date,
					fmt.Sprintf("%.2f", pt.Cash),
					fmt.Sprintf("%.2f", pt.PositionsValue),
					fmt.Sprintf("%.2f", pt.TotalEquity),
					fmt.Sprintf("%.4f", pt.DrawdownPct),
					strconv.Itoa(pt.OpenPositions),
				})
			}
			w.Flush()
			f.Close()
			fmt.Printf("   ✅ Exported: %s (%d daily records)\n", eqCSVPath, len(equityCurve))
		}

		// D. JSON Metrics Export
		jsonMetricsPath := filepath.Join(*exportDir, "ibkr_performance_metrics.json")
		exportPayload := map[string]interface{}{
			"report":         rep,
			"open_positions": openPositions,
			"summary": map[string]interface{}{
				"total_deposits":     totalDeposits,
				"total_withdrawals":  totalWithdrawals,
				"net_capital_base":   netInvestedCapital,
				"ending_cash":        runningCash,
				"open_equity":        openPositionsValue,
				"total_equity":       endingEquity,
				"net_realized_pnl":   netRealizedProfit,
				"total_dividends":    totalDividends,
				"total_interest_fee": totalInterestFees,
			},
		}
		if jsonBytes, err := json.MarshalIndent(exportPayload, "", "  "); err == nil {
			_ = os.WriteFile(jsonMetricsPath, jsonBytes, 0644)
			fmt.Printf("   ✅ Exported: %s\n", jsonMetricsPath)
		}

		// E. Markdown Tear Sheet
		mdPath := filepath.Join(*exportDir, "ibkr_metrics_tear_sheet.md")
		mdContent := fmt.Sprintf(`# 📊 Interactive Brokers Live Portfolio Performance Tear Sheet

**Analysis Period**: %s ➔ %s (%.2f Years | %d Trading Days)  
**Data Source**: [%s](file://%s)

---

## 1. Executive Performance Overview

| Metric | Value | Institutional Benchmark / Context |
| :--- | :--- | :--- |
| **Initial Capital** | **$%.2f** | First account deposit (%s) |
| **Cumulative Deposits** | **$%.2f** | Total capital injected |
| **Cumulative Withdrawals** | **$%.2f** | Total disbursements |
| **Net Invested Capital** | **$%.2f** | Deposits minus Withdrawals |
| **Ending Total Equity** | **$%.2f** | Cash ($%.2f) + Open Positions ($%.2f) |
| **Net Realized Profit** | **+$%.2f** | Realized trading gains after broker fees |
| **Total Return %%** | **%.2f%%%%** | Time-weighted cumulative return on net capital |
| **CAGR (Annualized Return)** | **%.2f%%%%** | Compound Annual Growth Rate |
| **Sharpe Ratio (Annualized)** | **%.2f** | Risk-adjusted return (Rf = 0%%) |
| **Sortino Ratio (Annualized)** | **%.2f** | Downside volatility adjusted return |
| **Calmar Ratio** | **%.2f** | CAGR / Maximum Drawdown |
| **🔴 Max Drawdown (MDD %%)** | **%.2f%%%%** | Worst peak-to-trough account decline |
| **🔴 Max Drawdown ($$ Loss)** | **-$%.2f** | Peak: $%.2f ➔ Trough: $%.2f |
| **🔴 Max Drawdown Dates** | **%s ➔ %s** | Duration: %d days |
| **Total Completed Trades** | **%d** | **%d Wins / %d Losses** |
| **Trade Win Rate** | **%.2f%%%%** | Percentage of round-trip trades closed in profit |
| **Profit Factor** | **%.2f** | Gross Profits ($%.2f) / Gross Losses ($%.2f) |
| **Win / Loss Payoff Ratio** | **%.2f** | Avg Win ($%.2f) / Avg Loss ($%.2f) |
| **Average Holding Period** | **%.1f days** | Average duration from entry to exit |
| **Total Dividends Received** | **$%.2f** | Cash dividends credited |
| **Total Financing & Broker Fees** | **$%.2f** | Debit interest & transfer fees deducted |

---

## 2. Currently Open Positions (as of %s)

| Symbol | Shares | Entry Date | Entry Price | Current Price | Unrealized PnL |
| :--- | :--- | :--- | :--- | :--- | :--- |
`,
			txs[0].Date, lastDate, totalYears, totalTradingDays,
			*dbPath, *dbPath,
			initialDeposit, txs[0].Date,
			totalDeposits, totalWithdrawals, netInvestedCapital,
			endingEquity, runningCash, openPositionsValue,
			netRealizedProfit, totalReturnPct*100,
			cagr*100, sharpeRatio, sortinoRatio, calmarRatio,
			maxDrawdownPct*100, maxDrawdownDollars, mddPeakEq, mddTroughEq,
			mddPeakDate, mddTroughDate, maxDrawdownDuration,
			totalTradesCount, winningTrades, losingTrades,
			winRate*100, profitFactor, grossWins, grossLosses,
			avgWin/math.Max(0.01, avgLoss), avgWin, avgLoss,
			avgHoldingDays, totalDividends, totalInterestFees,
			lastDate,
		)

		for _, p := range openPositions {
			mdContent += fmt.Sprintf("| **%s** | %d | %s | $%.2f | $%.2f | $%.2f |\n",
				p.Symbol, p.Shares, p.EntryDate, p.EntryPrice, p.CurrentPrice, p.UnrealizedPnL)
		}

		_ = os.WriteFile(mdPath, []byte(mdContent), 0644)
		fmt.Printf("   ✅ Exported: %s\n", mdPath)

		// F. Save SQLite Tables directly into transactions.db
		_, _ = db.Exec(`
			CREATE TABLE IF NOT EXISTS closed_trades (
				id INTEGER PRIMARY KEY,
				symbol TEXT,
				entry_date TEXT,
				entry_price REAL,
				exit_date TEXT,
				exit_price REAL,
				shares INTEGER,
				invested_capital REAL,
				gross_pnl REAL,
				net_pnl REAL,
				return_pct REAL,
				hold_days INTEGER,
				commission_paid REAL,
				outcome TEXT
			);
			DELETE FROM closed_trades;
		`)

		txDB, _ := db.Begin()
		if txDB != nil {
			stmt, err := txDB.Prepare(`
				INSERT INTO closed_trades (
					id, symbol, entry_date, entry_price, exit_date, exit_price,
					shares, invested_capital, gross_pnl, net_pnl, return_pct,
					hold_days, commission_paid, outcome
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			`)
			if err == nil {
				for _, t := range closedTrades {
					outcome := "WIN"
					if t.NetPnL <= 0 {
						outcome = "LOSS"
					}
					_, _ = stmt.Exec(
						t.ID, t.Symbol, t.EntryDate, t.EntryPrice, t.ExitDate, t.ExitPrice,
						t.Shares, t.InvestedCapital, t.GrossPnL, t.NetPnL, t.ReturnPct,
						t.HoldDays, t.CommissionPaid, outcome,
					)
				}
				stmt.Close()
				_ = txDB.Commit()
				fmt.Printf("   ✅ Persisted: 'closed_trades' table into %s\n", *dbPath)
			}
		}

		_, _ = db.Exec(`
			CREATE TABLE IF NOT EXISTS daily_equity_curve (
				date TEXT PRIMARY KEY,
				cash REAL,
				positions_value REAL,
				total_equity REAL,
				drawdown_pct REAL,
				open_positions INTEGER
			);
			DELETE FROM daily_equity_curve;
		`)

		txEq, _ := db.Begin()
		if txEq != nil {
			stmt, err := txEq.Prepare(`
				INSERT INTO daily_equity_curve (date, cash, positions_value, total_equity, drawdown_pct, open_positions)
				VALUES (?, ?, ?, ?, ?, ?)
			`)
			if err == nil {
				for _, pt := range equityCurve {
					_, _ = stmt.Exec(pt.Date, pt.Cash, pt.PositionsValue, pt.TotalEquity, pt.DrawdownPct, pt.OpenPositions)
				}
				stmt.Close()
				_ = txEq.Commit()
				fmt.Printf("   ✅ Persisted: 'daily_equity_curve' table into %s\n", *dbPath)
			}
		}

		// G. Generate Interactive HTML Report in Income Folder as well
		incomeHTMLPath := filepath.Join(*exportDir, "ibkr_portfolio_report.html")
		var eqSeries, ddSeries []float64
		for _, pt := range equityCurve {
			eqSeries = append(eqSeries, pt.TotalEquity)
			ddSeries = append(ddSeries, pt.DrawdownPct)
		}

		htmlData := analytics.MultiStrategyHTMLData{
			Title:      "Interactive Brokers Live Account Performance Report",
			Symbol:     "IBKR Portfolio",
			StartDate:  rep.StartDate,
			EndDate:    rep.EndDate,
			TotalDays:  rep.TotalTradingDays,
			TotalYears: rep.TotalCalendarYears,
			InitialCap: rep.InitialCapital,
			Strategies: []analytics.StrategyReportData{
				{
					ID:     "ibkr-live",
					Name:   "Interactive Brokers Live Account",
					Type:   "Live Portfolio",
					Report: rep,
					Trades: closedTrades,
				},
			},
			AllDates: sortedDates,
			EquityCurves: map[string][]float64{
				"ibkr-live": eqSeries,
			},
			DrawdownCurves: map[string][]float64{
				"ibkr-live": ddSeries,
			},
		}

		_ = analytics.GenerateComparisonHTML(incomeHTMLPath, htmlData)
		fmt.Printf("   ✅ Exported: %s\n", incomeHTMLPath)
		if *htmlOutput != "" && *htmlOutput != incomeHTMLPath {
			_ = analytics.GenerateComparisonHTML(*htmlOutput, htmlData)
		}
	}
}
