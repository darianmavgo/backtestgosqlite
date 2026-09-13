-- ==============================================================================
-- SHARED-ACCOUNT MULTI-STRATEGY AUDIT & PERFORMANCE ANALYSIS (SQLITE)
-- ==============================================================================
-- Strategy Hierarchy:
--   Primary:   voo-tecl-combo (Priority 0: Capital Precedence)
--   Secondary: bb-capitulation (Priority 1: Idle Cash Utilization)
-- Preemption:  Secondary positions liquidated at market if primary signal needs cash
-- ==============================================================================

-- 1. Quantitative Breakdown by Strategy and Exit Reason
SELECT
    COALESCE(strategy_id, 'UNKNOWN') AS strategy,
    exit_reason,
    COUNT(*) AS trade_count,
    ROUND(SUM(CASE WHEN net_pnl > 0 THEN 1 ELSE 0 END) * 100.0 / COUNT(*), 1) AS win_rate_pct,
    ROUND(SUM(net_pnl), 2) AS total_net_pnl,
    ROUND(AVG(net_pnl), 2) AS avg_trade_pnl,
    ROUND(AVG(return_pct) * 100, 2) AS avg_return_pct,
    ROUND(AVG(hold_days), 1) AS avg_hold_days
FROM trades
GROUP BY strategy_id, exit_reason
ORDER BY strategy_id, trade_count DESC;

-- 2. Audit All Preemption Events (Secondary Liquidated by Primary Buy Signals)
SELECT
    id AS trade_id,
    strategy_id,
    symbol,
    entry_date,
    ROUND(entry_price, 2) AS entry_price,
    exit_date AS preempted_date,
    ROUND(exit_price, 2) AS exit_price,
    hold_days,
    ROUND(net_pnl, 2) AS net_pnl,
    ROUND(return_pct * 100, 2) AS return_pct
FROM trades
WHERE exit_reason = 'PREEMPTED_BY_PRIMARY'
ORDER BY exit_date;

-- 3. Performance Summary Comparison Table
SELECT
    strategy_id,
    ROUND(initial_capital, 2) AS initial_capital,
    ROUND(final_equity, 2) AS final_equity,
    ROUND(net_profit, 2) AS net_profit,
    ROUND(total_return_pct * 100, 2) AS total_return_pct,
    ROUND(cagr * 100, 2) AS cagr_pct,
    ROUND(sharpe_ratio, 2) AS sharpe_ratio,
    ROUND(sortino_ratio, 2) AS sortino_ratio,
    ROUND(max_drawdown_pct * 100, 2) AS max_dd_pct,
    total_trades,
    ROUND(win_rate * 100, 1) AS win_rate_pct,
    ROUND(profit_factor, 2) AS profit_factor
FROM performance_summary
ORDER BY CASE WHEN strategy_id = 'SHARED_ACCOUNT' THEN 0 ELSE 1 END, net_profit DESC;

-- 4. Calendar-Year Returns of the Consolidated Shared Account
WITH daily_ranks AS (
    SELECT
        strftime('%Y', date) AS cal_year,
        date,
        total_equity,
        cash,
        invested,
        drawdown_pct,
        ROW_NUMBER() OVER (PARTITION BY strftime('%Y', date) ORDER BY date ASC) AS rn_start,
        ROW_NUMBER() OVER (PARTITION BY strftime('%Y', date) ORDER BY date DESC) AS rn_end
    FROM equity_curve
    WHERE strategy_id = 'SHARED_ACCOUNT'
),
year_bounds AS (
    SELECT
        s.cal_year,
        s.date AS year_start_date,
        s.total_equity AS year_open_equity,
        e.date AS year_end_date,
        e.total_equity AS year_close_equity
    FROM (SELECT cal_year, date, total_equity FROM daily_ranks WHERE rn_start = 1) s
    JOIN (SELECT cal_year, date, total_equity FROM daily_ranks WHERE rn_end = 1) e
      ON s.cal_year = e.cal_year
),
year_stats AS (
    SELECT
        strftime('%Y', date) AS cal_year,
        MAX(drawdown_pct) AS max_year_dd,
        AVG(cash / total_equity) AS avg_cash_pct,
        COUNT(*) AS trading_days
    FROM equity_curve
    WHERE strategy_id = 'SHARED_ACCOUNT'
    GROUP BY strftime('%Y', date)
)
SELECT
    b.cal_year AS calendar_year,
    b.year_start_date,
    b.year_end_date,
    ROUND(b.year_open_equity, 2) AS starting_equity,
    ROUND(b.year_close_equity, 2) AS ending_equity,
    ROUND(b.year_close_equity - b.year_open_equity, 2) AS net_pnl,
    ROUND(((b.year_close_equity - b.year_open_equity) / b.year_open_equity) * 100, 2) AS year_return_pct,
    ROUND(s.max_year_dd * 100, 2) AS max_drawdown_pct,
    ROUND(s.avg_cash_pct * 100, 1) AS avg_idle_cash_pct,
    s.trading_days
FROM year_bounds b
JOIN year_stats s ON b.cal_year = s.cal_year
ORDER BY b.cal_year ASC;
