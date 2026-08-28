// Package broker handles Interactive Brokers order ticket generation, bracket orders, and API formatting.
package broker

import (
	"encoding/json"
	"fmt"
	"math"
)

// BracketOrderTicket encapsulates a full parent-child bracket order for Interactive Brokers.
type BracketOrderTicket struct {
	Symbol          string  `json:"symbol"`
	Direction       string  `json:"direction"` // "LONG_TECL" or "SHORT_SPXU"
	Shares          int     `json:"shares"`
	EstEntryPrice   float64 `json:"est_entry_price"`
	TotalCost       float64 `json:"total_cost"`
	AccountNAV      float64 `json:"account_nav"`
	AllocationPct   float64 `json:"allocation_pct"`
	TargetPrice     float64 `json:"target_price"`
	TargetPct       float64 `json:"target_pct"`
	StopPrice       float64 `json:"stop_price"`
	StopPct         float64 `json:"stop_pct"`
	MaxHoldDays     int     `json:"max_hold_days"`
	OCOGroupName    string  `json:"oco_group_name"`
	TWSInstructions string  `json:"tws_instructions"`
	ClientPortalAPI string  `json:"client_portal_api_json"`
}

// BuildBracketTicket calculates exact sizing and formats bracket order instructions for IBKR.
func BuildBracketTicket(symbol string, estPrice, accountNAV, allocRatio float64) (*BracketOrderTicket, error) {
	if estPrice <= 0 {
		return nil, fmt.Errorf("invalid price for %s: $%.2f", symbol, estPrice)
	}
	if accountNAV <= 0 {
		return nil, fmt.Errorf("invalid account NAV: $%.2f", accountNAV)
	}
	if allocRatio <= 0 || allocRatio > 1.0 {
		allocRatio = 0.65 // Default 65% dynamic allocation
	}

	targetCapital := accountNAV * allocRatio
	shares := int(targetCapital / estPrice)
	if shares <= 0 {
		return nil, fmt.Errorf("insufficient capital ($%.2f) to purchase 1 share of %s at $%.2f", targetCapital, symbol, estPrice)
	}

	var targetPct, stopPct float64
	var maxHold int
	var direction string

	if symbol == "TECL" {
		direction = "LONG_TECL"
		targetPct = 0.05 // +5.0% Take Profit
		stopPct = 0.20   // -20.0% Disaster Guard Stop Loss
		maxHold = 8      // 8 Trading Days Time Barrier
	} else if symbol == "SPXU" {
		direction = "SHORT_SPXU"
		targetPct = 0.06 // +6.0% Take Profit
		stopPct = 0.05   // -5.0% Stop Loss
		maxHold = 2      // 2 Trading Days Time Barrier
	} else {
		// Default generic dip bracket
		direction = "LONG_" + symbol
		targetPct = 0.05
		stopPct = 0.15
		maxHold = 8
	}

	targetPrice := math.Round(estPrice*(1.0+targetPct)*100) / 100.0
	stopPrice := math.Round(estPrice*(1.0-stopPct)*100) / 100.0
	totalCost := float64(shares) * estPrice
	ocoGroup := fmt.Sprintf("BRACKET_%s_%d", symbol, shares)

	// Format TWS Manual Order Instructions
	twsGuide := fmt.Sprintf(`
📋 INTERACTIVE BROKERS ORDER TICKET (TWS / MOBILE):
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
1. PARENT ORDER:
   • Action: BUY %d shares of %s
   • Order Type: MARKET-ON-CLOSE (MOC) or LIMIT at $%.2f
   • Time-In-Force: DAY (Execute at 3:55 PM EST)
   • Total Value: $%.2f (%.1f%% of $%.2f NAV)

2. PROFIT-TAKER CHILD ORDER (Attach Bracket):
   • Action: SELL %d shares of %s
   • Order Type: LIMIT
   • Limit Price: $%.2f (+%.1f%% Target)
   • Time-In-Force: GTC (Good-Til-Cancelled)

3. STOP-LOSS CHILD ORDER (Attach Bracket):
   • Action: SELL %d shares of %s
   • Order Type: STOP
   • Stop Price: $%.2f (-%.1f%% Protection)
   • Time-In-Force: GTC (Good-Til-Cancelled)

4. TIME-BARRIER RULE:
   • If neither order fills within %d trading days, cancel bracket and exit at market close.
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━`,
		shares, symbol, estPrice, totalCost, allocRatio*100, accountNAV,
		shares, symbol, targetPrice, targetPct*100,
		shares, symbol, stopPrice, stopPct*100,
		maxHold,
	)

	// Format Client Portal Web API Order Payload
	type ChildOrder struct {
		AcctId   string  `json:"acctId"`
		Conid    int     `json:"conid"`
		SecType  string  `json:"secType"`
		OrderType string `json:"orderType"`
		Price    float64 `json:"price,omitempty"`
		Quantity float64 `json:"quantity"`
		Tif      string  `json:"tif"`
		Side     string  `json:"side"`
		ParentId string  `json:"parentId,omitempty"`
	}

	type BracketPayload struct {
		Symbol     string       `json:"symbol"`
		Parent     ChildOrder   `json:"parent_buy"`
		TakeProfit ChildOrder   `json:"take_profit_child"`
		StopLoss   ChildOrder   `json:"stop_loss_child"`
	}

	apiObj := BracketPayload{
		Symbol: symbol,
		Parent: ChildOrder{
			OrderType: "MKT",
			Quantity:  float64(shares),
			Side:      "BUY",
			Tif:       "DAY",
		},
		TakeProfit: ChildOrder{
			OrderType: "LMT",
			Price:     targetPrice,
			Quantity:  float64(shares),
			Side:      "SELL",
			Tif:       "GTC",
		},
		StopLoss: ChildOrder{
			OrderType: "STP",
			Price:     stopPrice,
			Quantity:  float64(shares),
			Side:      "SELL",
			Tif:       "GTC",
		},
	}

	apiJSON, _ := json.MarshalIndent(apiObj, "", "  ")

	return &BracketOrderTicket{
		Symbol:          symbol,
		Direction:       direction,
		Shares:          shares,
		EstEntryPrice:   estPrice,
		TotalCost:       totalCost,
		AccountNAV:      accountNAV,
		AllocationPct:   allocRatio,
		TargetPrice:     targetPrice,
		TargetPct:       targetPct,
		StopPrice:       stopPrice,
		StopPct:         stopPct,
		MaxHoldDays:     maxHold,
		OCOGroupName:    ocoGroup,
		TWSInstructions: twsGuide,
		ClientPortalAPI: string(apiJSON),
	}, nil
}
