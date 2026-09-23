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
package strateval

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/appenv"
	"github.com/darianmavgo/backtestgosqlite/pkg/cliutils"
	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
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

// Config holds the settings of a strateval run; subcommands use the subset they need.
type Config struct {
	Db            string  // -db
	Allowlist     string  // -allowlist
	RunId         string  // -run-id
	MarketDb      string  // -market-db
	Table         string  // -table
	Strategy      string  // -strategy
	OutDir        string  // -out-dir
	Capital       float64 // -capital
	Start         string  // -start
	OosMonths     int     // -oos-months
	Optimize      bool    // -optimize
	MaxTrials     int     // -max-trials
	List          bool    // -list
	SyncDeployed  bool    // -sync-deployed
	MinOosTrades  int     // -min-oos-trades
	MinOosWinRate float64 // -min-oos-win-rate
	MaxOosDd      float64 // -max-oos-dd
	Subcommand    string  // "report", "status", "sync-deployed", "path", or empty for an evaluation run
}

// DefaultConfig returns the CLI defaults.
func DefaultConfig() Config {
	return Config{
		Db:            defaultLedgerDB(),
		Allowlist:     os.Getenv("STRATEGY_ALLOWLIST"),
		RunId:         "",
		MarketDb:      appenv.MarketDB(),
		Table:         "backtest_start",
		Strategy:      "",
		OutDir:        appenv.ReportFile("strateval_runs"),
		Capital:       100000,
		Start:         storage.DefaultStartDate,
		OosMonths:     12,
		Optimize:      false,
		MaxTrials:     50,
		List:          false,
		SyncDeployed:  false,
		MinOosTrades:  12,
		MinOosWinRate: 0.55,
		MaxOosDd:      0.15,
	}
}

// Main is the CLI entry point.
func Main() {
	log.SetFlags(0)
	conf := DefaultConfig()
	d := conf
	conf.Subcommand = cliutils.PopSubcommand(map[string]string{"report": "report", "status": "status", "browse": "status", "sync-deployed": "sync-deployed", "path": "path"})
	flag.StringVar(&conf.Db, "db", d.Db, "strategies SQLite ledger")
	flag.StringVar(&conf.Allowlist, "allowlist", d.Allowlist, "current STRATEGY_ALLOWLIST CSV")
	flag.StringVar(&conf.RunId, "run-id", d.RunId, "optional run id filter")
	flag.StringVar(&conf.MarketDb, "market-db", d.MarketDb, "market bars SQLite")
	flag.StringVar(&conf.Table, "table", d.Table, "bars table")
	flag.StringVar(&conf.Strategy, "strategy", d.Strategy, "strategy id, comma-list, or 'all'")
	flag.StringVar(&conf.OutDir, "out-dir", d.OutDir, "per-run artifact dir")
	flag.Float64Var(&conf.Capital, "capital", d.Capital, "starting capital")
	flag.StringVar(&conf.Start, "start", d.Start, "earliest bar date")
	flag.IntVar(&conf.OosMonths, "oos-months", d.OosMonths, "held-out OOS months")
	flag.BoolVar(&conf.Optimize, "optimize", d.Optimize, "coarse IS-only param sweep before OOS")
	flag.IntVar(&conf.MaxTrials, "max-trials", d.MaxTrials, "max optimize trials per strategy")
	flag.BoolVar(&conf.List, "list", d.List, "list registered strategies and exit")
	flag.BoolVar(&conf.SyncDeployed, "sync-deployed", d.SyncDeployed, "also snapshot allowlist into deployments table after eval")
	flag.IntVar(&conf.MinOosTrades, "min-oos-trades", d.MinOosTrades, "Tier-A min OOS trades (stack books: monthly ≈ 12)")
	flag.Float64Var(&conf.MinOosWinRate, "min-oos-win-rate", d.MinOosWinRate, "tier-A min OOS win rate (0-1)")
	flag.Float64Var(&conf.MaxOosDd, "max-oos-dd", d.MaxOosDd, "tier-A max OOS drawdown (0-1, e.g. 0.15=15%)")
	flag.Parse()
	if err := Run(conf); err != nil {
		log.Fatal(err)
	}
}

// Run executes the subcommand (or evaluation) selected by conf. It returns
// errors instead of exiting.
func Run(conf Config) error {
	switch conf.Subcommand {
	case "report":
		return runReport(conf)
	case "status":
		return runStatus(conf)
	case "sync-deployed":
		return runSyncDeployed(conf)
	case "path":
		return runPath(conf)
	}
	return runEval(conf)
}

func runPath(conf Config) error {
	fmt.Print(BrowserHints(conf.Db))
	return nil
}

func runStatus(conf Config) error {
	st, err := OpenStore(conf.Db)
	if err != nil {
		return fmt.Errorf("open store: %v", err)
	}
	defer st.Close()
	rows, err := st.ListStatus()
	if err != nil {
		return fmt.Errorf("status: %v", err)
	}
	fmt.Printf("ledger: %s\n\n", conf.Db)
	fmt.Print(FormatStatusTable(rows))
	fmt.Printf("\nViews: v_strategy_status, v_latest_scores, v_deployed, v_promote_candidates, v_demote_candidates\n")
	return nil
}

func runSyncDeployed(conf Config) error {
	st, err := OpenStore(conf.Db)
	if err != nil {
		return fmt.Errorf("open store: %v", err)
	}
	defer st.Close()
	live, retired, err := st.SyncAllowlist(splitCSV(conf.Allowlist), "sync-allowlist")
	if err != nil {
		return fmt.Errorf("sync: %v", err)
	}
	fmt.Printf("synced allowlist into %s (live_recorded=%d retired_recorded=%d)\n", conf.Db, live, retired)
	fmt.Println("(did not modify any .env — ledger only)")
	rows, err := st.ListStatus()
	if err == nil {
		fmt.Print("\n")
		fmt.Print(FormatStatusTable(rows))
	}
	return nil
}

func runReport(conf Config) error {

	st, err := OpenStore(conf.Db)
	if err != nil {
		return fmt.Errorf("open store: %v", err)
	}
	defer st.Close()
	rows, err := st.LatestByStrategy(conf.RunId)
	if err != nil {
		return fmt.Errorf("query: %v", err)
	}
	fmt.Print(FormatReport(rows, splitCSV(conf.Allowlist)))
	return nil
}

func runEval(conf Config) error {
	if conf.RunId == "" {
		conf.RunId = time.Now().UTC().Format("20060102T150405Z")
	}

	strategy.AutoRegisterSQLStrategies(appenv.Folder(), conf.MarketDb)

	if conf.List {
		for _, s := range strategy.List() {
			fmt.Println(s.ID())
		}
		return nil
	}
	if strings.TrimSpace(conf.Strategy) == "" {
		return fmt.Errorf("-strategy is required (id, csv, or all). Or: strateval status | report | sync-deployed | path")
	}

	strats, err := resolveStrategies(conf.Strategy)
	if err != nil {
		return err
	}
	db, err := storage.OpenSQLite(conf.MarketDb)
	if err != nil {
		return fmt.Errorf("open market db: %v", err)
	}
	defer db.Close()

	st, err := OpenStore(conf.Db)
	if err != nil {
		return fmt.Errorf("open ledger: %v", err)
	}
	defer st.Close()

	alMap := ParseAllowlistCSV(conf.Allowlist)
	fmt.Printf("strateval run_id=%s strategies=%d oos_months=%d optimize=%v\n",
		conf.RunId, len(strats), conf.OosMonths, conf.Optimize)
	fmt.Printf("ledger: %s\n", conf.Db)
	fmt.Println("(does not modify STRATEGY_ALLOWLIST or live jobs)")

	var rows []EvalRow
	for _, s := range strats {
		fmt.Printf("… %s\n", s.ID())
		res, err := EvaluateStrategy(db, s, EvalOptions{
			MarketDB: conf.MarketDb, Table: conf.Table, OutDir: conf.OutDir,
			Capital: conf.Capital, StartDate: conf.Start, OOSMonths: conf.OosMonths,
			Optimize: conf.Optimize, MaxTrials: conf.MaxTrials, Allowlist: alMap, RunID: conf.RunId,
			Gates: Gates{MinOOSTrades: conf.MinOosTrades, MinOOSWinRate: conf.MinOosWinRate, MaxOOSDDAbs: conf.MaxOosDd},
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

	if conf.SyncDeployed {
		live, retired, err := st.SyncAllowlist(splitCSV(conf.Allowlist), "sync-allowlist")
		if err != nil {
			log.Printf("sync-deployed: %v", err)
		} else {
			fmt.Printf("deployments synced (live_recorded=%d retired_recorded=%d)\n", live, retired)
		}
	}

	fmt.Print("\n")
	fmt.Print(FormatReport(rows, splitCSV(conf.Allowlist)))
	fmt.Printf("\nBrowse: open %s — or `strateval status` / `strateval path`\n", conf.Db)
	return nil
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
