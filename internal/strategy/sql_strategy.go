package strategy

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/darianmavgo/backtestgosqlite/internal/models"
)

// SQLPipelineStrategy adapts any directory of sequential SQL scripts into a runnable Strategy.
type SQLPipelineStrategy struct {
	id          string
	name        string
	description string
	pipelineDir string
	dbPath      string
	config      StrategyConfig
}

// NewSQLPipelineStrategy creates a new SQL-backed strategy from a directory of SQL scripts.
func NewSQLPipelineStrategy(id, name, description, pipelineDir, dbPath string, config StrategyConfig) *SQLPipelineStrategy {
	config.ID = id
	config.Name = name
	config.Description = description

	s := &SQLPipelineStrategy{
		id:          id,
		name:        name,
		description: description,
		pipelineDir: pipelineDir,
		dbPath:      dbPath,
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

func (s *SQLPipelineStrategy) Validate() error {
	return ValidateConfig(s.config)
}

// SetDBPath sets the target database for executing the SQL pipeline.
func (s *SQLPipelineStrategy) SetDBPath(dbPath string) {
	s.dbPath = dbPath
}

// GenerateSignals executes the SQL pipeline scripts in order and extracts entry signals.
func (s *SQLPipelineStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	targetDb := s.dbPath
	if targetDb == "" {
		targetDb = "data/wc_master_backtest.db"
	}

	db, err := sqlx.Open("sqlite3", targetDb)
	if err != nil {
		log.Printf("Warning: SQL strategy %s failed to open DB %s: %v", s.id, targetDb, err)
		return nil
	}
	defer db.Close()

	// Execute pipeline scripts in lexical order
	files, err := os.ReadDir(s.pipelineDir)
	if err == nil {
		var sqlFiles []string
		for _, f := range files {
			if !f.IsDir() && strings.HasSuffix(f.Name(), ".sql") {
				sqlFiles = append(sqlFiles, filepath.Join(s.pipelineDir, f.Name()))
			}
		}
		sort.Strings(sqlFiles)

		for _, sqlFile := range sqlFiles {
			content, err := os.ReadFile(sqlFile)
			if err != nil {
				log.Printf("[sql_strategy %s] failed to read %s: %v", s.id, sqlFile, err)
				continue
			}
			queries := strings.Split(string(content), ";")
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
		"wc_backtest_details",
		"wc_buy_signal_slice",
		"entry",
		"signals",
	}

	for _, tbl := range tableCandidates {
		query := fmt.Sprintf(`
			SELECT idx, symbol, substr(date, 1, 10) as date, open, high, low, close, volume, buylimit, entry
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

// AutoRegisterSQLStrategies scans the sql/strategies directory and registers any SQL pipeline folders.
func AutoRegisterSQLStrategies(rootDir string, defaultDBPath ...string) {
	dbPath := "data/sample_stocks.db"
	if len(defaultDBPath) > 0 && defaultDBPath[0] != "" {
		dbPath = defaultDBPath[0]
	}

	stratDir := filepath.Join(rootDir, "sql", "strategies")
	entries, err := os.ReadDir(stratDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			dirName := entry.Name()
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
			case "millwharf":
				cfg.TargetPct = 1.15
				cfg.StopLossPct = 0.93
				cfg.HoldingWindow = 10
			}

			NewSQLPipelineStrategy(id, name, desc, pipelinePath, dbPath, cfg)
		}
	}
}
