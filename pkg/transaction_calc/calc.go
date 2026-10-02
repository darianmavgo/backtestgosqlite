package transaction_calc

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

type TradeRecord struct {
	Symbol   string
	Date     string
	Quantity float64 // positive for buy, negative for sell
	Price    float64
	Comm     float64
}

// ParseIBKR parses an IBKR Activity Statement CSV.
func ParseIBKR(path string) ([]models.Trade, []models.EquityPoint, float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, 0, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // Allow variable number of fields
	r.LazyQuotes = true

	var tradeRecords []TradeRecord
	var equityPoints []models.EquityPoint

	var tradesHeader map[string]int
	var navHeader map[string]int
	var mtmHeader map[string]int

	var initialCash float64

	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue // Skip bad rows
		}

		if len(rec) < 3 {
			continue
		}

		section := rec[0]
		rowType := rec[1]

		if section == "Trades" {
			if rowType == "Header" {
				tradesHeader = make(map[string]int)
				for i, col := range rec {
					tradesHeader[col] = i
				}
			} else if rowType == "Data" {
				if tradesHeader == nil {
					continue
				}
				symIdx, ok1 := tradesHeader["Symbol"]
				dateIdx, ok2 := tradesHeader["Date/Time"]
				qtyIdx, ok3 := tradesHeader["Quantity"]
				priceIdx, ok4 := tradesHeader["T. Price"]
				commIdx, ok5 := tradesHeader["Comm/Fee"]
				if !(ok1 && ok2 && ok3 && ok4 && ok5) {
					continue
				}
				if symIdx >= len(rec) || dateIdx >= len(rec) || qtyIdx >= len(rec) || priceIdx >= len(rec) || commIdx >= len(rec) {
					continue
				}
				
				// Exclude summary rows (often they lack Date/Time)
				if rec[dateIdx] == "" || strings.HasPrefix(rec[dateIdx], "Total") {
					continue
				}

				qtyStr := strings.ReplaceAll(rec[qtyIdx], ",", "")
				qty, err := strconv.ParseFloat(qtyStr, 64)
				if err != nil || qty == 0 {
					continue
				}

				price, _ := strconv.ParseFloat(strings.ReplaceAll(rec[priceIdx], ",", ""), 64)
				comm, _ := strconv.ParseFloat(strings.ReplaceAll(rec[commIdx], ",", ""), 64)

				dateStr := strings.Split(rec[dateIdx], ",")[0]
				if len(dateStr) > 10 {
					dateStr = dateStr[:10]
				}

				tradeRecords = append(tradeRecords, TradeRecord{
					Symbol:   rec[symIdx],
					Date:     dateStr,
					Quantity: qty,
					Price:    price,
					Comm:     comm,
				})
			}
		} else if section == "Net Asset Value" {
			if rowType == "Header" {
				navHeader = make(map[string]int)
				for i, col := range rec {
					navHeader[col] = i
				}
			} else if rowType == "Data" {
				if navHeader == nil {
					continue
				}
				assetClassIdx := navHeader["Asset Class"]
				dateIdx := navHeader["Date"] // Could be "Field Name" in some exports? Let's check typical NAV.
				totalIdx := navHeader["Total"]
				if totalIdx == 0 {
					continue // not found
				}

				// If dateIdx is missing, it might be horizontal NAV. But usually it's "Net Asset Value", "Data", "Total", ...
				// Let's assume standard vertical NAV if dateIdx exists.
				if dateIdx > 0 && dateIdx < len(rec) && totalIdx < len(rec) {
					if assetClassIdx > 0 && assetClassIdx < len(rec) && rec[assetClassIdx] != "Total" {
						continue // Only take the total NAV
					}
					dateStr := rec[dateIdx]
					if len(dateStr) > 10 {
						dateStr = dateStr[:10]
					}
					total, err := strconv.ParseFloat(strings.ReplaceAll(rec[totalIdx], ",", ""), 64)
					if err == nil {
						equityPoints = append(equityPoints, models.EquityPoint{
							Date:        dateStr,
							TotalEquity: total,
						})
					}
				}
			}
		} else if section == "Mark-to-Market Performance Summary" {
			if rowType == "Header" {
				mtmHeader = make(map[string]int)
				for i, col := range rec {
					mtmHeader[col] = i
				}
			} else if rowType == "Data" {
				if mtmHeader == nil {
					continue
				}
				dateIdx := mtmHeader["Date"]
				navIdx := mtmHeader["Total"] // or "NAV"
				if dateIdx > 0 && navIdx > 0 && dateIdx < len(rec) && navIdx < len(rec) {
					dateStr := rec[dateIdx]
					if len(dateStr) > 10 {
						dateStr = dateStr[:10]
					}
					nav, err := strconv.ParseFloat(strings.ReplaceAll(rec[navIdx], ",", ""), 64)
					if err == nil {
						equityPoints = append(equityPoints, models.EquityPoint{
							Date:        dateStr,
							TotalEquity: nav,
						})
					}
				}
			}
		}
	}

	// Match trades FIFO
	openLots := make(map[string][]TradeRecord)
	var closedTrades []models.Trade

	// Sort trade records by date just in case
	sort.Slice(tradeRecords, func(i, j int) bool {
		return tradeRecords[i].Date < tradeRecords[j].Date
	})

	for _, rec := range tradeRecords {
		qty := rec.Quantity
		lots := openLots[rec.Symbol]

		for math.Abs(qty) > 0.0001 && len(lots) > 0 {
			lot := lots[0]
			// Check if same direction - if so, just add to lots and break to handle below
			if (lot.Quantity > 0 && qty > 0) || (lot.Quantity < 0 && qty < 0) {
				break
			}

			// Opposing direction - match
			matchedQty := math.Min(math.Abs(lot.Quantity), math.Abs(qty))
			
			// If lot was long and we are selling (qty < 0)
			entryPrice := lot.Price
			exitPrice := rec.Price
			direction := "LONG"
			if lot.Quantity < 0 {
				direction = "SHORT"
			}

			grossPnl := (exitPrice - entryPrice) * matchedQty
			if direction == "SHORT" {
				grossPnl = (entryPrice - exitPrice) * matchedQty
			}

			// proportional commission
			entryComm := lot.Comm * (matchedQty / math.Abs(lot.Quantity))
			exitComm := rec.Comm * (matchedQty / math.Abs(rec.Quantity))
			netPnl := grossPnl + entryComm + exitComm // comms are usually negative in IBKR

			entryTime, _ := time.Parse("2006-01-02", lot.Date)
			exitTime, _ := time.Parse("2006-01-02", rec.Date)
			holdDays := int(exitTime.Sub(entryTime).Hours() / 24)
			if holdDays < 0 {
				holdDays = 0
			}

			closedTrades = append(closedTrades, models.Trade{
				Symbol:          rec.Symbol,
				EntryDate:       lot.Date,
				ExitDate:        rec.Date,
				EntryPrice:      entryPrice,
				ExitPrice:       exitPrice,
				Shares:          int(matchedQty),
				InvestedCapital: entryPrice * matchedQty,
				GrossPnL:        grossPnl,
				NetPnL:          netPnl,
				CommissionPaid:  math.Abs(entryComm + exitComm),
				HoldDays:        holdDays,
				ExitReason:      models.ExitReasonSignal, // Default to Signal
			})

			// adjust quantities
			if qty > 0 {
				qty -= matchedQty
			} else {
				qty += matchedQty
			}
			
			if lot.Quantity > 0 {
				lot.Quantity -= matchedQty
			} else {
				lot.Quantity += matchedQty
			}

			if math.Abs(lot.Quantity) < 0.0001 {
				lots = lots[1:] // remove consumed lot
			} else {
				lots[0] = lot // update remaining
			}
		}

		if math.Abs(qty) > 0.0001 {
			lots = append(lots, TradeRecord{
				Symbol:   rec.Symbol,
				Date:     rec.Date,
				Quantity: qty,
				Price:    rec.Price,
				Comm:     rec.Comm, // Keep full comm, will be proportioned
			})
		}
		
		openLots[rec.Symbol] = lots
	}

	// Calculate ReturnPct for closed trades
	for i := range closedTrades {
		if closedTrades[i].InvestedCapital > 0 {
			closedTrades[i].ReturnPct = (closedTrades[i].NetPnL / closedTrades[i].InvestedCapital) * 100
		}
	}

	// Sort equity points by date
	sort.Slice(equityPoints, func(i, j int) bool {
		return equityPoints[i].Date < equityPoints[j].Date
	})

	if len(equityPoints) > 0 {
		initialCash = equityPoints[0].TotalEquity
	} else {
		initialCash = 100000 // Fallback
	}

	// De-duplicate equity points by date (take last)
	var deduped []models.EquityPoint
	var lastDate string
	for _, ep := range equityPoints {
		if ep.Date != lastDate {
			deduped = append(deduped, ep)
			lastDate = ep.Date
		} else {
			deduped[len(deduped)-1] = ep
		}
	}
	equityPoints = deduped

	return closedTrades, equityPoints, initialCash, nil
}
