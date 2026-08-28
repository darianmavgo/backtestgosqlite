// Package broker handles advanced Interactive Brokers order structures, multi-leg brackets, and fractional trading.
package broker

import (
	"encoding/json"
	"fmt"
	"math"
)

// ComplexOrderConfig specifies parameters for the most advanced IBKR order structure.
type ComplexOrderConfig struct {
	Symbol            string  // Target symbol (e.g. "SOXL", "TECL", "AAPL", "SPY")
	DollarAmount      float64 // Exact cash value to buy (e.g. $10.00 USD)
	EstPrice          float64 // Estimated market price per share
	TakeProfitPct     float64 // e.g. 0.05 (+5.0% limit target)
	StopLossPct       float64 // e.g. 0.05 (-5.0% stop loss)
	UseTrailingStop   bool    // Whether to use dynamic trailing stop instead of fixed stop
	TrailingStopPct   float64 // e.g. 0.04 (4% trailing stop)
	OutsideRTH        bool    // Pre-market and after-hours execution allowed
	UseAdaptiveAlgo   bool    // Use IBKR Smart Routing Adaptive Algo (Patient/Normal)
	AlgoPriority      string  // "Patient", "Normal", "Urgent"
	MaxHoldingDays    int     // Time-decay barrier
	AccountID         string  // IBKR Account ID (e.g. "U1234567" or "DU1234567" for paper)
}

// ComplexBracketOrder contains all legs and API payloads for Interactive Brokers.
type ComplexBracketOrder struct {
	Config           ComplexOrderConfig `json:"config"`
	FractionalShares float64            `json:"fractional_shares"`
	TotalInvestment  float64            `json:"total_investment"`
	TargetPrice      float64            `json:"target_price"`
	StopLossPrice    float64            `json:"stop_loss_price"`
	TrailingAmount   float64            `json:"trailing_amount,omitempty"`
	OCAGroupID       string             `json:"oca_group_id"`
	
	// API Payloads
	ClientPortalJSON string             `json:"client_portal_json"`
	TWSSocketSpec    string             `json:"tws_socket_spec"`
	TWSManualGuide   string             `json:"tws_manual_guide"`
}

// BuildComplexOrder constructs the most sophisticated multi-leg bracket order possible on IBKR.
func BuildComplexOrder(cfg ComplexOrderConfig) (*ComplexBracketOrder, error) {
	if cfg.DollarAmount <= 0 {
		cfg.DollarAmount = 10.00 // Default $10 test amount
	}
	if cfg.EstPrice <= 0 {
		return nil, fmt.Errorf("invalid estimated price for %s: $%.2f", cfg.Symbol, cfg.EstPrice)
	}
	if cfg.TakeProfitPct <= 0 {
		cfg.TakeProfitPct = 0.05 // +5% target
	}
	if cfg.StopLossPct <= 0 {
		cfg.StopLossPct = 0.05 // -5% stop loss
	}
	if cfg.AccountID == "" {
		cfg.AccountID = "YOUR_IBKR_ACCOUNT_ID"
	}

	// 1. Calculate Fractional Shares for exact $10 cash buy
	rawShares := cfg.DollarAmount / cfg.EstPrice
	// Round to 4 decimal places for IBKR fractional trading
	shares := math.Round(rawShares*10000.0) / 10000.0
	actualCost := shares * cfg.EstPrice

	// 2. Calculate Profit Target & Stop Prices
	targetPrice := math.Round(cfg.EstPrice*(1.0+cfg.TakeProfitPct)*100.0) / 100.0
	stopLossPrice := math.Round(cfg.EstPrice*(1.0-cfg.StopLossPct)*100.0) / 100.0
	trailingAmount := 0.0
	if cfg.UseTrailingStop && cfg.TrailingStopPct > 0 {
		trailingAmount = math.Round(cfg.EstPrice*cfg.TrailingStopPct*100.0) / 100.0
	}

	ocaGroup := fmt.Sprintf("OCA_%s_%d", cfg.Symbol, int(actualCost*100))

	// 3. Construct Client Portal REST API JSON Payload (Multi-Leg Bracket / OCA)
	type OrderLeg struct {
		AcctID       string                 `json:"acctId"`
		SecType      string                 `json:"secType"`
		OrderType    string                 `json:"orderType"`
		Side         string                 `json:"side"`
		Quantity     float64                `json:"quantity,omitempty"`
		CashQty      float64                `json:"cashQty,omitempty"`
		Price        float64                `json:"price,omitempty"`
		AuxPrice     float64                `json:"auxPrice,omitempty"` // For Stop / Trailing Stop
		Tif          string                 `json:"tif"`
		OutsideRTH   bool                   `json:"outsideRTH"`
		ParentID     string                 `json:"parentId,omitempty"`
		OcaGroupID   string                 `json:"ocaGroupId,omitempty"`
		OcaType      int                    `json:"ocaType,omitempty"` // 1 = Cancel with blocking
		Strategy     string                 `json:"strategy,omitempty"`
		StrategyParams map[string]string    `json:"strategyParameters,omitempty"`
	}

	type ClientPortalPayload struct {
		Orders []OrderLeg `json:"orders"`
	}

	// Leg 1: Parent Order (Adaptive Smart-Routed Entry)
	parentLeg := OrderLeg{
		AcctID:     cfg.AccountID,
		SecType:    "STK",
		OrderType:  "MKT",
		Side:       "BUY",
		CashQty:    cfg.DollarAmount, // Exact $10 buy via IBKR Cash Quantity feature!
		Tif:        "DAY",
		OutsideRTH: cfg.OutsideRTH,
	}
	if cfg.UseAdaptiveAlgo {
		parentLeg.OrderType = "LMT"
		parentLeg.Price = cfg.EstPrice
		parentLeg.Strategy = "Adaptive"
		parentLeg.StrategyParams = map[string]string{
			"adaptivePriority": cfg.AlgoPriority,
		}
	}

	// Leg 2: Child Profit-Taker (Limit GTC)
	profitLeg := OrderLeg{
		AcctID:     cfg.AccountID,
		SecType:    "STK",
		OrderType:  "LMT",
		Side:       "SELL",
		Quantity:   shares,
		Price:      targetPrice,
		Tif:        "GTC",
		OutsideRTH: cfg.OutsideRTH,
		OcaGroupID: ocaGroup,
		OcaType:    1,
	}

	// Leg 3: Child Stop Loss or Trailing Stop (GTC)
	stopLeg := OrderLeg{
		AcctID:     cfg.AccountID,
		SecType:    "STK",
		Side:       "SELL",
		Quantity:   shares,
		Tif:        "GTC",
		OutsideRTH: cfg.OutsideRTH,
		OcaGroupID: ocaGroup,
		OcaType:    1,
	}
	if cfg.UseTrailingStop && trailingAmount > 0 {
		stopLeg.OrderType = "TRAIL"
		stopLeg.AuxPrice = trailingAmount // Trail by $X.XX
	} else {
		stopLeg.OrderType = "STP"
		stopLeg.AuxPrice = stopLossPrice
	}

	portalPayload := ClientPortalPayload{
		Orders: []OrderLeg{parentLeg, profitLeg, stopLeg},
	}
	portalJSON, _ := json.MarshalIndent(portalPayload, "", "  ")

	// 4. Construct TWS Socket / Python IBAPI Equivalent Spec
	twsSocketSpec := fmt.Sprintf(`// TWS / IB Gateway Python/Go Socket Order Specification:
// ─────────────────────────────────────────────────────────────
// 1. Parent Contract: Stock("%s", "SMART", "USD")
//    Parent Order:   Order(action="BUY", totalQuantity=%.4f, orderType="MKT", cashQty=%.2f, outsideRth=%t, transmit=False)
// 2. Child Target:   Order(action="SELL", totalQuantity=%.4f, orderType="LMT", lmtPrice=%.2f, tif="GTC", ocaGroup="%s", ocaType=1, outsideRth=%t, parentId=parent.orderId, transmit=False)
// 3. Child Stop:     Order(action="SELL", totalQuantity=%.4f, orderType="%s", %s, tif="GTC", ocaGroup="%s", ocaType=1, outsideRth=%t, parentId=parent.orderId, transmit=True)`,
		cfg.Symbol, shares, cfg.DollarAmount, cfg.OutsideRTH,
		shares, targetPrice, ocaGroup, cfg.OutsideRTH,
		shares, stopLeg.OrderType, formatStopParams(stopLeg.OrderType, stopLossPrice, trailingAmount, cfg.TrailingStopPct), ocaGroup, cfg.OutsideRTH,
	)

	// 5. Construct Step-by-Step TWS & IBKR Mobile Order Guide
	twsGuide := fmt.Sprintf(`
══════════════════════════════════════════════════════════════════════════════════════
🤖 ULTIMATE IBKR ORDER TEST TICKET: $%.2f OF %s (COMPLEX MULTI-LEG BRACKET)
══════════════════════════════════════════════════════════════════════════════════════

1. 🟢 LEG 1: PARENT BUY ORDER (Entry)
   • Asset: %s (Stock / ETF)
   • Order Sizing: $%.2f USD Cash Quantity (Calculates to ~%.4f Fractional Shares @ $%.2f)
   • Order Type: %s
   • Route: SMART (IBKR Adaptive Algo: %s)
   • Outside Regular Trading Hours (Extended Hours): %t
   • Time-In-Force: DAY

2. 🎯 LEG 2: PROFIT-TAKER CHILD (One-Cancels-All Bracket)
   • Action: SELL %.4f shares of %s
   • Order Type: LIMIT (LMT)
   • Target Limit Price: $%.2f (+%.1f%% Target Profit)
   • Time-In-Force: GTC (Good-Til-Cancelled - Server-Side OCO)
   • Extended Hours (Outside RTH): %t (Fills on overnight gap-ups)

3. 🛡️ LEG 3: PROTECTIVE STOP / TRAIL CHILD (One-Cancels-All Bracket)
   • Action: SELL %.4f shares of %s
   • Order Type: %s
   • %s
   • Time-In-Force: GTC (Good-Til-Cancelled)
   • Extended Hours: %t
   • Server-Side OCO: Linked to Leg 2 (If Leg 2 fills, Leg 3 cancels instantly!)

4. ⏱️ LEG 4: CONDITIONAL TIME-BARRIER
   • Maximum Holding Window: %d Trading Days
   • If neither Leg 2 nor Leg 3 fills within %d days, cancel bracket and exit at market close.
══════════════════════════════════════════════════════════════════════════════════════`,
		cfg.DollarAmount, cfg.Symbol,
		cfg.Symbol, cfg.DollarAmount, shares, cfg.EstPrice,
		parentLeg.OrderType, cfg.AlgoPriority, cfg.OutsideRTH,
		shares, cfg.Symbol, targetPrice, cfg.TakeProfitPct*100.0, cfg.OutsideRTH,
		shares, cfg.Symbol, stopLeg.OrderType, formatStopDescription(cfg.UseTrailingStop, stopLossPrice, trailingAmount, cfg.TrailingStopPct), cfg.OutsideRTH,
		cfg.MaxHoldingDays, cfg.MaxHoldingDays,
	)

	return &ComplexBracketOrder{
		Config:           cfg,
		FractionalShares: shares,
		TotalInvestment:  actualCost,
		TargetPrice:      targetPrice,
		StopLossPrice:    stopLossPrice,
		TrailingAmount:   trailingAmount,
		OCAGroupID:       ocaGroup,
		ClientPortalJSON: string(portalJSON),
		TWSSocketSpec:    twsSocketSpec,
		TWSManualGuide:   twsGuide,
	}, nil
}

func formatStopParams(orderType string, stopPrice, trailAmt, trailPct float64) string {
	if orderType == "TRAIL" {
		return fmt.Sprintf("trailingPercent=%.1f, auxPrice=%.2f", trailPct*100.0, trailAmt)
	}
	return fmt.Sprintf("auxPrice=%.2f", stopPrice)
}

func formatStopDescription(isTrail bool, stopPrice, trailAmt, trailPct float64) string {
	if isTrail {
		return fmt.Sprintf("Trailing Stop Amount: $%.2f (%.1f%% Dynamic Trail from High)", trailAmt, trailPct*100)
	}
	return fmt.Sprintf("Stop Price: $%.2f (Protective Stop)", stopPrice)
}
