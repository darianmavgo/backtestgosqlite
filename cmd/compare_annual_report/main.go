package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
	"github.com/olekukonko/tablewriter"
)

//go:embed template.html
var htmlTemplate string

// resolveCombinedID identifies the shared account's consolidated-portfolio
// strategy_id (see the identical helper and its full rationale in
// cmd/audit_shared/main.go). Falls back to the legacy "SHARED_ACCOUNT" literal
// for older databases if the lookup comes up empty.
func resolveCombinedID(db *sqlx.DB) string {
	var id string
	if err := db.Get(&id, `SELECT strategy_id FROM equity_curve LIMIT 1`); err == nil && id != "" {
		return id
	}
	return "SHARED_ACCOUNT"
}

// initialCapitalOf reads a run's actual starting capital from its
// performance_summary table — the same column cmd/audit_shared already
// trusts for this. Standalone and shared-account runs are meant to be
// compared on a level footing, but that only holds if we normalize every
// return/CAGR by each run's own actual starting capital rather than
// assuming both used the default $100,000; a run backtested with a
// different -capital would otherwise silently get a wrong (and unfair)
// comparison. Falls back to $100,000 only if the column is missing (e.g. an
// older result DB predating this column).
func initialCapitalOf(db *sqlx.DB, table string) float64 {
	var capital float64
	if err := db.Get(&capital, fmt.Sprintf("SELECT initial_capital FROM %s LIMIT 1", table)); err != nil || capital <= 0 {
		log.Printf("Warning: could not read initial_capital from %s (%v) — defaulting to $100,000", table, err)
		return 100000.0
	}
	return capital
}

type AnnualComparisonRow struct {
	Year                  string  `db:"year" json:"year"`
	Horizon               string  `db:"horizon" json:"horizon"`
	TradingDays           int     `db:"trading_days" json:"trading_days"`
	CalendarDays          float64 `db:"calendar_days" json:"calendar_days"`
	VOOStart              float64 `db:"voo_start" json:"voo_start"`
	VOOEnd                float64 `db:"voo_end" json:"voo_end"`
	StandaloneStartEq     float64 `db:"standalone_start_eq" json:"standalone_start_eq"`
	StandaloneEndEq       float64 `db:"standalone_end_eq" json:"standalone_end_eq"`
	SharedStartEq         float64 `db:"shared_start_eq" json:"shared_start_eq"`
	SharedEndEq           float64 `db:"shared_end_eq" json:"shared_end_eq"`
	VOOReturnPct          float64 `db:"voo_return_pct" json:"voo_return_pct"`
	StandaloneReturnPct   float64 `db:"standalone_return_pct" json:"standalone_return_pct"`
	SharedReturnPct       float64 `db:"shared_return_pct" json:"shared_return_pct"`
	SpreadVsStandalonePct float64 `db:"spread_vs_standalone_pct" json:"spread_vs_standalone_pct"`
	VOOCAGRPct            float64 `json:"voo_cagr_pct"`
	StandaloneCAGRPct     float64 `json:"standalone_cagr_pct"`
	SharedCAGRPct         float64 `json:"shared_cagr_pct"`
	SharedDollarDiff      float64 `db:"shared_dollar_diff" json:"shared_dollar_diff"`
	StandaloneMaxDDPct    float64 `db:"standalone_max_dd_pct" json:"standalone_max_dd_pct"`
	SharedMaxDDPct        float64 `db:"shared_max_dd_pct" json:"shared_max_dd_pct"`
	StandaloneIdleCashPct float64 `db:"standalone_idle_cash_pct" json:"standalone_idle_cash_pct"`
	SharedIdleCashPct     float64 `db:"shared_idle_cash_pct" json:"shared_idle_cash_pct"`
}

type DailyPoint struct {
	Date   string  `db:"date" json:"date"`
	Equity float64 `db:"total_equity" json:"total_equity"`
	Cash   float64 `db:"cash" json:"cash"`
}

type PreemptedTrade struct {
	ID         int     `db:"id" json:"id"`
	Symbol     string  `db:"symbol" json:"symbol"`
	EntryDate  string  `db:"entry_date" json:"entry_date"`
	EntryPrice float64 `db:"entry_price" json:"entry_price"`
	ExitDate   string  `db:"exit_date" json:"exit_date"`
	ExitPrice  float64 `db:"exit_price" json:"exit_price"`
	HoldDays   int     `db:"hold_days" json:"hold_days"`
	NetPnL     float64 `db:"net_pnl" json:"net_pnl"`
	ReturnPct  float64 `db:"return_pct" json:"return_pct"`
}

func main() {
	sharedDBPath := flag.String("shared-db", "reports/shared_sig-voo-buy-tecl_bb-capitulation_2.db", "Path to shared account SQLite database")
	standaloneDBPath := flag.String("standalone-db", "reports/sig-voo-buy-tecl_4.db", "Path to standalone sig-voo-buy-tecl SQLite database")
	marketDBPath := flag.String("market-db", "data/market_history.db", "Path to market history SQLite database")
	htmlOut := flag.String("html", "reports/annual_comparison_standalone_vs_shared.html", "Path to export HTML comparison report")
	flag.Parse()

	// Ensure HTML report lands in reports/ directory
	if *htmlOut != "" && !filepath.IsAbs(*htmlOut) && !strings.HasPrefix(*htmlOut, "reports/") && !strings.HasPrefix(*htmlOut, "reports"+string(filepath.Separator)) {
		*htmlOut = filepath.Join("reports", *htmlOut)
	}

	fmt.Println("\n========================================================================================================================")
	fmt.Println("⚖️ ANNUAL PERFORMANCE COMPARISON: VOO-TECL COMBO vs. 2-STRATEGY SHARED ACCOUNT")
	fmt.Printf("   Shared DB:     %s\n", *sharedDBPath)
	fmt.Printf("   Standalone DB: %s\n", *standaloneDBPath)
	fmt.Printf("   Market DB:     %s\n", *marketDBPath)
	fmt.Println("========================================================================================================================")

	db, err := sqlx.Open("sqlite", *sharedDBPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(15000)")
	if err != nil {
		log.Fatalf("Failed to open shared DB: %v", err)
	}
	defer db.Close()

	// Attach standalone and market databases
	attachStandalone := fmt.Sprintf("ATTACH DATABASE '%s' AS standalone;", *standaloneDBPath)
	if _, err := db.Exec(attachStandalone); err != nil {
		log.Fatalf("Failed to attach standalone DB %s: %v", *standaloneDBPath, err)
	}

	attachMarket := fmt.Sprintf("ATTACH DATABASE '%s' AS market;", *marketDBPath)
	if _, err := db.Exec(attachMarket); err != nil {
		log.Fatalf("Failed to attach market DB %s: %v", *marketDBPath, err)
	}

	// 1. Run Year-by-Year Comparison Query
	query := `
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
			voo_start,
			voo_end,
			stand_start_eq AS standalone_start_eq,
			stand_end_eq AS standalone_end_eq,
			shared_start_eq AS shared_start_eq,
			shared_end_eq AS shared_end_eq,
			ROUND((voo_end - voo_start) / voo_start * 100.0, 2) AS voo_return_pct,
			ROUND((stand_end_eq - stand_start_eq) / stand_start_eq * 100.0, 2) AS standalone_return_pct,
			ROUND((shared_end_eq - shared_start_eq) / shared_start_eq * 100.0, 2) AS shared_return_pct,
			ROUND(((shared_end_eq - shared_start_eq) / shared_start_eq - (stand_end_eq - stand_start_eq) / stand_start_eq) * 100.0, 2) AS spread_vs_standalone_pct,
			ROUND(shared_end_eq - stand_end_eq, 2) AS shared_dollar_diff,
			ROUND(stand_max_dd * 100.0, 2) AS standalone_max_dd_pct,
			ROUND(shared_max_dd * 100.0, 2) AS shared_max_dd_pct,
			ROUND(stand_avg_cash * 100.0, 1) AS standalone_idle_cash_pct,
			ROUND(shared_avg_cash * 100.0, 1) AS shared_idle_cash_pct
		FROM combined
		ORDER BY year ASC;
	`

	// The combined-portfolio strategy_id used to be the generic literal
	// "SHARED_ACCOUNT"; it's now runner.SharedAccountID(primary, secondaries)
	// (e.g. "sig-voo-buy-tecl+mara_tree") so it's visible directly in reports.
	// Resolve it the same way cmd/audit_shared does (the combined row is the
	// only one with equity_curve entries) and substitute it into the query.
	combinedID := resolveCombinedID(db)
	query = strings.ReplaceAll(query, "'SHARED_ACCOUNT'", "'"+combinedID+"'")

	// Substitute each run's actual starting capital in place of the
	// hardcoded $100,000 fallback (see initialCapitalOf) so year-1 return/
	// CAGR stays correct even when a run wasn't backtested with -capital
	// 100000.
	sharedInitialCapital := initialCapitalOf(db, "performance_summary")
	standaloneInitialCapital := initialCapitalOf(db, "standalone.performance_summary")
	query = strings.ReplaceAll(query, "COALESCE(e_start.total_equity, 100000.0)", "COALESCE(e_start.total_equity, "+strconv.FormatFloat(sharedInitialCapital, 'f', 2, 64)+")")
	query = strings.ReplaceAll(query, "COALESCE(s_start.total_equity, 100000.0)", "COALESCE(s_start.total_equity, "+strconv.FormatFloat(standaloneInitialCapital, 'f', 2, 64)+")")

	var rows []AnnualComparisonRow
	if err := db.Select(&rows, query); err != nil {
		log.Fatalf("Failed to execute comparison query: %v", err)
	}

	// Calculate CAGRs in Go
	for i := range rows {
		if rows[i].CalendarDays > 0 {
			if rows[i].VOOStart > 0 && rows[i].VOOEnd > 0 {
				rows[i].VOOCAGRPct = (math.Pow(rows[i].VOOEnd/rows[i].VOOStart, 365.25/rows[i].CalendarDays) - 1.0) * 100.0
			}
			if rows[i].StandaloneStartEq > 0 && rows[i].StandaloneEndEq > 0 {
				rows[i].StandaloneCAGRPct = (math.Pow(rows[i].StandaloneEndEq/rows[i].StandaloneStartEq, 365.25/rows[i].CalendarDays) - 1.0) * 100.0
			}
			if rows[i].SharedStartEq > 0 && rows[i].SharedEndEq > 0 {
				rows[i].SharedCAGRPct = (math.Pow(rows[i].SharedEndEq/rows[i].SharedStartEq, 365.25/rows[i].CalendarDays) - 1.0) * 100.0
			}
		}
	}

	// 2. Print formatted terminal table
	printTerminalComparison(rows)

	// 3. Load daily curves for visual charting
	var sharedCurve []DailyPoint
	_ = db.Select(&sharedCurve, "SELECT date, total_equity, cash FROM equity_curve WHERE strategy_id = ? ORDER BY date ASC", combinedID)

	var standaloneCurve []DailyPoint
	_ = db.Select(&standaloneCurve, "SELECT date, total_equity, cash FROM standalone.equity_curve ORDER BY date ASC")

	// 4. Load preempted trades audit
	var preempted []PreemptedTrade
	_ = db.Select(&preempted, "SELECT id, symbol, entry_date, entry_price, exit_date, exit_price, hold_days, net_pnl, return_pct FROM trades WHERE exit_reason = 'PREEMPTED_BY_PRIMARY' ORDER BY exit_date DESC")

	// 5. Generate publication-grade HTML report
	if *htmlOut != "" {
		err := generateHTMLReport(*htmlOut, rows, sharedCurve, standaloneCurve, preempted, standaloneInitialCapital, sharedInitialCapital)
		if err != nil {
			log.Fatalf("Failed to generate HTML report: %v", err)
		}
		fmt.Printf("✨ Interactive HTML Report saved to: %s\n\n", *htmlOut)
	}
}

func printTerminalComparison(rows []AnnualComparisonRow) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{
		"Year", "Period Window", "VOO (BM)", "Standalone VOO-TECL", "2-Strategy Shared", "Return Spread", "Standalone End $", "Shared End $", "Idle Cash (Stand vs Shared)",
	})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for _, r := range rows {
		spreadSign := ""
		if r.SpreadVsStandalonePct > 0 {
			spreadSign = "+"
		}
		table.Append([]string{
			r.Year,
			r.Horizon,
			fmt.Sprintf("%+.2f%%", r.VOOReturnPct),
			fmt.Sprintf("%+.2f%%", r.StandaloneReturnPct),
			fmt.Sprintf("%+.2f%%", r.SharedReturnPct),
			fmt.Sprintf("%s%.2f%%", spreadSign, r.SpreadVsStandalonePct),
			fmt.Sprintf("$%.2f", r.StandaloneEndEq),
			fmt.Sprintf("$%.2f", r.SharedEndEq),
			fmt.Sprintf("%.1f%% vs %.1f%%", r.StandaloneIdleCashPct, r.SharedIdleCashPct),
		})
	}
	table.Render()
	fmt.Println()
}

func generateHTMLReport(
	outPath string,
	rows []AnnualComparisonRow,
	sharedCurve, standaloneCurve []DailyPoint,
	preempted []PreemptedTrade,
	standaloneInitialCapital, sharedInitialCapital float64,
) error {
	rowsJSON, _ := json.Marshal(rows)
	sharedCurveJSON, _ := json.Marshal(sharedCurve)
	standaloneCurveJSON, _ := json.Marshal(standaloneCurve)
	preemptedJSON, _ := json.Marshal(preempted)

	// Compute overall stats. Normalized by each run's own actual starting
	// capital (not a hardcoded $100,000) and the actual elapsed window (not
	// a hardcoded 5 years) — see initialCapitalOf for why this matters for a
	// fair standalone-vs-shared comparison.
	finalStand := 0.0
	finalShared := 0.0
	totalTradingDays := 0
	elapsedCalendarDays := 0.0
	if len(rows) > 0 {
		finalStand = rows[len(rows)-1].StandaloneEndEq
		finalShared = rows[len(rows)-1].SharedEndEq
		for _, r := range rows {
			totalTradingDays += r.TradingDays
			elapsedCalendarDays += r.CalendarDays
		}
	}
	elapsedYears := elapsedCalendarDays / 365.25
	if elapsedYears <= 0 {
		elapsedYears = 1
	}

	totalStandRet := (finalStand - standaloneInitialCapital) / standaloneInitialCapital * 100.0
	totalSharedRet := (finalShared - sharedInitialCapital) / sharedInitialCapital * 100.0
	standCAGR := (math.Pow(finalStand/standaloneInitialCapital, 1.0/elapsedYears) - 1.0) * 100.0
	sharedCAGR := (math.Pow(finalShared/sharedInitialCapital, 1.0/elapsedYears) - 1.0) * 100.0

	windowLabel := fmt.Sprintf("%.1f-Year", elapsedYears)

	content := htmlTemplate
	content = strings.ReplaceAll(content, "${FINAL_STANDALONE}", fmt.Sprintf("%.2f", finalStand))
	content = strings.ReplaceAll(content, "${TOTAL_RET_STAND}", fmt.Sprintf("%.1f", totalStandRet))
	content = strings.ReplaceAll(content, "${CAGR_STAND}", fmt.Sprintf("%.2f", standCAGR))
	content = strings.ReplaceAll(content, "${FINAL_SHARED}", fmt.Sprintf("%.2f", finalShared))
	content = strings.ReplaceAll(content, "${TOTAL_RET_SHARED}", fmt.Sprintf("%.1f", totalSharedRet))
	content = strings.ReplaceAll(content, "${CAGR_SHARED}", fmt.Sprintf("%.2f", sharedCAGR))
	content = strings.ReplaceAll(content, "${PREEMPTED_COUNT}", fmt.Sprintf("%d", len(preempted)))
	content = strings.ReplaceAll(content, "${WINDOW_LABEL}", windowLabel)
	content = strings.ReplaceAll(content, "${TOTAL_TRADING_DAYS}", fmt.Sprintf("%d", totalTradingDays))
	content = strings.ReplaceAll(content, "${ROWS_JSON}", string(rowsJSON))
	content = strings.ReplaceAll(content, "${SHARED_POINTS_JSON}", string(sharedCurveJSON))
	content = strings.ReplaceAll(content, "${STANDALONE_POINTS_JSON}", string(standaloneCurveJSON))
	content = strings.ReplaceAll(content, "${PREEMPT_DATA_JSON}", string(preemptedJSON))

	return os.WriteFile(outPath, []byte(content), 0644)
}
