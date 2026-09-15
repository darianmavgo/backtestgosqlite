package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/olekukonko/tablewriter"
)

func main() {
	dbPath := flag.String("db", "", "Path to shared-account SQLite results database (defaults to latest in reports/)")
	flag.Parse()

	targetDB := *dbPath
	if targetDB == "" {
		// Find latest shared_*.db in reports/
		files, err := filepath.Glob("reports/shared_*.db")
		if err != nil || len(files) == 0 {
			log.Fatalf("No shared account database found in reports/. Run backtest first.")
		}
		// Sort by mod time descending
		var latest string
		var latestMod int64
		for _, f := range files {
			if fi, err := os.Stat(f); err == nil {
				if fi.ModTime().UnixNano() > latestMod {
					latestMod = fi.ModTime().UnixNano()
					latest = f
				}
			}
		}
		targetDB = latest
	}

	fmt.Printf("\n🔍 Running Go SQL Control Audit on: %s\n\n", targetDB)

	db, err := sqlx.Open("sqlite3", targetDB+"?_journal_mode=WAL&_busy_timeout=15000")
	if err != nil {
		log.Fatalf("Failed to open database %s: %v", targetDB, err)
	}
	defer db.Close()

	combinedID := resolveCombinedID(db)

	// 1. Quantitative Breakdown by Strategy & Exit Reason
	runStrategyExitReasonAudit(db)

	// 2. Preempted Positions Audit
	runPreemptedPositionsAudit(db)

	// 3. Performance Summary
	runPerformanceSummaryAudit(db, combinedID)

	// 4. Calendar Year Performance
	runCalendarYearAudit(db, combinedID)
}

// resolveCombinedID identifies the shared account's consolidated-portfolio
// strategy_id. Per-strategy attribution rows only ever get written to
// performance_summary (see pkg/runner.ExecuteSharedAccount); only the combined
// row also has entries in equity_curve, trades, and signals — so that's the
// reliable way to find it regardless of what it's named. Older databases used
// the generic literal "SHARED_ACCOUNT"; newer ones use
// runner.SharedAccountID(primary, secondaries) (e.g. "sig-voo-buy-tecl+mara_tree").
// Falls back to the legacy literal if the lookup comes up empty.
func resolveCombinedID(db *sqlx.DB) string {
	var id string
	if err := db.Get(&id, `SELECT strategy_id FROM equity_curve LIMIT 1`); err == nil && id != "" {
		return id
	}
	return "SHARED_ACCOUNT"
}

func runStrategyExitReasonAudit(db *sqlx.DB) {
	query := `
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
	`
	type Row struct {
		Strategy     string  `db:"strategy"`
		ExitReason   string  `db:"exit_reason"`
		TradeCount   int     `db:"trade_count"`
		WinRatePct   float64 `db:"win_rate_pct"`
		TotalNetPnL  float64 `db:"total_net_pnl"`
		AvgTradePnL  float64 `db:"avg_trade_pnl"`
		AvgReturnPct float64 `db:"avg_return_pct"`
		AvgHoldDays  float64 `db:"avg_hold_days"`
	}

	var rows []Row
	if err := db.Select(&rows, query); err != nil {
		log.Printf("Error running strategy/exit query: %v", err)
		return
	}

	fmt.Printf("========================================================================================================================\n")
	fmt.Printf("📊 SECTION 1: EXECUTION BREAKDOWN BY STRATEGY & EXIT REASON\n")
	fmt.Printf("========================================================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Strategy ID", "Exit Trigger Reason", "Trades", "Win Rate %", "Total Net PnL", "Avg Trade PnL", "Avg Return %", "Avg Hold"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, r := range rows {
		table.Append([]string{
			r.Strategy,
			r.ExitReason,
			fmt.Sprintf("%d", r.TradeCount),
			fmt.Sprintf("%.1f%%", r.WinRatePct),
			fmt.Sprintf("$%.2f", r.TotalNetPnL),
			fmt.Sprintf("$%.2f", r.AvgTradePnL),
			fmt.Sprintf("%+.2f%%", r.AvgReturnPct),
			fmt.Sprintf("%.1f days", r.AvgHoldDays),
		})
	}
	table.Render()
	fmt.Println()
}

func runPreemptedPositionsAudit(db *sqlx.DB) {
	query := `
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
		ORDER BY exit_date DESC;
	`
	type Row struct {
		TradeID       int     `db:"trade_id"`
		StrategyID    string  `db:"strategy_id"`
		Symbol        string  `db:"symbol"`
		EntryDate     string  `db:"entry_date"`
		EntryPrice    float64 `db:"entry_price"`
		PreemptedDate string  `db:"preempted_date"`
		ExitPrice     float64 `db:"exit_price"`
		HoldDays      int     `db:"hold_days"`
		NetPnL        float64 `db:"net_pnl"`
		ReturnPct     float64 `db:"return_pct"`
	}

	var rows []Row
	if err := db.Select(&rows, query); err != nil {
		log.Printf("Error running preemption query: %v", err)
		return
	}

	fmt.Printf("========================================================================================================================\n")
	fmt.Printf("⚡ SECTION 2: AUDIT OF PREEMPTED SECONDARY POSITIONS (Total: %d Preempted)\n", len(rows))
	fmt.Printf("   Liquidated at market to free cash for primary (sig-voo-buy-tecl) buy signals\n")
	fmt.Printf("========================================================================================================================\n")

	if len(rows) == 0 {
		fmt.Printf("No positions were preempted (cash was always sufficient).\n\n")
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Trade ID", "Strategy", "Symbol", "Entry Date", "Entry $", "Preempt Date", "Exit $", "Hold Days", "Preemption PnL", "Return %"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	displayCount := len(rows)
	if displayCount > 25 {
		displayCount = 25
	}

	for _, r := range rows[:displayCount] {
		table.Append([]string{
			fmt.Sprintf("%d", r.TradeID),
			r.StrategyID,
			r.Symbol,
			r.EntryDate,
			fmt.Sprintf("$%.2f", r.EntryPrice),
			r.PreemptedDate,
			fmt.Sprintf("$%.2f", r.ExitPrice),
			fmt.Sprintf("%d d", r.HoldDays),
			fmt.Sprintf("$%.2f", r.NetPnL),
			fmt.Sprintf("%+.2f%%", r.ReturnPct),
		})
	}
	table.Render()
	if len(rows) > displayCount {
		fmt.Printf("... displaying most recent %d of %d preempted positions\n", displayCount, len(rows))
	}
	fmt.Println()
}

func runPerformanceSummaryAudit(db *sqlx.DB, combinedID string) {
	query := `
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
		ORDER BY CASE WHEN strategy_id = ? THEN 0 ELSE 1 END, net_profit DESC;
	`
	type Row struct {
		StrategyID     string  `db:"strategy_id"`
		InitialCapital float64 `db:"initial_capital"`
		FinalEquity    float64 `db:"final_equity"`
		NetProfit      float64 `db:"net_profit"`
		TotalReturnPct float64 `db:"total_return_pct"`
		CAGRPct        float64 `db:"cagr_pct"`
		SharpeRatio    float64 `db:"sharpe_ratio"`
		SortinoRatio   float64 `db:"sortino_ratio"`
		MaxDDPct       float64 `db:"max_dd_pct"`
		TotalTrades    int     `db:"total_trades"`
		WinRatePct     float64 `db:"win_rate_pct"`
		ProfitFactor   float64 `db:"profit_factor"`
	}

	var rows []Row
	if err := db.Select(&rows, query, combinedID); err != nil {
		log.Printf("Error running performance summary query: %v", err)
		return
	}

	fmt.Printf("========================================================================================================================\n")
	fmt.Printf("📈 SECTION 3: PERFORMANCE SUMMARY TABLE\n")
	fmt.Printf("========================================================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Strategy ID", "Initial $", "Final Equity", "Net Realized PnL", "Total Return", "CAGR", "Sharpe", "Sortino", "Max DD %", "Trades", "Win Rate", "Profit Factor"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, r := range rows {
		name := r.StrategyID
		if name == combinedID {
			name = "🏛️ " + name + " (Combined)"
		} else if strings.Contains(name, "voo-tecl") {
			name = "⭐ " + name + " (Primary)"
		} else {
			name = "⚡ " + name + " (Secondary)"
		}
		table.Append([]string{
			name,
			fmt.Sprintf("$%.2f", r.InitialCapital),
			fmt.Sprintf("$%.2f", r.FinalEquity),
			fmt.Sprintf("$%.2f", r.NetProfit),
			fmt.Sprintf("%+.2f%%", r.TotalReturnPct),
			fmt.Sprintf("%.2f%%", r.CAGRPct),
			fmt.Sprintf("%.2f", r.SharpeRatio),
			fmt.Sprintf("%.2f", r.SortinoRatio),
			fmt.Sprintf("%.2f%%", r.MaxDDPct),
			fmt.Sprintf("%d", r.TotalTrades),
			fmt.Sprintf("%.1f%%", r.WinRatePct),
			fmt.Sprintf("%.2f", r.ProfitFactor),
		})
	}
	table.Render()
	fmt.Println()
}

func runCalendarYearAudit(db *sqlx.DB, combinedID string) {
	query := `
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
			WHERE strategy_id = ?
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
			WHERE strategy_id = ?
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
	`
	type Row struct {
		CalYear        string  `db:"calendar_year"`
		StartDate      string  `db:"year_start_date"`
		EndDate        string  `db:"year_end_date"`
		StartingEquity float64 `db:"starting_equity"`
		EndingEquity   float64 `db:"ending_equity"`
		NetPnL         float64 `db:"net_pnl"`
		YearReturnPct  float64 `db:"year_return_pct"`
		MaxDDPct       float64 `db:"max_drawdown_pct"`
		AvgIdleCashPct float64 `db:"avg_idle_cash_pct"`
		TradingDays    int     `db:"trading_days"`
	}

	var rows []Row
	if err := db.Select(&rows, query, combinedID, combinedID); err != nil {
		log.Printf("Error running calendar year query: %v", err)
		return
	}

	fmt.Printf("========================================================================================================================\n")
	fmt.Printf("📅 SECTION 4: CALENDAR-YEAR RETURNS & CAPITAL UTILIZATION (SHARED ACCOUNT)\n")
	fmt.Printf("========================================================================================================================\n")

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Calendar Year", "Period Window", "Starting $", "Ending $", "Annual Net PnL", "Annual Return %", "Max Drawdown %", "Avg Idle Cash %", "Days"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, r := range rows {
		table.Append([]string{
			r.CalYear,
			fmt.Sprintf("%s ➔ %s", r.StartDate, r.EndDate),
			fmt.Sprintf("$%.2f", r.StartingEquity),
			fmt.Sprintf("$%.2f", r.EndingEquity),
			fmt.Sprintf("$%.2f", r.NetPnL),
			fmt.Sprintf("%+.2f%%", r.YearReturnPct),
			fmt.Sprintf("%.2f%%", r.MaxDDPct),
			fmt.Sprintf("%.1f%%", r.AvgIdleCashPct),
			fmt.Sprintf("%d", r.TradingDays),
		})
	}
	table.Render()
	fmt.Println()
}
