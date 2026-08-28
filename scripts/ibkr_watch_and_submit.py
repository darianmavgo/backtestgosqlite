#!/usr/bin/env python3
"""
scripts/ibkr_watch_and_submit.py
--------------------------------
Dual-channel live watcher:
1. Watches for Trader Workstation (TWS) socket on port 7497/7496.
2. Watches for Client Portal REST Gateway login on https://localhost:5001.

As soon as either channel authenticates, it immediately executes the $10 TECL bracket trade
and displays live confirmation tickets.
"""

import sys
import time
import json
import urllib.request
import ssl
from ib_insync import IB, Stock, Order, TagValue

# Ignore local self-signed SSL for Client Portal Gateway
ssl_ctx = ssl.create_default_context()
ssl_ctx.check_hostname = False
ssl_ctx.verify_mode = ssl.CERT_NONE

def find_active_port():
    candidate_ports = [7497, 7496, 4002, 4001]
    ib = IB()
    for port in candidate_ports:
        try:
            ib.connect('127.0.0.1', port, clientId=99, timeout=1.5)
            ib.disconnect()
            return port
        except Exception:
            continue
    return None

def check_gateway_auth():
    try:
        req = urllib.request.Request("https://localhost:5001/v1/api/iserver/auth/status")
        with urllib.request.urlopen(req, context=ssl_ctx, timeout=2) as resp:
            data = json.loads(resp.read().decode())
            if data.get("authenticated") and data.get("connected"):
                return True
    except Exception:
        pass
    return False

def submit_via_gateway():
    print("\n🌐 SUBMITTING ORDER VIA CLIENT PORTAL REST API GATEWAY (Port 5001)...")
    try:
        # Get Accounts
        req = urllib.request.Request("https://localhost:5001/v1/api/iserver/accounts")
        with urllib.request.urlopen(req, context=ssl_ctx, timeout=5) as resp:
            accts_data = json.loads(resp.read().decode())
            acct_id = accts_data.get("accounts", ["DU1234567"])[0]

        # Submit Bracket
        order_payload = {
            "orders": [
                {
                    "acctId": acct_id,
                    "secType": "STK",
                    "orderType": "LMT",
                    "side": "BUY",
                    "cashQty": 10.00,
                    "price": 210.66,
                    "tif": "DAY",
                    "outsideRTH": True,
                    "strategy": "Adaptive",
                    "strategyParameters": {"adaptivePriority": "Normal"}
                },
                {
                    "acctId": acct_id,
                    "secType": "STK",
                    "orderType": "LMT",
                    "side": "SELL",
                    "quantity": 0.0475,
                    "price": 221.19,
                    "tif": "GTC",
                    "outsideRTH": True,
                    "ocaGroupId": "OCA_TECL_1000",
                    "ocaType": 1
                },
                {
                    "acctId": acct_id,
                    "secType": "STK",
                    "orderType": "TRAIL",
                    "side": "SELL",
                    "quantity": 0.0475,
                    "auxPrice": 8.43,
                    "tif": "GTC",
                    "outsideRTH": True,
                    "ocaGroupId": "OCA_TECL_1000",
                    "ocaType": 1
                }
            ]
        }
        post_data = json.dumps(order_payload).encode()
        order_req = urllib.request.Request(
            f"https://localhost:5001/v1/api/iserver/account/{acct_id}/orders",
            data=post_data,
            headers={"Content-Type": "application/json"}
        )
        with urllib.request.urlopen(order_req, context=ssl_ctx, timeout=8) as resp:
            res_body = resp.read().decode()
            print("=======================================================================================================================")
            print("🟢 ORDER SUBMISSION SUCCESSFUL VIA CLIENT PORTAL GATEWAY!")
            print(f"📡 Gateway Response: {res_body}")
            print("=======================================================================================================================")
            return True
    except Exception as e:
        print(f"⚠️ Gateway submission failed: {e}")
        return False

def submit_via_tws(active_port):
    print(f"\n📡 Connecting to Trader Workstation on port {active_port} to submit $10 test trade...")
    ib = IB()
    try:
        ib.connect('127.0.0.1', active_port, clientId=22, timeout=8)
    except Exception as e:
        print(f"❌ Connection failed: {e}")
        return False

    try:
        accounts = ib.managedAccounts()
        acct_id = accounts[0] if accounts else "UNKNOWN"
        print(f"👤 Connected Account: {acct_id} ({'Paper Trading' if acct_id.startswith('DU') else 'Live Account'})")

        symbol = "TECL"
        contract = Stock(symbol, 'SMART', 'USD')
        ib.qualifyContracts(contract)

        ticker = ib.reqMktData(contract, '', False, False)
        ib.sleep(2.0)
        price = ticker.marketPrice()
        if not price or price != price or price <= 0:
            price = ticker.close or ticker.last or 210.66

        dollar_amount = 10.00
        qty = round(dollar_amount / price, 4)
        if qty <= 0:
            qty = 0.05

        tp_price = round(price * 1.05, 2)
        trail_amount = round(price * 0.04, 2)

        print(f"📊 Market Quote for {symbol}: ${price:.2f}")
        print(f"💰 Sizing: Exact $10.00 USD Cash Quantity ➔ {qty} Fractional Shares")
        print(f"🎯 Profit Target (+5%): ${tp_price:.2f} GTC")
        print(f"🛡️ Trailing Stop (4%): ${trail_amount:.2f} Trail GTC")

        # Parent Order
        parent = Order()
        parent.orderId = ib.client.getReqId()
        parent.action = 'BUY'
        parent.totalQuantity = qty
        parent.orderType = 'LMT'
        parent.lmtPrice = price
        parent.algoStrategy = 'Adaptive'
        parent.algoParams = [TagValue('adaptivePriority', 'Normal')]
        parent.outsideRth = True
        parent.account = acct_id
        parent.transmit = False

        # Profit Target Order
        take_profit = Order()
        take_profit.orderId = ib.client.getReqId()
        take_profit.action = 'SELL'
        take_profit.totalQuantity = qty
        take_profit.orderType = 'LMT'
        take_profit.lmtPrice = tp_price
        take_profit.tif = 'GTC'
        take_profit.outsideRth = True
        take_profit.parentId = parent.orderId
        take_profit.account = acct_id
        take_profit.transmit = False

        # Trailing Stop Order
        stop_order = Order()
        stop_order.orderId = ib.client.getReqId()
        stop_order.action = 'SELL'
        stop_order.totalQuantity = qty
        stop_order.orderType = 'TRAIL'
        stop_order.auxPrice = trail_amount
        stop_order.trailingPercent = 4.0
        stop_order.tif = 'GTC'
        stop_order.outsideRth = True
        stop_order.parentId = parent.orderId
        stop_order.account = acct_id
        stop_order.transmit = True

        print("\n🚀 Transmitting Bracket Order into Trader Workstation...")
        parent_trade = ib.placeOrder(contract, parent)
        tp_trade = ib.placeOrder(contract, take_profit)
        stop_trade = ib.placeOrder(contract, stop_order)

        ib.sleep(2.5)

        print("=======================================================================================================================")
        print("🟢 ORDER SUBMISSION SUCCESSFUL! LIVE IN TRADER WORKSTATION")
        print("=======================================================================================================================")
        print(f"   • Parent Buy Ticket #{parent.orderId}: BUY {qty} shares of {symbol} (Status: {parent_trade.orderStatus.status})")
        print(f"   • Profit Target Ticket #{take_profit.orderId}: SELL {qty} shares @ ${tp_price:.2f} GTC (Status: {tp_trade.orderStatus.status})")
        print(f"   • Trailing Stop Ticket #{stop_order.orderId}: SELL {qty} shares Trail ${trail_amount:.2f} GTC (Status: {stop_trade.orderStatus.status})")
        print("\n✨ Look at your Trader Workstation window — the 3 order tickets are now active in your Orders & Trades monitor!")
        print("=======================================================================================================================")
        return True
    finally:
        ib.disconnect()

def main():
    print("=======================================================================================================================")
    print("⏳ DUAL-CHANNEL IBKR WATCHER: MONITORING TWS SOCKET & BROWSER GATEWAY...")
    print("=======================================================================================================================")
    print("👉 CHANNEL 1: Trader Workstation App (Open on your desktop) ➔ Log in with your username/password.")
    print("👉 CHANNEL 2: Web Browser Gateway ➔ Open https://localhost:5001 in Safari/Chrome and complete 2FA login.")
    print("-----------------------------------------------------------------------------------------------------------------------")
    print("Actively polling every 2 seconds...")

    start_time = time.time()
    while time.time() - start_time < 600: # 10 minute window
        # Check Channel 1: TWS Socket
        active_port = find_active_port()
        if active_port:
            print(f"\n🎉 DETECTED ACTIVE TWS SOCKET ON PORT {active_port}!")
            if submit_via_tws(active_port):
                sys.exit(0)

        # Check Channel 2: Client Portal Gateway
        if check_gateway_auth():
            print("\n🎉 DETECTED AUTHENTICATED CLIENT PORTAL GATEWAY SESSION!")
            if submit_via_gateway():
                sys.exit(0)

        sys.stdout.write(".")
        sys.stdout.flush()
        time.sleep(2)

    print("\n❌ Watcher timed out after 10 minutes.")
    sys.exit(1)

if __name__ == "__main__":
    main()
