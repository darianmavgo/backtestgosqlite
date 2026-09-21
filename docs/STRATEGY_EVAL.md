# Strategy ledger (tested → scored → deployed)

One SQLite file tracks the full lifecycle:

```text
reports/strategies.db
```

**Additive only** — does not change evening-scan, cron, or auto-edit `STRATEGY_ALLOWLIST`.

## Browse in any SQLite UI

Open `reports/strategies.db` in **DB Browser for SQLite**, Datasette, VS Code SQLite, TablePlus, etc.

Useful views:

| View | What it shows |
|------|----------------|
| `v_strategy_status` | All strategies: lifecycle, tier, deployed?, OOS stats |
| `v_latest_scores` | Newest eval row per strategy |
| `v_deployed` | Currently live allowlist entries |
| `v_promote_candidates` | Tier A, not deployed |
| `v_demote_candidates` | Deployed but tier C/D |

```sql
SELECT strategy_id, lifecycle, tier, deployed, oos_sharpe, oos_trades, last_evaluated
FROM v_strategy_status
ORDER BY lifecycle, oos_sharpe DESC;
```

## CLI

```bash
# Print ledger path + view cheat-sheet
go run ./cmd/strateval path

# Terminal table of lifecycle
go run ./cmd/strateval status

# Snapshot what's live (from STRATEGY_ALLOWLIST) into deployments table
go run ./cmd/strateval sync-deployed -allowlist "$STRATEGY_ALLOWLIST"

# Run evals (writes strategy_evals + updates catalog)
go run ./cmd/strateval -strategy all -optimize -allowlist "$STRATEGY_ALLOWLIST" -sync-deployed

# Markdown scorecard
go run ./cmd/strateval report -allowlist "$STRATEGY_ALLOWLIST"
```

## Tables

- `strategy_evals` — every IS/OOS score run (history)
- `strategies` — catalog: first/last seen, eval_count, last_tier
- `deployments` — allowlist sync history (`live` / `retired`)

## Lifecycle labels

| Label | Meaning |
|-------|---------|
| `scored_A`…`D` | Latest eval tier; not on allowlist |
| `deployed` | On allowlist and looking fine |
| `deployed_review` | On allowlist but latest tier is C/D |
| `tested` / `registered` | Seen but no (or incomplete) score |

## Safety

- `sync-deployed` only writes the **ledger** — it never edits `.env`
- Promote/demote still requires you to change `STRATEGY_ALLOWLIST` by hand

## Allowlist sync tip

Do **not** `source` trade_orchestrator’s full `.env` into this repo — it sets
`APP_FOLDER=/mnt/data` for Linux deploys. Export only the allowlist:

```bash
export STRATEGY_ALLOWLIST="$(grep -E '^STRATEGY_ALLOWLIST=' ~/Documents/trade_orchestrator/.env | cut -d= -f2- | tr -d '\"')"
go run ./cmd/strateval sync-deployed -allowlist "$STRATEGY_ALLOWLIST"
# or: -db reports/strategies.db
```
