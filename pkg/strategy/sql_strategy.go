package strategy

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// Placeholders substituted with the strategy's own StrategyConfig values in
// any pipeline SQL file, so a strategy's take-profit/stop-loss/hold-days
// (and, for two-leg combos, its short-leg equivalents) are real tunable
// parameters instead of hardcoded literals baked into the .sql file — the
// same problem __DECLINE_DAYS__ solves for the consecutive decline/rally-day
// window (e.g. sql/strategies/gld_decline, sig_voo_buy_tecl, voo_tecl_spxu_combo).
const (
	declineDaysPlaceholder         = "__DECLINE_DAYS__"
	takeProfitMultPlaceholder      = "__TAKE_PROFIT_MULT__"
	stopLossMultPlaceholder        = "__STOP_LOSS_MULT__"
	holdDaysPlaceholder            = "__HOLD_DAYS__"
	shortTakeProfitMultPlaceholder = "__SHORT_TAKE_PROFIT_MULT__"
	shortStopLossMultPlaceholder   = "__SHORT_STOP_LOSS_MULT__"
	shortHoldDaysPlaceholder       = "__SHORT_HOLD_DAYS__"
	// symbolPlaceholder is substituted from StrategyConfig.Benchmark, letting one
	// pipeline directory (e.g. sql/strategies/decision_tree_features) serve many
	// per-symbol strategy instances instead of needing one directory per ticker.
	symbolPlaceholder = "__SYMBOL__"
)

// takeProfitMultiplier converts a StrategyConfig take-profit *offset* (e.g.
// TakeProfitPct/ShortTakeProfitPct = 0.08 for +8%) into the multiplier a SQL
// pipeline should apply to an entry price, preserving the 0.0 "no per-signal
// override" sentinel the simulator checks for (pkg/simulator/portfolio.go:
// `if sig.TakeProfit > 0`, falling back to config-level TargetPct/
// TakeProfitPct) when a strategy hasn't set that side at all.
func takeProfitMultiplier(offsetPct float64) float64 {
	if offsetPct <= 0 {
		return 0.0
	}
	return 1.0 + offsetPct
}

// stopLossMultiplier passes a StrategyConfig stop-loss *multiplier* (e.g.
// StopLossPct/ShortStopLossPct = 0.98 for -2%, applied directly as
// entryPrice*multiplier — see StrategyConfig.StopLossPct's doc comment)
// straight through to SQL, preserving the same 0.0 "no per-signal override"
// sentinel as takeProfitMultiplier.
func stopLossMultiplier(multiplier float64) float64 {
	if multiplier <= 0 {
		return 0.0
	}
	return multiplier
}

// substitutePlaceholders replaces every strategy-config placeholder present
// in sqlText. Only __DECLINE_DAYS__ has a failure mode worth warning about —
// 0.0 is a legitimate "no override" value for every TP/SL/hold placeholder,
// but a decline/rally window of 0 days produces a broken WHERE clause.
func substitutePlaceholders(sqlText, id, fileName string, cfg StrategyConfig) string {
	if strings.Contains(sqlText, declineDaysPlaceholder) {
		if cfg.DeclineDays <= 0 {
			log.Printf("[sql_strategy %s] %s references %s but config.DeclineDays is unset — leaving query unsubstituted, it will fail", id, fileName, declineDaysPlaceholder)
		} else {
			sqlText = strings.ReplaceAll(sqlText, declineDaysPlaceholder, strconv.Itoa(cfg.DeclineDays))
		}
	}
	sqlText = strings.ReplaceAll(sqlText, takeProfitMultPlaceholder, strconv.FormatFloat(takeProfitMultiplier(cfg.TakeProfitPct), 'f', 6, 64))
	sqlText = strings.ReplaceAll(sqlText, stopLossMultPlaceholder, strconv.FormatFloat(stopLossMultiplier(cfg.StopLossPct), 'f', 6, 64))
	sqlText = strings.ReplaceAll(sqlText, holdDaysPlaceholder, strconv.Itoa(cfg.HoldingWindow))
	sqlText = strings.ReplaceAll(sqlText, shortTakeProfitMultPlaceholder, strconv.FormatFloat(takeProfitMultiplier(cfg.ShortTakeProfitPct), 'f', 6, 64))
	sqlText = strings.ReplaceAll(sqlText, shortStopLossMultPlaceholder, strconv.FormatFloat(stopLossMultiplier(cfg.ShortStopLossPct), 'f', 6, 64))
	sqlText = strings.ReplaceAll(sqlText, shortHoldDaysPlaceholder, strconv.Itoa(cfg.ShortHoldingWindow))
	if strings.Contains(sqlText, symbolPlaceholder) {
		if cfg.Benchmark == "" {
			log.Printf("[sql_strategy %s] %s references %s but config.Benchmark is unset — leaving query unsubstituted, it will fail", id, fileName, symbolPlaceholder)
		} else {
			sqlText = strings.ReplaceAll(sqlText, symbolPlaceholder, cfg.Benchmark)
		}
	}
	return sqlText
}

var sqlPipelineMu sync.Mutex

// SQLPipelineStrategy adapts any directory of sequential SQL scripts into a runnable Strategy.
type SQLPipelineStrategy struct {
	id           string
	name         string
	description  string
	pipelineDir  string
	marketDBPath string
	calcDBPath   string
	config       StrategyConfig
}

// NewSQLPipelineStrategy creates a new SQL-backed strategy from a directory of SQL scripts.
func NewSQLPipelineStrategy(id, name, description, pipelineDir string, config StrategyConfig) *SQLPipelineStrategy {
	config.ID = id
	config.Name = name
	config.Description = description

	s := &SQLPipelineStrategy{
		id:          id,
		name:        name,
		description: description,
		pipelineDir: pipelineDir,
		config:      config,
	}
	Register(s)
	return s
}

func (s *SQLPipelineStrategy) ID() string {
	return s.id
}

func (s *SQLPipelineStrategy) Name() string {
	return s.name
}

func (s *SQLPipelineStrategy) Description() string {
	return s.description
}

func (s *SQLPipelineStrategy) DefaultConfig() StrategyConfig {
	return s.config
}

// PipelineDir returns the directory of SQL scripts this pipeline executes,
// resolved however it was originally constructed (e.g. AutoRegisterSQLStrategies
// resolves it relative to a caller-supplied root, which differs between
// production binaries run from the repo root and tests run from a package
// directory). Lets other strategies that build their own one-off pipeline
// instance (to inject a per-instance config) reuse the already-correct path
// instead of hardcoding one that only works from the repo root.
func (s *SQLPipelineStrategy) PipelineDir() string {
	return s.pipelineDir
}

func (s *SQLPipelineStrategy) Validate() error {
	return ValidateConfig(s.config)
}

// SetDatabases injects the required database paths.
func (s *SQLPipelineStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

// RequiredSymbols inspects the pipeline SQL queries for explicit symbol requirements (e.g. symbol = 'XYZ').
func (s *SQLPipelineStrategy) RequiredSymbols() []string {
	if s.pipelineDir == "" {
		return nil
	}
	files, err := readPipelineDir(s.pipelineDir)
	if err != nil {
		return nil
	}

	symMap := make(map[string]bool)
	symRegex := regexp.MustCompile(`(?i)\bsymbol\s*=\s*'([A-Za-z0-9]+)'`)

	for _, f := range files {
		if !f.IsDir() && strings.HasSuffix(f.Name(), ".sql") {
			content, err := readPipelineFile(s.pipelineDir, f.Name())
			if err != nil {
				continue
			}
			matches := symRegex.FindAllStringSubmatch(string(content), -1)
			for _, m := range matches {
				if len(m) > 1 {
					sym := strings.ToUpper(strings.TrimSpace(m[1]))
					if sym != "" {
						symMap[sym] = true
					}
				}
			}
		}
	}

	var syms []string
	for sym := range symMap {
		syms = append(syms, sym)
	}
	sort.Strings(syms)
	return syms
}

// openAttachedCalcDB opens calcDBPath, attaches marketDBPath read-only as
// "market", and creates the temp view every pipeline script reads from.
// Shared by SQLPipelineStrategy.GenerateSignals and any other pipeline runner
// (e.g. decisiontree.go's SQL-backed feature extraction) that needs the same
// market-DB-as-source-of-truth setup without duplicating the DSN/attach
// boilerplate.
func openAttachedCalcDB(marketDBPath, calcDBPath string) (*sqlx.DB, error) {
	// The calc DB's directory may not exist yet (livescan writes to a fresh temp
	// directory on App Engine). Without it the ATTACH below fails with "unable to
	// open database file", the pipeline returns no signals, and a real entry signal
	// is reported as NO_SIGNAL.
	if dir := filepath.Dir(calcDBPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create calc DB dir %s: %w", dir, err)
		}
	}

	dsn := calcDBPath
	if !strings.Contains(dsn, "?") {
		dsn += "?_busy_timeout=15000&_journal_mode=WAL"
	} else {
		dsn += "&_busy_timeout=15000"
	}

	db, err := sqlx.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open calc DB %s: %w", calcDBPath, err)
	}

	attachQuery := fmt.Sprintf("ATTACH DATABASE '%s' AS market;", marketDBPath)
	if _, err := db.Exec(attachQuery); err != nil {
		db.Close()
		return nil, fmt.Errorf("attach market database %s: %w", marketDBPath, err)
	}

	if _, err := db.Exec("CREATE TEMP VIEW IF NOT EXISTS backtest_start AS SELECT rowid, * FROM market.backtest_start;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("create backtest_start view: %w", err)
	}

	return db, nil
}

// GenerateSignals executes the SQL pipeline scripts in order and extracts entry signals.
func (s *SQLPipelineStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	sqlPipelineMu.Lock()
	defer sqlPipelineMu.Unlock()

	if s.marketDBPath == "" || s.calcDBPath == "" {
		log.Printf("Warning: SQL strategy %s requires both marketDBPath and calcDBPath", s.id)
		return nil
	}

	db, err := openAttachedCalcDB(s.marketDBPath, s.calcDBPath)
	if err != nil {
		log.Printf("Warning: SQL strategy %s failed to open pipeline DB: %v", s.id, err)
		return nil
	}
	defer db.Close()

	// Execute pipeline scripts in lexical order
	files, err := readPipelineDir(s.pipelineDir)
	if err != nil {
		// Never fail silently: with no SQL run there are no slice tables, and a
		// real entry signal would be reported as NO_SIGNAL.
		log.Printf("[sql_strategy %s] ERROR: cannot read pipeline %s, no signals will be produced: %v", s.id, s.pipelineDir, err)
	} else {
		var sqlFiles []string
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".sql") {
				lower := strings.ToLower(f.Name())
				// Skip post-backtest reporting and audit queries that operate on equity_curve
				if strings.Contains(lower, "audit") || strings.Contains(lower, "annual") || strings.Contains(lower, "report") || strings.Contains(lower, "compare") {
					continue
				}
				sqlFiles = append(sqlFiles, f.Name())
			}
		}
		sort.Strings(sqlFiles)

		for _, sqlFile := range sqlFiles {
			content, err := readPipelineFile(s.pipelineDir, sqlFile)
			if err != nil {
				log.Printf("[sql_strategy %s] failed to read %s: %v", s.id, sqlFile, err)
				continue
			}
			sqlText := substitutePlaceholders(string(content), s.id, filepath.Base(sqlFile), s.config)
			queries := strings.Split(sqlText, ";")
			for _, q := range queries {
				trimmed := strings.TrimSpace(q)
				if trimmed == "" {
					continue
				}
				if _, err := db.Exec(trimmed); err != nil {
					log.Printf("[sql_strategy %s] SQL exec error in %s: %v (query: %.80s...)", s.id, filepath.Base(sqlFile), err, trimmed)
				}
			}
		}
	}

	// Extract generated entry signals from any available signal table
	var signals []models.Signal
	baseDir := filepath.Base(s.pipelineDir)
	cleanID := strings.TrimSuffix(strings.ReplaceAll(s.id, "-", "_"), "_sql")
	tableCandidates := []string{
		cleanID + "_signals",
		baseDir + "_signals",
		"rsi_oversold_signals",
		"entry",
		"signals",
	}

	for _, tbl := range tableCandidates {
		// First try extended select with optional direction, regime, and overrides
		extendedQuery := fmt.Sprintf(`
			SELECT coalesce(idx, rowid, 0) as idx, symbol, substr(date, 1, 10) as date,
			       open, high, low, close, volume, buylimit, entry,
			       coalesce(direction, 'LONG') as direction,
			       coalesce(regime, 'All Regimes') as regime,
			       coalesce(hold_days_override, 0) as hold_days_override,
			       coalesce(take_profit, 0.0) as take_profit,
			       coalesce(stop_loss, 0.0) as stop_loss
			FROM %s
			WHERE entry = 1
			ORDER BY date, symbol ASC;
		`, tbl)
		err = db.Select(&signals, extendedQuery)
		if err == nil && len(signals) > 0 {
			break
		}

		// Fallback to basic columns with safe coalesce on idx
		query := fmt.Sprintf(`
			SELECT coalesce(idx, rowid, 0) as idx, symbol, substr(date, 1, 10) as date,
			       open, high, low, close, volume, buylimit, entry
			FROM %s
			WHERE entry = 1
			ORDER BY date, symbol ASC;
		`, tbl)
		err = db.Select(&signals, query)
		if err == nil && len(signals) > 0 {
			break
		}
	}

	if len(signals) == 0 {
		// Fallback query if standard signal tables aren't found
		fallbackQuery := `
			SELECT b.idx, b.symbol, substr(b.date, 1, 10) as date, b.open, b.high, b.low, b.close, b.volume, b.close as buylimit, 1 as entry
			FROM backtest_start b
			INNER JOIN entry e ON b.symbol = e.symbol AND substr(b.date, 1, 10) = substr(e.date, 1, 10)
			ORDER BY b.date, b.symbol ASC;
		`
		_ = db.Select(&signals, fallbackQuery)
	}

	return signals
}

// hasGoStrategy reports whether a strategy defined in package strategy (its
// init-registered Go types, not another SQL pipeline) corresponds to a
// sql/strategies folder name, ignoring case, dashes and underscores.
func hasGoStrategy(dirName string) bool {
	registryLock.RLock()
	defer registryLock.RUnlock()
	want := normalizeKey(dirName)
	for _, s := range registry {
		if _, isSQL := s.(*SQLPipelineStrategy); isSQL {
			continue
		}
		if normalizeKey(s.ID()) == want {
			return true
		}
	}
	return false
}

var (
	autoRegisteredMu sync.Mutex
	autoRegistered   = map[string]bool{}
)

// AutoRegisterSQLStrategies registers the SQL pipeline behind each Go strategy
// defined in pkg/strategy. A sql/strategies folder with no matching Go
// strategy in this package (archived, failed_training, audit-only, or
// abandoned) is NOT registered: strategies come only from pkg/strategy, never
// from other folders or subfolders.
//
// It is idempotent: repeated calls with the same rootDir and default DB path
// (e.g. one per live scan in a long-running process) register once and then
// return immediately. Strategies added on disk after the first call are not
// picked up.
func AutoRegisterSQLStrategies(rootDir string, defaultDBPath ...string) {
	key := rootDir + "\x00" + strings.Join(defaultDBPath, "\x00")
	autoRegisteredMu.Lock()
	defer autoRegisteredMu.Unlock()
	if autoRegistered[key] {
		return
	}
	autoRegistered[key] = true

	stratDir := filepath.Join(rootDir, "sql", "strategies")
	entries, err := os.ReadDir(stratDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			dirName := entry.Name()
			if !hasGoStrategy(dirName) {
				continue
			}
			id := fmt.Sprintf("%s-sql", dirName)
			name := fmt.Sprintf("%s (SQL Pipeline)", strings.Title(strings.ReplaceAll(dirName, "_", " ")))
			desc := fmt.Sprintf("SQL pipeline executed from sql/strategies/%s", dirName)
			pipelinePath := filepath.Join(stratDir, dirName)

			cfg := StrategyConfig{
				ID:                 id,
				Name:               name,
				Description:        desc,
				TargetPct:          1.20,
				StopLossPct:        0.93,
				HoldingWindow:      10,
				PositionCap:        5,
				AllocationPct:      0.20,
				SlippagePct:        0.0005,
				CommissionPerShare: 0.0001,
			}

			// Specific default config tuning for recognized strategies
			switch dirName {
			case "donchian_breakout":
				cfg.TargetPct = 1.25
				cfg.StopLossPct = 0.92
				cfg.UseTrailingStop = true
				cfg.TrailingStopPct = 0.06
				cfg.HoldingWindow = 20
			case "bb_capitulation":
				cfg.TargetPct = 1.18
				cfg.StopLossPct = 0.93
				cfg.HoldingWindow = 10
			case "rsi2_trend":
				cfg.TargetPct = 1.10
				cfg.StopLossPct = 0.94
				cfg.HoldingWindow = 6
			case "macd_crossover":
				cfg.TargetPct = 1.15
				cfg.StopLossPct = 0.95
				cfg.HoldingWindow = 12
			case "gld_decline":
				cfg.Benchmark = "GLD"
				cfg.AllocationPct = 0.65
				cfg.TargetPct = 1.08
				cfg.TakeProfitPct = 0.08
				cfg.StopLossPct = 0.98
				cfg.HoldingWindow = 12
				cfg.PositionCap = 1
				cfg.CashYieldAnnual = 0.045
				cfg.DeclineDays = 2 // matches GLDDeclineStrategy's default DeclineDays
			case "sig_voo_buy_spxu":
				// Matches SigVooBuySpxu's own defaults.
				cfg.AllocationPct = 0.65
				cfg.TargetPct = 1.06
				cfg.TakeProfitPct = 0.06
				cfg.StopLossPct = 0.95
				cfg.HoldingWindow = 2
				cfg.PositionCap = 1
				cfg.CashYieldAnnual = 0.045
				cfg.DeclineDays = 3
			case "mara_tree":
				// Matches MARATreeStrategy's own defaults.
				cfg.AllocationPct = 0.65
				cfg.TargetPct = 1.05
				cfg.TakeProfitPct = 0.05
				cfg.StopLossPct = 0.92
				cfg.HoldingWindow = 1
				cfg.PositionCap = 1
				cfg.CashYieldAnnual = 0.045
			case "nvdl_tree":
				// Matches NVDLTreeStrategy's own defaults.
				cfg.AllocationPct = 0.65
				cfg.TargetPct = 1.15
				cfg.TakeProfitPct = 0.15
				cfg.StopLossPct = 0.93
				cfg.HoldingWindow = 5
				cfg.PositionCap = 1
				cfg.CashYieldAnnual = 0.045
			case "pdd_tree":
				// Matches PDDTreeStrategy's own defaults.
				cfg.AllocationPct = 0.65
				cfg.TargetPct = 1.05
				cfg.TakeProfitPct = 0.05
				cfg.StopLossPct = 0.94
				cfg.HoldingWindow = 3
				cfg.PositionCap = 1
				cfg.CashYieldAnnual = 0.045
			case "sig_voo_up1_buy_tqqq":
				// Matches SigVooUp1BuyTqqq's own defaults.
				cfg.AllocationPct = 0.65
				cfg.TargetPct = 1.08
				cfg.TakeProfitPct = 0.08
				cfg.StopLossPct = 0.90
				cfg.HoldingWindow = 10
				cfg.PositionCap = 1
				cfg.CashYieldAnnual = 0.045
			case "voo_up3":
				// Matches VOOUp3Strategy's own defaults.
				cfg.AllocationPct = 0.65
				cfg.TargetPct = 1.05
				cfg.TakeProfitPct = 0.05
				cfg.StopLossPct = 0.94
				cfg.HoldingWindow = 8
				cfg.PositionCap = 1
				cfg.CashYieldAnnual = 0.045
				cfg.DeclineDays = 3 // VOOUp3Strategy's default GainDays
			case "sig_qqq_up1_buy_sqqq", "sig_qqq_up1_buy_tqqq":
				// Matches SigQqqUp1BuySqqq's/SigQqqUp1BuyTqqq's own defaults.
				cfg.AllocationPct = 0.65
				cfg.TargetPct = 1.08
				cfg.TakeProfitPct = 0.08
				cfg.StopLossPct = 0.0
				cfg.HoldingWindow = 1
				cfg.PositionCap = 1
				cfg.CashYieldAnnual = 0.045
			case "sig_voo_buy_tecl", "voo_tecl_spxu_combo":
				// Matches SigVooBuyTecl's/VOOTECLSPXUCombo's own defaults.
				cfg.AllocationPct = 0.65
				cfg.TargetPct = 1.05
				cfg.TakeProfitPct = 0.05
				cfg.StopLossPct = 0.00
				cfg.HoldingWindow = 8
				cfg.PositionCap = 1
				cfg.CashYieldAnnual = 0.045
				cfg.DeclineDays = 3
				cfg.ShortTakeProfitPct = 0.06
				cfg.ShortStopLossPct = 0.95
				cfg.ShortHoldingWindow = 2
			}

			NewSQLPipelineStrategy(id, name, desc, pipelinePath, cfg)
		}
	}
}
