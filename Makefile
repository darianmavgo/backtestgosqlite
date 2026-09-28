.PHONY: all build clean test tidy backtest download ui server compare example-csv list

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
	@echo "Building cmd/scan_check..."
	$(GOBUILD) -o $(BIN_DIR)/scan_check ./cmd/scan_check
	@echo "Building cmd/stop_prob..."
	$(GOBUILD) -o $(BIN_DIR)/stop_prob ./cmd/stop_prob
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

clean:
	@echo "Cleaning binaries..."
	rm -rf $(BIN_DIR)/*
