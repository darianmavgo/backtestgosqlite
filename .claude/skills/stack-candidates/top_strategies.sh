#!/bin/zsh
# Rank the strategies of one backtest run for a stack, across every family.
#
#   top_strategies.sh <run_dir> [n]
#
# <run_dir> is data/reports/<run_id>, which holds one database per family
# (markov.db, tree.db, ...) and the held-out pass in oos/. A strategy is a
# candidate only if it was backtested in both windows and passes the gates below.
# Score = the lower of its in-sample and out-of-sample Calmar ratio, so a strategy
# must hold up in both. One strategy per trade symbol and at most PER_FAMILY per
# family are kept, so the list is not twenty copies of one idea.
#
# Environment (defaults): MIN_IS_TRADES=30 MIN_OOS_TRADES=8 MAX_OOS_DD=0.30 PER_FAMILY=8
# IDS_ONLY=1 prints just the ranked strategy ids, comma separated, for -strategy.
# When data/reports/gridsearch.db has a sweep for a strategy, its best config
# (by resilience score) is shown as gridsearch_best.
set -euo pipefail

RUN=${1:?usage: top_strategies.sh <run_dir> [n]}
N=${2:-20}
MIN_IS=${MIN_IS_TRADES:-30}
MIN_OOS=${MIN_OOS_TRADES:-8}
MAX_DD=${MAX_OOS_DD:-0.30}
PER_FAMILY=${PER_FAMILY:-8}
ROOT=${APP_FOLDER:-.}
STRATS=$ROOT/refdata/strategies.db
LEDGER=$ROOT/data/reports/strategies.db

[[ -d $RUN ]] || { echo "no such run folder: $RUN" >&2; exit 1 }
[[ -d $RUN/oos ]] || { echo "$RUN has no oos/ folder: rerun backtest without -holdout-months 0" >&2; exit 1 }

WORK=$(mktemp -d)
trap 'rm -rf $WORK' EXIT
DB=$WORK/cand.db
sqlite3 $DB "CREATE TABLE cand (strategy_id TEXT, family TEXT, symbol TEXT,
  is_cagr REAL, is_dd REAL, is_calmar REAL, is_sharpe REAL, is_trades INTEGER,
  oos_cagr REAL, oos_dd REAL, oos_calmar REAL, oos_sharpe REAL, oos_trades INTEGER);"

# family -> (table, symbol column) in refdata/strategies.db; other families use the id.
symbol_expr() {
  case $1 in
    streak|tree|markov) echo "(SELECT trade_symbol FROM r.${1}_strategy WHERE id = p.strategy_id)" ;;
    hold|hold_bail)     echo "(SELECT symbol FROM r.${1}_strategy WHERE id = p.strategy_id)" ;;
    *)                  echo "p.strategy_id" ;;
  esac
}

for f in $RUN/*.db; do
  fam=${f:t:r}
  [[ $fam == scoreboard ]] && continue
  [[ -f $RUN/oos/$fam.db ]] || { echo "skipping $fam: no held-out results in $RUN/oos/$fam.db" >&2; continue }
  sqlite3 $DB <<SQL
ATTACH '$f' AS i;
ATTACH '$RUN/oos/$fam.db' AS o;
ATTACH '$STRATS' AS r;
INSERT INTO cand
SELECT p.strategy_id, '$fam', COALESCE($(symbol_expr $fam), p.strategy_id),
       p.cagr, p.max_drawdown_pct, p.calmar_ratio, p.sharpe_ratio, p.total_trades,
       q.cagr, q.max_drawdown_pct, q.calmar_ratio, q.sharpe_ratio, q.total_trades
FROM i.performance_summary p
JOIN (SELECT strategy_id, MAX(run_id) AS run_id FROM i.performance_summary GROUP BY strategy_id) pm
  ON pm.strategy_id = p.strategy_id AND pm.run_id = p.run_id
JOIN i.runs ir ON ir.run_id = p.run_id AND ir.strategy_id = p.strategy_id
JOIN o.performance_summary q ON q.strategy_id = p.strategy_id
JOIN (SELECT strategy_id, MAX(run_id) AS run_id FROM o.performance_summary GROUP BY strategy_id) qm
  ON qm.strategy_id = q.strategy_id AND qm.run_id = q.run_id;
SQL
done

TIER_JOIN=""
TIER_COL="'-'"
if [[ -f $LEDGER ]]; then
  sqlite3 $DB "ATTACH '$LEDGER' AS l; SELECT 1 FROM l.sqlite_master WHERE name = 'v_latest_scores';" | grep -q 1 && { TIER_JOIN="LEFT JOIN l.v_latest_scores t ON t.strategy_id = s.strategy_id"; TIER_COL="COALESCE(t.tier, '-')" }
fi
ATTACH_LEDGER=""
[[ -n $TIER_JOIN ]] && ATTACH_LEDGER="ATTACH '$LEDGER' AS l;"
GRID=$ROOT/data/reports/gridsearch.db
GRID_COL="'-'"
if [[ -f $GRID ]]; then
  ATTACH_LEDGER="$ATTACH_LEDGER ATTACH '$GRID' AS g;"
  GRID_COL="COALESCE((SELECT label FROM g.gridsearch_results WHERE strategy_id = s.strategy_id ORDER BY resilience_score DESC LIMIT 1), '-')"
fi

if [[ -z ${IDS_ONLY:-} ]]; then
sqlite3 $DB <<SQL
.mode column
.headers on
SELECT 'candidates' AS step, COUNT(*) AS strategies FROM cand
UNION ALL SELECT 'in-sample trades >= $MIN_IS', COUNT(*) FROM cand WHERE is_trades >= $MIN_IS
UNION ALL SELECT 'and out-of-sample trades >= $MIN_OOS', COUNT(*) FROM cand WHERE is_trades >= $MIN_IS AND oos_trades >= $MIN_OOS
UNION ALL SELECT 'and profitable in both windows', COUNT(*) FROM cand WHERE is_trades >= $MIN_IS AND oos_trades >= $MIN_OOS AND is_cagr > 0 AND oos_cagr > 0
UNION ALL SELECT 'and out-of-sample drawdown <= $MAX_DD', COUNT(*) FROM cand WHERE is_trades >= $MIN_IS AND oos_trades >= $MIN_OOS AND is_cagr > 0 AND oos_cagr > 0 AND oos_dd <= $MAX_DD;
SQL
echo
fi

if [[ -n ${IDS_ONLY:-} ]]; then
  export OUT_MODE=list OUT_HEADERS=off
fi
OUT=$(
sqlite3 $DB <<SQL
$ATTACH_LEDGER
CREATE TABLE gated AS
SELECT *, MIN(is_calmar, oos_calmar) AS score FROM cand
WHERE is_trades >= $MIN_IS AND oos_trades >= $MIN_OOS AND is_cagr > 0 AND oos_cagr > 0 AND oos_dd <= $MAX_DD;
CREATE TABLE ranked AS
SELECT *, ROW_NUMBER() OVER (PARTITION BY symbol ORDER BY score DESC) AS sym_rank,
          ROW_NUMBER() OVER (PARTITION BY family ORDER BY score DESC) AS fam_rank
FROM gated;
.mode ${OUT_MODE:-markdown}
.headers ${OUT_HEADERS:-on}
SELECT ROW_NUMBER() OVER (ORDER BY s.score DESC) AS rank, s.strategy_id AS strategy, s.family, s.symbol,
       printf('%.2f', s.score) AS score,
       printf('%.1f%%', s.is_cagr*100) AS is_cagr, printf('%.1f%%', s.is_dd*100) AS is_dd, s.is_trades,
       printf('%.1f%%', s.oos_cagr*100) AS oos_cagr, printf('%.1f%%', s.oos_dd*100) AS oos_dd, s.oos_trades,
       $TIER_COL AS strateval_tier,
       $GRID_COL AS gridsearch_best
FROM ranked s $TIER_JOIN
WHERE s.sym_rank = 1 AND s.fam_rank <= $PER_FAMILY
ORDER BY s.score DESC
LIMIT $N;
SQL
)
if [[ -n ${IDS_ONLY:-} ]]; then
  echo "$OUT" | awk -F'|' '{print $2}' | paste -sd, -
else
  echo "$OUT"
fi
