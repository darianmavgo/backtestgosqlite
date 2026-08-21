#!/usr/bin/env bash
set -e

# 1. Compile binaries
echo "🔨 Building binaries..."
go build -o bin/download ./cmd/download
go build -o bin/backtest ./cmd/backtest

# 2. Ingest custom CSV into local SQLite DB
echo "📥 Ingesting sample CSV..."
./bin/download -csv examples/custom_csv_backtest/sample_stocks.csv -db data/sample_stocks.db

# 3. Run backtest on custom data
echo "🚀 Running Donchian Breakout backtest on imported CSV..."
./bin/backtest -db data/sample_stocks.db -strategy donchian-breakout -capital 50000

echo "✅ Example completed successfully!"
