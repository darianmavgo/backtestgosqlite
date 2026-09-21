// Command strateval: additive IS/OOS evaluation loop.
// Does not modify STRATEGY_ALLOWLIST, evening-scan, or other live jobs.
//
//	strateval -list
//	strateval -strategy sig_voo_buy_tecl
//	strateval -strategy all -optimize -allowlist "$STRATEGY_ALLOWLIST"
//	strateval report -db reports/strategy_evals.db -allowlist "$STRATEGY_ALLOWLIST"
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
	"github.com/darianmavgo/backtestgosqlite/pkg/strateval"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
	_ "modernc.org/sqlite"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) > 1 && os.Args[1] == "report" {
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
		runReport()
		return
	}
	runEval()
}

func runReport() {
	dbPath := flag.String("db", appenv.ReportFile("strategy_evals.db"), "strategy_evals SQLite path")
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
	marketDB := flag.String("db", appenv.MarketDB(), "market bars SQLite")
	table := flag.String("table", "backtest_start", "bars table")
	stratArg := flag.String("strategy", "", "strategy id, comma-list, or 'all'")
	outDir := flag.String("out-dir", appenv.ReportFile("strateval_runs"), "per-run artifact dir")
	evalDB := flag.String("eval-db", appenv.ReportFile("strategy_evals.db"), "strategy_evals SQLite")
	capital := flag.Float64("capital", 100000, "starting capital")
	start := flag.String("start", storage.DefaultStartDate, "earliest bar date")
	oosMonths := flag.Int("oos-months", 12, "held-out OOS months")
	optimize := flag.Bool("optimize", false, "coarse IS-only param sweep before OOS")
	maxTrials := flag.Int("max-trials", 50, "max optimize trials per strategy")
	allowlist := flag.String("allowlist", os.Getenv("STRATEGY_ALLOWLIST"), "current allowlist CSV")
	runID := flag.String("run-id", time.Now().UTC().Format("20060102T150405Z"), "batch run id")
	list := flag.Bool("list", false, "list registered strategies and exit")
	flag.Parse()

	strategy.AutoRegisterSQLStrategies(appenv.Folder(), *marketDB)

	if *list {
		for _, s := range strategy.List() {
			fmt.Println(s.ID())
		}
		return
	}
	if strings.TrimSpace(*stratArg) == "" {
		log.Fatal("-strategy is required (id, csv, or all)")
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
		log.Fatalf("open eval store: %v", err)
	}
	defer st.Close()

	alMap := strateval.ParseAllowlistCSV(*allowlist)
	fmt.Printf("strateval run_id=%s strategies=%d oos_months=%d optimize=%v\n",
		*runID, len(strats), *oosMonths, *optimize)
	fmt.Println("(does not modify STRATEGY_ALLOWLIST or live jobs)")

	var rows []strateval.EvalRow
	for _, s := range strats {
		fmt.Printf("… %s\n", s.ID())
		res, err := strateval.EvaluateStrategy(db, s, strateval.EvalOptions{
			MarketDB: *marketDB, Table: *table, OutDir: *outDir,
			Capital: *capital, StartDate: *start, OOSMonths: *oosMonths,
			Optimize: *optimize, MaxTrials: *maxTrials, Allowlist: alMap, RunID: *runID,
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
	fmt.Print("\n")
	fmt.Print(strateval.FormatReport(rows, splitCSV(*allowlist)))
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
