# Strategy eval loop (promote → scan)

Additive tooling. **Does not** change `cmd/backtest`, `cmd/gridsearch`, `cmd/scoreboard`,
evening-scan, cron, or `STRATEGY_ALLOWLIST` application.

## Pipeline

1. **IS backtest** on history before the holdout window  
2. Optional **`--optimize`** — coarse grid on **IS only** (never peeks OOS)  
3. **OOS** locked evaluation (default last 12 months)  
4. **Tier** A/B/C/D from hard gates  
5. **Allowlist diff** printed only — you edit `STRATEGY_ALLOWLIST` manually  

## Commands

```bash
# List registry
go run ./cmd/strateval -list

# Evaluate one / many / all
go run ./cmd/strateval -strategy sig_voo_buy_tecl
go run ./cmd/strateval -strategy sig_voo_buy_tecl,gld_decline -optimize
go run ./cmd/strateval -strategy all -allowlist "$STRATEGY_ALLOWLIST"

# Re-print scorecard + allowlist proposal from DB
go run ./cmd/strateval report -db reports/strategy_evals.db -allowlist "$STRATEGY_ALLOWLIST"
```

Results: `reports/strategy_evals.db` (table `strategy_evals`).

## Default gates (tier A)

- OOS trades ≥ 30  
- OOS Sharpe > 0  
- OOS avg trade return > 0  
- OOS max DD ≤ 1.5 × IS max DD  

Tier **D** = on allowlist but failing gates (demote candidate).

## Safety

- Existing jobs unchanged unless you opt into calling `strateval`  
- No auto-write of `.env` / `STRATEGY_ALLOWLIST`  
- Optimizer cannot see OOS dates  

## Suggested cadence

Weekly (or after research changes):

```bash
go run ./cmd/strateval -strategy all -optimize -allowlist "$STRATEGY_ALLOWLIST" \
  | tee reports/strateval_latest.md
```

Then manually promote tier-A adds / demote tier-D from the printed proposal.
