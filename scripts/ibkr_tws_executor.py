#!/usr/bin/env python3
"""
scripts/ibkr_tws_executor.py
----------------------------
Direct Interactive Brokers TWS / IB Gateway API Socket Executor.
Connects via localhost:7497 (Paper) or localhost:7496 (Live) / 4002 / 4001.
Places complex multi-leg bracket orders (Parent Buy + Child TP + Child SL / Trail)
and returns execution status to Go.
"""

import sys
import json
import argparse
import time
from ib_insync import IB, Stock, MarketOrder, LimitOrder, StopOrder, Order, TagValue, util

def find_active_port(host='127.0.0.1', preferred_port=None):
    """Scan standard IBKR TWS and Gateway ports to find an active listener."""
    candidate_ports = [7497, 7496, 4002, 4001]
    if preferred_port and preferred_port in candidate_ports:
        candidate_ports.remove(preferred_port)
        candidate_ports.insert(0, preferred_port)
    elif preferred_port:
        candidate_ports.insert(0, preferred_port)

    ib = IB()
    for port in candidate_ports:
        try:
            ib.connect(host, port, clientId=99, timeout=2)
            ib.disconnect()
            return port
        except Exception:
            continue
    return None

def get_account_summary(host='127.0.0.1', port=7497, client_id=10):
    ib = IB()
    try:
        ib.connect(host, port, clientId=client_id, timeout=5)
    except Exception as e:
        return {"success": False, "error": f"Failed to connect to TWS on {host}:{port}: {str(e)}"}

    try:
        accounts = ib.managedAccounts()
        acct = accounts[0] if accounts else "UNKNOWN"
        summary_tags = ib.accountSummary(acct)
        
        nav = 0.0
        buying_power = 0.0
        cash = 0.0
        
        for item in summary_tags:
            if item.tag == 'NetLiquidation':
                try: nav = float(item.value)
                except: pass
            elif item.tag == 'BuyingPower':
                try: buying_power = float(item.value)
                except: pass
            elif item.tag == 'TotalCashValue':
                try: cash = float(item.value)
                except: pass

        positions = []
        for pos in ib.positions(acct):
            positions.append({
                "symbol": pos.contract.symbol,
                "secType": pos.contract.secType,
                "shares": float(pos.position),
                "avgCost": float(pos.avgCost)
            })

        return {
            "success": True,
            "connected": True,
            "port": port,
            "account_id": acct,
            "is_paper": acct.startswith("DU"),
            "net_liquidation": nav,
            "buying_power": buying_power,
            "cash_balance": cash,
            "positions": positions
        }
    finally:
        ib.disconnect()

def execute_complex_order(args):
    ib = IB()
    active_port = args.port
    if not active_port:
        active_port = find_active_port(args.host)
        if not active_port:
            return {
                "success": False,
                "error": "No active IBKR TWS / Gateway socket found. Please ensure Trader Workstation is running and API is enabled (Port 7497 for Paper, 7496 for Live)."
            }

    try:
        ib.connect(args.host, active_port, clientId=args.client_id, timeout=8)
    except Exception as e:
        return {"success": False, "error": f"Connection failed to TWS on {args.host}:{active_port}: {str(e)}"}

    try:
        accounts = ib.managedAccounts()
        acct_id = args.account if args.account and args.account != "YOUR_IBKR_ACCOUNT_ID" else (accounts[0] if accounts else "UNKNOWN")

        # 1. Qualify Contract
        contract = Stock(args.symbol.upper(), 'SMART', 'USD')
        ib.qualifyContracts(contract)

        # 2. Get Real-Time / Last Market Price
        ticker = ib.reqMktData(contract, '', False, False)
        ib.sleep(1.5)
        market_price = ticker.marketPrice()
        if not market_price or market_price != market_price or market_price <= 0: # Check NaN/zero
            market_price = ticker.close or ticker.last or args.fallback_price
        
        if not market_price or market_price <= 0:
            return {"success": False, "error": f"Could not obtain valid market price for {args.symbol}"}

        # 3. Calculate Quantity (Fractional or Cash Sizing)
        if args.shares > 0:
            quantity = args.shares
        else:
            quantity = round(args.amount / market_price, 4)
            if quantity <= 0:
                quantity = 1.0

        target_price = round(market_price * (1.0 + args.tp), 2)
        stop_price = round(market_price * (1.0 - args.sl), 2)
        trail_amount = round(market_price * args.trail_pct, 2) if args.trail else 0.0

        # 4. Create Parent & Child Bracket Orders
        parent = Order()
        parent.orderId = ib.client.getReqId()
        parent.action = 'BUY'
        parent.totalQuantity = quantity
        parent.account = acct_id
        parent.outsideRth = args.extended
        parent.transmit = False

        if args.adaptive:
            parent.orderType = 'LMT'
            parent.lmtPrice = market_price
            parent.algoStrategy = 'Adaptive'
            parent.algoParams = [TagValue('adaptivePriority', args.algo_priority)]
        else:
            parent.orderType = 'MKT'

        # Profit Taker (Leg 2)
        take_profit = Order()
        take_profit.orderId = ib.client.getReqId()
        take_profit.action = 'SELL'
        take_profit.totalQuantity = quantity
        take_profit.orderType = 'LMT'
        take_profit.lmtPrice = target_price
        take_profit.tif = 'GTC'
        take_profit.outsideRth = args.extended
        take_profit.parentId = parent.orderId
        take_profit.account = acct_id
        take_profit.transmit = False

        # Stop Loss / Trailing Stop (Leg 3)
        stop_order = Order()
        stop_order.orderId = ib.client.getReqId()
        stop_order.action = 'SELL'
        stop_order.totalQuantity = quantity
        stop_order.tif = 'GTC'
        stop_order.outsideRth = args.extended
        stop_order.parentId = parent.orderId
        stop_order.account = acct_id
        stop_order.transmit = True # Transmit the whole bracket on final leg

        if args.trail and trail_amount > 0:
            stop_order.orderType = 'TRAIL'
            stop_order.auxPrice = trail_amount
            stop_order.trailingPercent = args.trail_pct * 100.0
        else:
            stop_order.orderType = 'STP'
            stop_order.auxPrice = stop_price

        # Place Orders into TWS
        parent_trade = ib.placeOrder(contract, parent)
        tp_trade = ib.placeOrder(contract, take_profit)
        stop_trade = ib.placeOrder(contract, stop_order)

        ib.sleep(2.0)

        return {
            "success": True,
            "connected_port": active_port,
            "account_id": acct_id,
            "symbol": args.symbol.upper(),
            "quantity": quantity,
            "est_price": market_price,
            "total_cost": round(quantity * market_price, 2),
            "target_price": target_price,
            "stop_price": stop_price if not args.trail else f"Trailing by ${trail_amount:.2f} ({args.trail_pct*100:.1f}%)",
            "parent_order_id": parent.orderId,
            "take_profit_order_id": take_profit.orderId,
            "stop_order_id": stop_order.orderId,
            "parent_status": parent_trade.orderStatus.status,
            "tp_status": tp_trade.orderStatus.status,
            "stop_status": stop_trade.orderStatus.status,
            "message": f"Successfully placed {args.symbol} bracket order into Interactive Brokers TWS!"
        }
    except Exception as e:
        return {"success": False, "error": f"Order placement failed: {str(e)}"}
    finally:
        ib.disconnect()

def main():
    parser = argparse.ArgumentParser(description="IBKR TWS Order Executor & Diagnostics")
    parser.add_argument("--host", default="127.0.0.1", help="TWS / Gateway Host")
    parser.add_argument("--port", type=int, default=0, help="TWS / Gateway Port (0 = auto-detect 7497/7496)")
    parser.add_argument("--client-id", type=int, default=25, help="API Client ID")
    parser.add_argument("--account", default="", help="IBKR Account ID")
    parser.add_argument("--status", action="store_true", help="Query account summary and positions")
    parser.add_argument("--symbol", default="TECL", help="Symbol to buy")
    parser.add_argument("--amount", type=float, default=10.00, help="Dollar cash amount to buy")
    parser.add_argument("--shares", type=float, default=0.0, help="Explicit share quantity")
    parser.add_argument("--tp", type=float, default=0.05, help="Take profit % (0.05 = +5%)")
    parser.add_argument("--sl", type=float, default=0.05, help="Stop loss % (0.05 = -5%)")
    parser.add_argument("--trail", action="store_true", default=True, help="Use trailing stop")
    parser.add_argument("--trail-pct", type=float, default=0.04, help="Trailing stop % (0.04 = 4%)")
    parser.add_argument("--extended", action="store_true", default=True, help="Allow extended hours (pre/post market)")
    parser.add_argument("--adaptive", action="store_true", default=True, help="Use IBKR Adaptive Smart Algo")
    parser.add_argument("--algo-priority", default="Normal", help="Adaptive priority (Patient/Normal/Urgent)")
    parser.add_argument("--fallback-price", type=float, default=210.00, help="Fallback price if market data snapshot fails")

    args = parser.parse_args()

    if args.status:
        active_port = args.port or find_active_port(args.host)
        if not active_port:
            print(json.dumps({
                "success": False,
                "connected": False,
                "error": "TWS is not yet listening on API ports (7497/7496). Please log into TWS and enable API settings."
            }))
            sys.exit(0)
        res = get_account_summary(args.host, active_port, args.client_id)
        print(json.dumps(res, indent=2))
        sys.exit(0)

    result = execute_complex_order(args)
    print(json.dumps(result, indent=2))

if __name__ == "__main__":
    main()
