.PHONY: all build clean test tidy backtest download ui server compare example-csv list millwharf ibkr

# Go Parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Output directory
BIN_DIR=bin

all: build

build: tidy
	@mkdir -p $(BIN_DIR)
	@echo "Building cmd/download..."
	$(GOBUILD) -o $(BIN_DIR)/download ./cmd/download
	@echo "Building cmd/backtest..."
	$(GOBUILD) -o $(BIN_DIR)/backtest ./cmd/backtest
	@echo "Building cmd/ui..."
	$(GOBUILD) -o $(BIN_DIR)/ui ./cmd/ui
	@echo "Building cmd/server..."
	$(GOBUILD) -o $(BIN_DIR)/server ./cmd/server
	@echo "Building cmd/compare..."
	$(GOBUILD) -o $(BIN_DIR)/compare ./cmd/compare
	@echo "Building cmd/millwharf..."
	$(GOBUILD) -o $(BIN_DIR)/millwharf ./cmd/millwharf
	@echo "Building cmd/ibkr_analytics..."
	$(GOBUILD) -o $(BIN_DIR)/ibkr_analytics ./cmd/ibkr_analytics
	@echo "Building cmd/ibkr_preflight..."
	$(GOBUILD) -o $(BIN_DIR)/ibkr_preflight ./cmd/ibkr_preflight
	@echo "Building cmd/ibkr_test_harness..."
	$(GOBUILD) -o $(BIN_DIR)/ibkr_test_harness ./cmd/ibkr_test_harness
	@echo "Building cmd/ibkr_live..."
	$(GOBUILD) -o $(BIN_DIR)/ibkr_live ./cmd/ibkr_live
	@echo "Building cmd/ibkr_bot..."
	$(GOBUILD) -o $(BIN_DIR)/ibkr_bot ./cmd/ibkr_bot
	@echo "✅ All binaries built successfully in $(BIN_DIR)/"

tidy:
	@echo "Tidying go.mod..."
	$(GOMOD) tidy

test:
	@echo "Running tests..."
	$(GOTEST) -v ./internal/... ./cmd/...

list: build
	./$(BIN_DIR)/backtest -list

backtest: build
	@echo "Running default backtest (BB-Capitulation)..."
	./$(BIN_DIR)/backtest -strategy bb-capitulation -capital 100000

example-csv: build
	@echo "Running custom CSV ingestion and backtest example..."
	./examples/custom_csv_backtest/run_example.sh

download: build
	@echo "Downloading 4 years of history for top 50 symbols..."
	./$(BIN_DIR)/download -db data/leveraged_backtest.db -settings data/settings.db -table leveraged_etf -limit 50 -years 4

ui: build
	@echo "Launching UI server on http://localhost:8080..."
	./$(BIN_DIR)/ui -port 8080 -master data/wc_master_backtest.db -settings data/settings.db

server: build
	@echo "Launching live trading GAE server on http://localhost:8080..."
	./$(BIN_DIR)/server

compare: build
	@echo "Running side-by-side strategy benchmark comparison..."
	./$(BIN_DIR)/compare -db data/wc_master_backtest.db -capital 100000

millwharf: build
	@echo "Scanning stock universe for longest consistent declines since Jan 1 2026..."
	./$(BIN_DIR)/millwharf -db data/live_scan.db -start 2026-01-01 -top 25

ibkr: build
	@echo "Analyzing Interactive Brokers live account portfolio performance..."
	./$(BIN_DIR)/ibkr_analytics -db /Users/darianhickman/Documents/Income/transactions.db -prices data/ibkr_history.db

ibkr-preflight: build
	@echo "Running Interactive Brokers pre-flight diagnostics..."
	./$(BIN_DIR)/ibkr_preflight

ibkr-test: build
	@echo "Executing $10 complex bracket test order on TECL..."
	./$(BIN_DIR)/ibkr_test_harness -symbol TECL -amount 10

ibkr-scan: build
	@echo "Running 3:50 PM EOD market scan..."
	./$(BIN_DIR)/ibkr_bot -scan -capital 180000

ibkr-status: build
	@echo "Checking active live position status..."
	./$(BIN_DIR)/ibkr_bot -status

ibkr-history: build
	@echo "Viewing live trade performance history..."
	./$(BIN_DIR)/ibkr_bot -history

ibkr-gateway:
	@echo "Launching Interactive Brokers Client Portal Web API Gateway on port 5001..."
	./scripts/start_ibkr_gateway.sh

clean:
	@echo "Cleaning binaries..."
	rm -rf $(BIN_DIR)/*
