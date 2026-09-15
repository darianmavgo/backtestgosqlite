-- ==============================================================================
-- Strategy: VOO -> TECL All-Weather Combo
-- File: 04_calc_annual_returns_vs_voo.sql
-- Description: Pure SQLite calculation of Period Returns, Annualized CAGR, 
--              and Excess Alpha vs VOO (S&P 500) per calendar year.
--
-- Prerequisite Databases:
--   - Main DB: reports/voo-tecl-combo.db (or voo-tecl-combo_4.db) containing equity_curve
--   - Attached DB: data/market_history.db AS market containing backtest_start
--
-- Usage via SQLite CLI:
--   sqlite3 reports/voo-tecl-combo_4.db < sql/strategies/voo_tecl_combo/04_calc_annual_returns_vs_voo.sql
-- ==============================================================================

ATTACH DATABASE 'data/market_history.db' AS market;

WITH yearly_ranges AS (
    SELECT 
        strftime('%Y', date) AS year,
        MIN(date) AS first_date,
        MAX(date) AS last_date,
        COUNT(*) AS trading_days
    FROM equity_curve
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
equity_points AS (
    SELECT 
        p.year,
        COALESCE(p.prior_last_date, p.first_date) AS start_date,
        p.last_date AS end_date,
        p.trading_days,
        -- Initial capital ($100,000) for first year, or prior year-end equity
        COALESCE(e_start.total_equity, 100000.0) AS strat_start_eq,
        e_end.total_equity AS strat_end_eq,
        julianday(p.last_date) - julianday(COALESCE(p.prior_last_date, p.first_date)) AS calendar_days
    FROM prior_year_links p
    LEFT JOIN equity_curve e_start ON e_start.date = p.prior_last_date
    JOIN equity_curve e_end ON e_end.date = p.last_date
),
voo_prices AS (
    SELECT date, close 
    FROM market.backtest_start 
    WHERE symbol = 'VOO'
),
combined AS (
    SELECT 
        ep.year,
        ep.start_date,
        ep.end_date,
        ep.trading_days,
        ep.calendar_days,
        ep.strat_start_eq,
        ep.strat_end_eq,
        COALESCE(v_start.close, (SELECT close FROM voo_prices WHERE date >= ep.start_date ORDER BY date ASC LIMIT 1)) AS voo_start,
        COALESCE(v_end.close, (SELECT close FROM voo_prices WHERE date <= ep.end_date ORDER BY date DESC LIMIT 1)) AS voo_end
    FROM equity_points ep
    LEFT JOIN voo_prices v_start ON v_start.date = ep.start_date
    LEFT JOIN voo_prices v_end ON v_end.date = ep.end_date
),
annual_metrics AS (
    SELECT 
        year,
        start_date || ' to ' || end_date AS horizon,
        trading_days,
        strat_start_eq,
        strat_end_eq,
        ROUND((strat_end_eq - strat_start_eq) / strat_start_eq * 100.0, 2) AS strat_return_pct,
        ROUND((voo_end - voo_start) / voo_start * 100.0, 2) AS voo_return_pct,
        ROUND((POWER(strat_end_eq / strat_start_eq, 365.25 / calendar_days) - 1.0) * 100.0, 2) AS strat_cagr_pct,
        ROUND((POWER(voo_end / voo_start, 365.25 / calendar_days) - 1.0) * 100.0, 2) AS voo_cagr_pct,
        ROUND(((strat_end_eq - strat_start_eq) / strat_start_eq - (voo_end - voo_start) / voo_start) * 100.0, 2) AS excess_alpha_pct
    FROM combined
),
overall_totals AS (
    SELECT 
        'Total / 5-Yr' AS year,
        MIN(start_date) || ' to ' || MAX(end_date) AS horizon,
        SUM(trading_days) AS trading_days,
        MIN(strat_start_eq) AS strat_start_eq,
        MAX(strat_end_eq) AS strat_end_eq,
        ROUND((MAX(strat_end_eq) - 100000.0) / 100000.0 * 100.0, 2) AS strat_return_pct,
        ROUND((MAX(voo_end) - MIN(voo_start)) / MIN(voo_start) * 100.0, 2) AS voo_return_pct,
        ROUND((POWER(MAX(strat_end_eq) / 100000.0, 365.25 / (julianday(MAX(end_date)) - julianday(MIN(start_date)))) - 1.0) * 100.0, 2) AS strat_cagr_pct,
        ROUND((POWER(MAX(voo_end) / MIN(voo_start), 365.25 / (julianday(MAX(end_date)) - julianday(MIN(start_date)))) - 1.0) * 100.0, 2) AS voo_cagr_pct,
        ROUND(((MAX(strat_end_eq) - 100000.0) / 100000.0 - (MAX(voo_end) - MIN(voo_start)) / MIN(voo_start)) * 100.0, 2) AS excess_alpha_pct
    FROM combined
)
SELECT 
    year,
    horizon,
    trading_days,
    strat_return_pct,
    voo_return_pct,
    excess_alpha_pct,
    strat_cagr_pct,
    voo_cagr_pct
FROM annual_metrics
UNION ALL
SELECT 
    year,
    horizon,
    trading_days,
    strat_return_pct,
    voo_return_pct,
    excess_alpha_pct,
    strat_cagr_pct,
    voo_cagr_pct
FROM overall_totals;
