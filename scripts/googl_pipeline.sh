#!/usr/bin/env bash
# Run every command except livescan for the strategies tied to GOOGL, in pipeline
# order, with -force wherever the command has it. See README "Create and polish a
# strategy: GOOGL walkthrough". Run from anywhere: it cd's to the repo root.
#
#   scripts/googl_pipeline.sh            # the GOOGL steps
#   SKIP_NETWORK=1 scripts/googl_pipeline.sh  # skip the market_history downloads
#
# A step that fails is logged and the script moves on. The exit code is 1 if any failed.
# Writes: data/market_history.db, data/*_models.db, data/reports/, and
# refdata/strategies.db (gridsearch promote adds streak rows).
set -uo pipefail
cd "$(dirname "$0")/.."

SYM=${SYM:-GOOGL}
PARK=${PARK:-SGOV}
BENCH=${BENCH:-VOO}
YEARS=${YEARS:-6}
sym_lc=$(echo "$SYM" | tr '[:upper:]' '[:lower:]')
park_lc=$(echo "$PARK" | tr '[:upper:]' '[:lower:]')

failed=()
step() { # step "<label>" cmd args...
  local label=$1; shift
  printf '\n\033[1m== %s\033[0m\n   $ %s\n' "$label" "$*"
  "$@" || { echo "   !! FAILED: $label"; failed+=("$label"); }
}

echo "Building..."
make build >/dev/null || { echo "build failed"; exit 1; }

# 1. data
if [ -z "${SKIP_NETWORK:-}" ]; then
  step "market_history $SYM $BENCH $PARK" ./bin/market_history -force -years "$YEARS" "$SYM" "$BENCH" "$PARK"
  if [ -n "${POLYGON_API_KEY:-}" ]; then
    step "market_history options $SYM" ./bin/market_history -force -source polygon-options -symbols "$SYM"
  else
    echo "-- skipping option bars: POLYGON_API_KEY is not set"
  fi
fi

# 2. which strategies are ours
step "strategy (counts)" ./bin/strategy
IDS=$(./bin/stratlist -comma sql/lists/googl.sql)
[ -n "$IDS" ] || { echo "no $SYM strategies found"; exit 1; }
echo "$SYM strategies: $IDS"
PRIMARY=${PRIMARY:-streak-${sym_lc}-down3-${sym_lc}}

# 3. explore and train
step "markov_test" ./bin/markov_test
step "study hmm_regime $SYM" ./bin/study -study hmm_regime -symbol "$SYM"
step "train markov $SYM" ./bin/train markov "$SYM"
step "train tree $SYM" ./bin/train tree "$SYM"
step "train streak (nothing to train)" ./bin/train streak
step "train hold (nothing to train)" ./bin/train hold

# 4. backtest each, then sweep and promote. The sweep stops at the holdout cutoff.
step "backtest $SYM strategies" ./bin/backtest -force -strategy "$IDS"
CUTOFF=$(sqlite3 -readonly data/market_history.db \
  "SELECT date(max(substr(Date,1,10)), '-12 months') FROM backtest_start WHERE symbol='$SYM' AND length(Date)=10")
for id in $(echo "$IDS" | tr ',' ' '); do
  step "gridsearch params $id" ./bin/gridsearch params "$id"
done
step "gridsearch $SYM strategies" ./bin/gridsearch -force -end "$CUTOFF" -strategy "$IDS"
step "gridsearch promote $PRIMARY" ./bin/gridsearch promote -strategy "$PRIMARY" -min-win-rate 0.6 -min-trades 30 -top 5
step "backtest optimized" ./bin/backtest optimized -strategy "$IDS"

# 5. validate
step "walk_forward" ./bin/walk_forward -keep-going -strategy "$IDS"
step "check_overfit" ./bin/check_overfit
step "strateval" ./bin/strateval -strategy "$IDS" -optimize -max-trials 50
step "strateval report" ./bin/strateval report
step "strateval status" ./bin/strateval status
step "strateval path" ./bin/strateval path

# 6. stack
SECONDARY=$(echo "$IDS" | tr ',' '\n' | grep -vx "$PRIMARY" | paste -sd, -)
step "backtest stack with park-$sym_lc" ./bin/backtest -force -alloc 0.1 -strategy "$PRIMARY+${sym_lc}_tree+park-$sym_lc"
step "backtest stack-eval" ./bin/backtest stack-eval -primary "$PRIMARY" -secondary "$SECONDARY,park-$park_lc" -stack-depth 3 -persist-best
if [ -z "${SKIP_NETWORK:-}" ] && [ -n "${POLYGON_API_KEY:-}" ]; then
  step "backtest covered-call $SYM" ./bin/backtest covered-call -symbol "$SYM" -otm 2 -commission 0.65 -opt-slip 0.05
fi

# 7. compare the same strategies: park_sweep (streak and markov rows only) and scoreboard
step "park_sweep seed" ./bin/park_sweep -strategy "$IDS" seed
step "park_sweep run" ./bin/park_sweep -strategy "$IDS" run
step "park_sweep rank" ./bin/park_sweep -strategy "$IDS" rank
step "park_sweep report" ./bin/park_sweep -strategy "$IDS" report
step "scoreboard" ./bin/scoreboard -force -strategy "$IDS"
step "scoreboard compile" ./bin/scoreboard compile -strategy "$IDS"
step "scoreboard status" ./bin/scoreboard status -strategy "$IDS"

# 8. staleness
step "backtest stale" ./bin/backtest stale
step "gridsearch stale" ./bin/gridsearch stale

echo
if [ ${#failed[@]} -gt 0 ]; then
  echo "FAILED steps (${#failed[@]}):"; printf '  - %s\n' "${failed[@]}"; exit 1
fi
echo "All steps passed."
