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

make build >/dev/null 2>&1 || { echo "build failed: run make build"; exit 1; }
IDS=$(./bin/stratlist -comma sql/lists/googl.sql)
[ -n "$IDS" ] || { echo "no $SYM strategies found"; exit 1; }
echo "$SYM strategies: $IDS"
PRIMARY=${PRIMARY:-streak-${sym_lc}-down3-${sym_lc}}

# 1. data
if [ -z "${SKIP_NETWORK:-}" ]; then
  step "market_history $SYM $BENCH $PARK" ./bin/market_history -force -years "$YEARS" "$SYM" "$BENCH" "$PARK"
  if [ -n "${POLYGON_API_KEY:-}" ]; then
    step "market_history options $SYM" ./bin/market_history -force -source polygon-options -symbols "$SYM"
  else
    echo "-- skipping option bars: POLYGON_API_KEY is not set"
  fi
fi

# 3. explore and train
step "markov_test" ./bin/markov_test
step "study hmm_regime $SYM" ./bin/study -study hmm_regime -symbol "$SYM"
step "train markov $SYM" ./bin/train markov "$SYM"
step "train tree $SYM" ./bin/train tree "$SYM"
step "train streak (nothing to train)" ./bin/train streak
step "train hold (nothing to train)" ./bin/train hold

# 4. everything below writes into one run folder, data/reports/<RUN>, so no command
# sees results from earlier runs or other strategies.
RUN=$(./bin/backtest newrun) || { echo "could not start a run folder"; exit 1; }
RUNDIR=data/reports/$RUN
echo "Run folder: $RUNDIR"

# 5. backtest each, then sweep and promote. The sweep stops at the holdout cutoff.
step "backtest $SYM strategies" ./bin/backtest -force -run-id "$RUN" -strategy "$IDS"
CUTOFF=$(sqlite3 data/market_history.db \
  "SELECT date(max(substr(Date,1,10)), '-12 months') FROM backtest_start WHERE symbol='$SYM' AND length(Date)=10")
[ -n "$CUTOFF" ] || { echo "no $SYM bars in data/market_history.db"; exit 1; }
for id in $(echo "$IDS" | tr ',' ' '); do
  step "gridsearch params $id" ./bin/gridsearch params "$id"
done
step "gridsearch $SYM strategies" ./bin/gridsearch -force -run-id "$RUN" -end "$CUTOFF" -strategy "$IDS"
step "gridsearch promote $PRIMARY" ./bin/gridsearch promote -run-id "$RUN" -strategy "$PRIMARY" -min-win-rate 0.6 -min-trades 30 -top 5
step "gridsearch apply" ./bin/gridsearch apply -run-id "$RUN" -strategy "$IDS"

# 6. validate
step "validate" ./bin/validate -keep-going -run-id "$RUN" -strategy "$IDS"
step "strateval" ./bin/strateval -run-id "$RUN" -strategy "$IDS" -optimize -max-trials 50
step "strateval report" ./bin/strateval report -run-id "$RUN" -strategy "$IDS"
step "strateval status" ./bin/strateval status -run-id "$RUN" -strategy "$IDS"
step "strateval path" ./bin/strateval path -run-id "$RUN"

# 7. stack
SECONDARY=$(echo "$IDS" | tr ',' '\n' | grep -vx "$PRIMARY" | paste -sd, -)
step "backtest stack with park-$sym_lc" ./bin/backtest -force -run-id "$RUN" -alloc 0.1 -strategy "$PRIMARY+${sym_lc}_tree+park-$sym_lc"
step "backtest stack" ./bin/backtest stack -run-id "$RUN" -primary "$PRIMARY" -secondary "$SECONDARY,park-$park_lc" -stack-depth 3 -persist-best
if [ -z "${SKIP_NETWORK:-}" ] && [ -n "${POLYGON_API_KEY:-}" ]; then
  step "backtest covered-call $SYM" ./bin/backtest covered-call -symbol "$SYM" -otm 2 -commission 0.65 -opt-slip 0.05
fi

# 8. compare the same strategies: park_sweep (streak and markov rows only) and scoreboard
PS="./bin/park_sweep -db $RUNDIR/park_googl.db -report-db $RUNDIR/park_googl_report.db -html $RUNDIR/park_googl.html -strategy $IDS"
step "park_sweep seed" $PS seed
step "park_sweep run" $PS run
step "park_sweep rank" $PS rank
step "park_sweep report" $PS report
step "scoreboard" ./bin/scoreboard -force -run-id "$RUN" -strategy "$IDS"
step "scoreboard compile" ./bin/scoreboard compile -run-id "$RUN" -strategy "$IDS"
step "scoreboard status" ./bin/scoreboard status -run-id "$RUN" -strategy "$IDS"

# 9. staleness of this run's sweep (backtest stale is skipped: it reports on every run)
step "gridsearch stale" ./bin/gridsearch stale -run-id "$RUN" -strategy "$IDS"

echo
if [ ${#failed[@]} -gt 0 ]; then
  echo "FAILED steps (${#failed[@]}):"; printf '  - %s\n' "${failed[@]}"; exit 1
fi
echo "All steps passed."
