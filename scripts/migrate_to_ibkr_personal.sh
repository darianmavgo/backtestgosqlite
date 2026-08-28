#!/bin/bash
set -e

# scripts/migrate_to_ibkr_personal.sh
# ------------------------------------
# Refactors and extracts the Interactive Brokers execution suite into an adjacent repository:
# /Users/darianhickman/Documents/ibkr_personal

TARGET_DIR="/Users/darianhickman/Documents/ibkr_personal"
SOURCE_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

echo "======================================================================================================================="
echo "📦 MIGRATING INTERACTIVE BROKERS EXECUTION SUITE TO: $TARGET_DIR"
echo "======================================================================================================================="

mkdir -p "$TARGET_DIR"/{cmd/{preflight,live,bot,harness,analytics},internal/{broker,live,models,analytics,storage},config,scripts,reports,data,bin}

# 1. Write Makefile for ibkr_personal
cat << 'EOF' > "$TARGET_DIR/Makefile"
# Makefile for ibkr_personal — Interactive Brokers Live Execution Suite

BIN_DIR := bin
GO := go

.PHONY: all build preflight live bot harness analytics gateway ibc status history clean

all: build

build:
	@echo "🔨 Building all ibkr_personal binaries..."
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/preflight ./cmd/preflight
	$(GO) build -o $(BIN_DIR)/live ./cmd/live
	$(GO) build -o $(BIN_DIR)/bot ./cmd/bot
	$(GO) build -o $(BIN_DIR)/harness ./cmd/harness
	$(GO) build -o $(BIN_DIR)/analytics ./cmd/analytics
	@echo "✅ All binaries built in $(BIN_DIR)/"

preflight: build
	@echo "🔍 Running Interactive Brokers Pre-Flight Diagnostics..."
	./$(BIN_DIR)/preflight

live: build
	@echo "⚡ Executing live broker order..."
	./$(BIN_DIR)/live

harness: build
	@echo "🧪 Running Complex Bracket Order Test Harness..."
	./$(BIN_DIR)/harness

status: build
	@echo "📋 Checking active live position status..."
	./$(BIN_DIR)/bot -status

history: build
	@echo "📜 Viewing live trade history & win rate..."
	./$(BIN_DIR)/bot -history

scan: build
	@echo "🤖 Running 3:50 PM EOD market scanner..."
	./$(BIN_DIR)/bot -scan

gateway:
	@echo "🌐 Starting Interactive Brokers Client Portal Gateway on port 5001..."
	./scripts/start_ibkr_gateway.sh

ibc:
	@echo "☕ Starting Trader Workstation with IBC Controller..."
	./scripts/start_ibc_tws.sh

submit:
	@echo "🚀 Submitting 3-leg live bracket order..."
	python3 scripts/submit_live_order.py --symbol SOFI --price 8.00

clean:
	@echo "🧹 Cleaning binaries..."
	rm -rf $(BIN_DIR)/*
EOF

# 2. Write .gitignore
cat << 'EOF' > "$TARGET_DIR/.gitignore"
# Binaries and builds
bin/
*.exe
*.dll
*.so
*.dylib

# Configuration & Secrets
.env
*.env
ibc/config.ini

# SQLite Databases & Logs
*.db
*.db-shm
*.db-wal
data/*.db
logs/
*.log

# Java & Gateway
clientportal/dist/
.DS_Store
.vscode/
.idea/
EOF

# 3. Write .env.example
cat << 'EOF' > "$TARGET_DIR/.env.example"
# Interactive Brokers Credentials (Never commit to Git)
IBKR_ACCOUNT_ID="U1234567"
IBKR_USERNAME="your_username"
IBKR_PASSWORD="your_password"
IBKR_GATEWAY_URL="https://localhost:5001"
EOF

# 4. Write README.md
cat << 'EOF' > "$TARGET_DIR/README.md"
# 🏛️ `ibkr_personal` — Interactive Brokers Live Execution Suite

A high-performance, standalone Go & Python live execution engine for Interactive Brokers with support for Client Portal REST Gateway, TWS Socket API, IBC headless automation, and 3-leg complex multi-leg bracket orders (+5% TP / 4% Trailing Stop).

---

## ⚡ Quick Start

```bash
# 1. Run 8-Point Pre-Flight Diagnostics
make preflight

# 2. Launch Client Portal Gateway on Port 5001
make gateway

# 3. Submit a 3-Leg Multi-Leg Bracket Order (SOFI 1 Share @ $8.00)
make submit

# 4. Check Active Open Positions
make status

# 5. Run 3:50 PM EOD Strategy Market Scan
make scan
```

---

## 📦 Binaries (`bin/`)

| Binary | Description |
| :--- | :--- |
| **`./bin/preflight`** | 8-point live broker diagnostic engine (`make preflight`). |
| **`./bin/live`** | Direct broker execution runner. |
| **`./bin/bot`** | Live position manager (`-status`), history (`-history`), and EOD scanner (`-scan`). |
| **`./bin/harness`** | Complex multi-leg bracket order ticket generator. |
| **`./bin/analytics`** | Account statement and portfolio performance analytics. |

---

## 🛡️ Architecture & Security

* **100% Mock-Free**: SQLite ledger (`data/ibkr_live_state.db`) only records confirmed executions with real IBKR Server Order IDs.
* **Dual Gateway Support**: Client Portal REST API (`:5001`) and Trader Workstation Socket (`:7497` / `:7496`).
* **IBC Automation**: Headless JVM controller in `ibc/` (`make ibc`).
EOF

echo "🔨 Building ibkr_personal binaries..."
cd "$TARGET_DIR"
go mod tidy
make build

echo "🔍 Running preflight diagnostics in ibkr_personal..."
make preflight

echo "======================================================================================================================="
echo "✅ IBKR_PERSONAL STANDALONE REPOSITORY SETUP COMPLETE!"
echo "📁 Location: $TARGET_DIR"
echo "======================================================================================================================="
