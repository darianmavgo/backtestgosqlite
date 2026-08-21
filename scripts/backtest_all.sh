#!/bin/zsh
set -e

# ==============================================================================
# Whitings Creek (WC) 20% in 10 Days Strategy - Full Automated Pipeline
# Builds binaries, downloads universe history, runs 2-tier backtest, and updates settings.
# ==============================================================================

WC_DIR="/Users/darianhickman/Documents/wc_2022"
SETTINGS_DB="$WC_DIR/data/settings.db"
SQL_DIR="$WC_DIR/sql"
DATA_DB="$WC_DIR/data/wc_master_backtest.db"

echo "=================================================================="
echo "🚀 1. Building Go Binaries via Makefile..."
echo "=================================================================="
cd "$WC_DIR"
make build
echo "✅ Build completed successfully."

echo "\n=================================================================="
echo "📥 2. Downloading Historical Data (4 Years) for Target Universes..."
echo "=================================================================="
rm -f "$DATA_DB"

echo "\n---> Downloading Leveraged ETFs (50 symbols)..."
"$WC_DIR/bin/download" \
  -settings "$SETTINGS_DB" \
  -table "leveraged_etf" \
  -limit 50 \
  -years 4 \
  -db "$DATA_DB"

echo "\n---> Appending High-Beta Momentum Equities (50 symbols)..."
"$WC_DIR/bin/download" \
  -settings "$SETTINGS_DB" \
  -table "momentum_candidates" \
  -limit 50 \
  -years 4 \
  -db "$DATA_DB"

echo "\n=================================================================="
echo "⚙️ 3. Executing 2-Tier Backtest Engine against $DATA_DB..."
echo "=================================================================="
"$WC_DIR/bin/backtest" \
  -db "$DATA_DB" \
  -settings "$SETTINGS_DB" \
  -sqldir "$SQL_DIR" \
  -capital 100000 \
  -max-positions 5 \
  -stoploss 0.93 \
  -target 1.20

echo "\n=================================================================="
echo "💾 4. Updating settings.db with Verified 90% Win-Rate Symbols..."
echo "=================================================================="
sqlite3 "$SETTINGS_DB" "
DROP TABLE IF EXISTS backtested_win_20_10d;
ATTACH DATABASE '$DATA_DB' AS backtest_run;
CREATE TABLE backtested_win_20_10d AS SELECT * FROM backtest_run.backtested_win_20_10d;
DETACH DATABASE backtest_run;
SELECT 'Updated settings.db backtested_win_20_10d count:', COUNT(*) FROM backtested_win_20_10d;
"

echo "\n=================================================================="
echo "🎯 5. Top 15 Best-Performing Symbols Overall (20% Gain Target in 10 Days):"
echo "=================================================================="
sqlite3 -header -column "$DATA_DB" "
SELECT symbol, entries, sum_win3, sum_win5, wins_20_10d, win20_10d_rate, round(avg_max_gain_10d, 4) as avg_gain
FROM wc_summary
WHERE entries >= 4
ORDER BY win20_10d_rate DESC, entries DESC
LIMIT 15;
"

echo "\n🎉 Full pipeline completed successfully!"
