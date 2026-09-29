DROP TABLE IF EXISTS walk_forward_summary;
CREATE TABLE walk_forward_summary AS
SELECT
	strategy_id,
	COUNT(*) AS folds,
	AVG(is_sharpe) AS is_sharpe,
	AVG(oos_sharpe) AS oos_sharpe,
	AVG(is_return_pct) AS is_return_pct,
	AVG(oos_return_pct) AS oos_return_pct,
	SUM(oos_trades) AS oos_trades,
	SUM(CASE WHEN oos_return_pct > 0 THEN 1 ELSE 0 END) AS oos_positive_folds,
	MAX(trials) AS trials
FROM walk_forward_fold
GROUP BY strategy_id;
