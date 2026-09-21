// Command strateval: additive IS/OOS evaluation + SQLite lifecycle ledger.
// Does not modify STRATEGY_ALLOWLIST, evening-scan, or other live jobs.
//
//	strateval -list
//	strateval -strategy sig_voo_buy_tecl
//	strateval -strategy all -optimize -allowlist "$STRATEGY_ALLOWLIST"
//	strateval report
//	strateval status
//	strateval sync-deployed -allowlist "$STRATEGY_ALLOWLIST"
//	strateval path
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strateval"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	_ "modernc.org/sqlite"
)

func defaultLedgerDB() string {
	// Do NOT use appenv.ReportFile here: sourcing trade_orchestrator's .env
	// often sets APP_FOLDER=/mnt/data (Linux deploy path), which breaks on a Mac.
	if v := strings.TrimSpace(os.Getenv("STRATEGIES_DB")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("STRATEVAL_DB")); v != "" {
		return v
	}
	// Prefer <repo>/reports/strategies.db when run from the module tree.
	if root := findModuleRoot(); root != "" {
		return filepath.Join(root, "reports", "strategies.db")
	}
	return filepath.Join("reports", "strategies.db")
}

func findModuleRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func main() {
	log.SetFlags(0)
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "report":
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
			runReport()
			return
		case "status", "browse":
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
			runStatus()
			return
		case "sync-deployed":
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
			runSyncDeployed()
			return
		case "path":
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
			runPath()
			return
		}
	}
	runEval()
}

func runPath() {
	dbPath := flag.String("db", defaultLedgerDB(), "strategies SQLite ledger")
	flag.Parse()
	fmt.Print(strateval.BrowserHints(*dbPath))
}

func runStatus() {
	dbPath := flag.String("db", defaultLedgerDB(), "strategies SQLite ledger")
	flag.Parse()
	st, err := strateval.OpenStore(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()
	rows, err := st.ListStatus()
	if err != nil {
		log.Fatalf("status: %v", err)
	}
	fmt.Printf("ledger: %s\n\n", *dbPath)
	fmt.Print(strateval.FormatStatusTable(rows))
	fmt.Printf("\nViews: v_strategy_status, v_latest_scores, v_deployed, v_promote_candidates, v_demote_candidates\n")
}

func runSyncDeployed() {
	dbPath := flag.String("db", defaultLedgerDB(), "strategies SQLite ledger")
	allowlist := flag.String("allowlist", os.Getenv("STRATEGY_ALLOWLIST"), "current STRATEGY_ALLOWLIST CSV")
	flag.Parse()
	st, err := strateval.OpenStore(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()
	live, retired, err := st.SyncAllowlist(splitCSV(*allowlist), "sync-allowlist")
	if err != nil {
		log.Fatalf("sync: %v", err)
	}
	fmt.Printf("synced allowlist into %s (live_recorded=%d retired_recorded=%d)\n", *dbPath, live, retired)
	fmt.Println("(did not modify any .env — ledger only)")
	rows, err := st.ListStatus()
	if err == nil {
		fmt.Print("\n")
		fmt.Print(strateval.FormatStatusTable(rows))
	}
}

func runReport() {
	dbPath := flag.String("db", defaultLedgerDB(), "strategies SQLite ledger")
	runID := flag.String("run-id", "", "optional run id filter")
	allowlist := flag.String("allowlist", os.Getenv("STRATEGY_ALLOWLIST"), "current allowlist CSV (diff only)")
	flag.Parse()

	st, err := strateval.OpenStore(*dbPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()
	rows, err := st.LatestByStrategy(*runID)
	if err != nil {
		log.Fatalf("query: %v", err)
	}
	fmt.Print(strateval.FormatReport(rows, splitCSV(*allowlist)))
}

func runEval() {
	marketDB := flag.String("market-db", appenv.MarketDB(), "market bars SQLite")
	table := flag.String("table", "backtest_start", "bars table")
	stratArg := flag.String("strategy", "", "strategy id, comma-list, or 'all'")
	outDir := flag.String("out-dir", appenv.ReportFile("strateval_runs"), "per-run artifact dir")
	evalDB := flag.String("db", defaultLedgerDB(), "strategies SQLite ledger")
	capital := flag.Float64("capital", 100000, "starting capital")
	start := flag.String("start", storage.DefaultStartDate, "earliest bar date")
	oosMonths := flag.Int("oos-months", 12, "held-out OOS months")
	optimize := flag.Bool("optimize", false, "coarse IS-only param sweep before OOS")
	maxTrials := flag.Int("max-trials", 50, "max optimize trials per strategy")
	allowlist := flag.String("allowlist", os.Getenv("STRATEGY_ALLOWLIST"), "current allowlist CSV")
	runID := flag.String("run-id", time.Now().UTC().Format("20060102T150405Z"), "batch run id")
	list := flag.Bool("list", false, "list registered strategies and exit")
	syncDeployed := flag.Bool("sync-deployed", false, "also snapshot allowlist into deployments table after eval")
	minTrades := flag.Int("min-oos-trades", 12, "Tier-A min OOS trades (stack books: monthly ≈ 12)")
	minWin := flag.Float64("min-oos-win-rate", 0.55, "tier-A min OOS win rate (0-1)")
	maxDD := flag.Float64("max-oos-dd", 0.15, "tier-A max OOS drawdown (0-1, e.g. 0.15=15%)")
	flag.Parse()

	strategy.AutoRegisterSQLStrategies(appenv.Folder(), *marketDB)

	if *list {
		for _, s := range strategy.List() {
			fmt.Println(s.ID())
		}
		return
	}
	if strings.TrimSpace(*stratArg) == "" {
		log.Fatal("-strategy is required (id, csv, or all). Or: strateval status | report | sync-deployed | path")
	}

	strats, err := resolveStrategies(*stratArg)
	if err != nil {
		log.Fatal(err)
	}
	db, err := storage.OpenSQLite(*marketDB)
	if err != nil {
		log.Fatalf("open market db: %v", err)
	}
	defer db.Close()

	st, err := strateval.OpenStore(*evalDB)
	if err != nil {
		log.Fatalf("open ledger: %v", err)
	}
	defer st.Close()

	alMap := strateval.ParseAllowlistCSV(*allowlist)
	fmt.Printf("strateval run_id=%s strategies=%d oos_months=%d optimize=%v\n",
		*runID, len(strats), *oosMonths, *optimize)
	fmt.Printf("ledger: %s\n", *evalDB)
	fmt.Println("(does not modify STRATEGY_ALLOWLIST or live jobs)")

	var rows []strateval.EvalRow
	for _, s := range strats {
		fmt.Printf("… %s\n", s.ID())
		res, err := strateval.EvaluateStrategy(db, s, strateval.EvalOptions{
			MarketDB: *marketDB, Table: *table, OutDir: *outDir,
			Capital: *capital, StartDate: *start, OOSMonths: *oosMonths,
			Optimize: *optimize, MaxTrials: *maxTrials, Allowlist: alMap, RunID: *runID,
			Gates: strateval.Gates{MinOOSTrades: *minTrades, MinOOSWinRate: *minWin, MaxOOSDDAbs: *maxDD},
		})
		if err != nil {
			log.Printf("FAIL %s: %v", s.ID(), err)
			continue
		}
		if err := st.Insert(res.Row); err != nil {
			log.Printf("persist %s: %v", s.ID(), err)
		}
		rows = append(rows, res.Row)
		fmt.Printf("  tier=%s oos_sharpe=%.2f oos_trades=%d oos_cagr=%.1f%%\n",
			res.Row.Tier, res.Row.OOS.Sharpe, res.Row.OOS.Trades, res.Row.OOS.CAGR*100)
	}

	if *syncDeployed {
		live, retired, err := st.SyncAllowlist(splitCSV(*allowlist), "sync-allowlist")
		if err != nil {
			log.Printf("sync-deployed: %v", err)
		} else {
			fmt.Printf("deployments synced (live_recorded=%d retired_recorded=%d)\n", live, retired)
		}
	}

	fmt.Print("\n")
	fmt.Print(strateval.FormatReport(rows, splitCSV(*allowlist)))
	fmt.Printf("\nBrowse: open %s — or `strateval status` / `strateval path`\n", *evalDB)
}

func resolveStrategies(arg string) ([]strategy.Strategy, error) {
	arg = strings.TrimSpace(arg)
	if strings.EqualFold(arg, "all") {
		all := strategy.List()
		if len(all) == 0 {
			return nil, fmt.Errorf("no strategies registered")
		}
		return all, nil
	}
	var out []strategy.Strategy
	for _, id := range strings.Split(arg, ",") {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		s, ok := strategy.Get(id)
		if !ok {
			return nil, fmt.Errorf("strategy %q not found (try -list)", id)
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no strategies selected")
	}
	return out, nil
}

func splitCSV(raw string) []string {
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
