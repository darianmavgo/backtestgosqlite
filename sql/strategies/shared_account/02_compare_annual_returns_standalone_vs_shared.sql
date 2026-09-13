-- ==============================================================================
-- Strategy Comparison: VOO-TECL Combo Standalone vs. 2-Strategy Shared Account
-- File: 02_compare_annual_returns_standalone_vs_shared.sql
-- Description: Pure SQLite comparison of annual returns, equity, drawdowns, 
--              idle cash utilization, and alpha vs. VOO benchmark.
--
-- Prerequisite Databases:
--   - Main DB: reports/shared_voo-tecl-combo_bb-capitulation_2.db (Shared Account)
--   - Attached: reports/voo-tecl-combo_4.db AS standalone
--   - Attached: data/market_history.db AS market
-- ==============================================================================

ATTACH DATABASE 'reports/voo-tecl-combo_4.db' AS standalone;
ATTACH DATABASE 'data/market_history.db' AS market;

WITH yearly_ranges AS (
    SELECT 
        strftime('%Y', date) AS year,
        MIN(date) AS first_date,
        MAX(date) AS last_date,
        COUNT(*) AS trading_days
    FROM equity_curve
    WHERE strategy_id = 'SHARED_ACCOUNT'
    GROUP BY strftime('%Y', date)
),
prior_year_links AS (
    SELECT 
        year,
        first_date,
        last_date,
        trading_days,
        LAG(last_date) OVER (ORDER BY year) AS prior_last_date
    FROM yearly_ranges
),
shared_equity_points AS (
    SELECT 
        p.year,
        COALESCE(p.prior_last_date, p.first_date) AS start_date,
        p.last_date AS end_date,
        p.trading_days,
        COALESCE(e_start.total_equity, 100000.0) AS shared_start_eq,
        e_end.total_equity AS shared_end_eq,
        julianday(p.last_date) - julianday(COALESCE(p.prior_last_date, p.first_date)) AS calendar_days
    FROM prior_year_links p
    LEFT JOIN equity_curve e_start ON e_start.strategy_id = 'SHARED_ACCOUNT' AND e_start.date = p.prior_last_date
    JOIN equity_curve e_end ON e_end.strategy_id = 'SHARED_ACCOUNT' AND e_end.date = p.last_date
),
standalone_equity_points AS (
    SELECT 
        p.year,
        COALESCE(s_start.total_equity, 100000.0) AS stand_start_eq,
        s_end.total_equity AS stand_end_eq
    FROM prior_year_links p
    LEFT JOIN standalone.equity_curve s_start ON s_start.date = p.prior_last_date
    JOIN standalone.equity_curve s_end ON s_end.date = p.last_date
),
voo_prices AS (
    SELECT date, close 
    FROM market.backtest_start 
    WHERE symbol = 'VOO'
),
shared_year_stats AS (
    SELECT 
        strftime('%Y', date) AS year,
        MAX(drawdown_pct) AS shared_max_dd,
        AVG(cash / total_equity) AS shared_avg_cash
    FROM equity_curve
    WHERE strategy_id = 'SHARED_ACCOUNT'
    GROUP BY strftime('%Y', date)
),
standalone_year_stats AS (
    SELECT 
        strftime('%Y', date) AS year,
        MAX(drawdown_pct) AS stand_max_dd,
        AVG(cash / total_equity) AS stand_avg_cash
    FROM standalone.equity_curve
    GROUP BY strftime('%Y', date)
),
combined AS (
    SELECT 
        sep.year,
        sep.start_date,
        sep.end_date,
        sep.trading_days,
        sep.calendar_days,
        sep.shared_start_eq,
        sep.shared_end_eq,
        sta.stand_start_eq,
        sta.stand_end_eq,
        sys.shared_max_dd,
        sys.shared_avg_cash,
        sas.stand_max_dd,
        sas.stand_avg_cash,
        COALESCE(v_start.close, (SELECT close FROM voo_prices WHERE date >= sep.start_date ORDER BY date ASC LIMIT 1)) AS voo_start,
        COALESCE(v_end.close, (SELECT close FROM voo_prices WHERE date <= sep.end_date ORDER BY date DESC LIMIT 1)) AS voo_end
    FROM shared_equity_points sep
    JOIN standalone_equity_points sta ON sep.year = sta.year
    JOIN shared_year_stats sys ON sep.year = sys.year
    JOIN standalone_year_stats sas ON sep.year = sas.year
    LEFT JOIN voo_prices v_start ON v_start.date = sep.start_date
    LEFT JOIN voo_prices v_end ON v_end.date = sep.end_date
)
SELECT 
    year,
    start_date || ' to ' || end_date AS horizon,
    trading_days,
    calendar_days,
    -- Price & Equity Levels
    ROUND(voo_start, 2) AS voo_start,
    ROUND(voo_end, 2) AS voo_end,
    ROUND(stand_start_eq, 2) AS standalone_start_eq,
    ROUND(stand_end_eq, 2) AS standalone_end_eq,
    ROUND(shared_start_eq, 2) AS shared_start_eq,
    ROUND(shared_end_eq, 2) AS shared_end_eq,
    -- Period Returns
    ROUND((voo_end - voo_start) / voo_start * 100.0, 2) AS voo_return_pct,
    ROUND((stand_end_eq - stand_start_eq) / stand_start_eq * 100.0, 2) AS standalone_return_pct,
    ROUND((shared_end_eq - shared_start_eq) / shared_start_eq * 100.0, 2) AS shared_return_pct,
    ROUND(((shared_end_eq - shared_start_eq) / shared_start_eq - (stand_end_eq - stand_start_eq) / stand_start_eq) * 100.0, 2) AS spread_vs_standalone_pct,
    -- Ending Portfolios & Dollar Diff
    ROUND(shared_end_eq - stand_end_eq, 2) AS shared_dollar_diff,
    -- Max Drawdown within year
    ROUND(stand_max_dd * 100.0, 2) AS standalone_max_dd_pct,
    ROUND(shared_max_dd * 100.0, 2) AS shared_max_dd_pct,
    -- Idle Cash Utilization
    ROUND(stand_avg_cash * 100.0, 1) AS standalone_idle_cash_pct,
    ROUND(shared_avg_cash * 100.0, 1) AS shared_idle_cash_pct
FROM combined
ORDER BY year ASC;
