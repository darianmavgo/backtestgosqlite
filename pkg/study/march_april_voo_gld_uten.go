package study

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

type VooGldUtenStudy struct {
	id            string
	name          string
	description   string
	marketDBPath  string
	resultsDBPath string
}

func init() {
	Register(&VooGldUtenStudy{
		id:          "march_april_voo_gld_uten",
		name:        "VOO, GLD, UTEN 1m Analysis (Mar/Apr 2025)",
		description: "Pivots 1-minute data for VOO, GLD, UTEN into a prototyping table and triggers Python for Granger Causality and Volatility Spillover.",
	})
}

func (s *VooGldUtenStudy) ID() string {
	return s.id
}

func (s *VooGldUtenStudy) Name() string {
	return s.name
}

func (s *VooGldUtenStudy) Description() string {
	return s.description
}

func (s *VooGldUtenStudy) SetDatabases(marketDBPath, resultsDBPath string) {
	s.marketDBPath = marketDBPath
	s.resultsDBPath = resultsDBPath
}

func (s *VooGldUtenStudy) Run() error {
	log.Printf("Running study: %s", s.name)
	
	if err := os.MkdirAll(filepath.Dir(s.resultsDBPath), 0755); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	db, err := sqlx.Open("sqlite3", s.resultsDBPath)
	if err != nil {
		return fmt.Errorf("failed to open results db: %w", err)
	}
	defer db.Close()

	attachQuery := fmt.Sprintf("ATTACH DATABASE '%s' AS market;", s.marketDBPath)
	if _, err := db.Exec(attachQuery); err != nil {
		return fmt.Errorf("failed to attach market database: %w", err)
	}

	log.Println("Pivoting data into prototyping sandbox table...")
	
	// Create prototyping table by aligning minute data for VOO, GLD, UTEN
	// We assume we want data strictly in March and April 2025
	pivotSQL := `
		DROP TABLE IF EXISTS prototyping;
		CREATE TABLE prototyping AS
		WITH 
			voo AS (
				SELECT date, close, ((close - open) / open) as return 
				FROM market.backtest_start 
				WHERE symbol = 'VOO' AND timeframe = '1m' AND date >= '2025-03-01' AND date < '2025-05-01'
			),
			gld AS (
				SELECT date, close, ((close - open) / open) as return 
				FROM market.backtest_start 
				WHERE symbol = 'GLD' AND timeframe = '1m' AND date >= '2025-03-01' AND date < '2025-05-01'
			),
			uten AS (
				SELECT date, close, ((close - open) / open) as return 
				FROM market.backtest_start 
				WHERE symbol = 'UTEN' AND timeframe = '1m' AND date >= '2025-03-01' AND date < '2025-05-01'
			)
		SELECT 
			voo.date as date,
			voo.close as voo_close,
			voo.return as voo_return,
			gld.close as gld_close,
			gld.return as gld_return,
			uten.close as uten_close,
			uten.return as uten_return
		FROM voo
		INNER JOIN gld ON voo.date = gld.date
		INNER JOIN uten ON voo.date = uten.date
		ORDER BY voo.date ASC;
	`
	if _, err := db.Exec(pivotSQL); err != nil {
		return fmt.Errorf("failed to create prototyping table: %w", err)
	}
	
	var count int
	_ = db.Get(&count, "SELECT COUNT(*) FROM prototyping;")
	log.Printf("Successfully created prototyping sandbox with %d aligned 1-minute rows.", count)

	// Close DB so Python script can open it safely
	db.Close()

	// Execute Python script
	scriptPath := filepath.Join("scripts", "studies", "voo_gld_uten_stats.py")
	log.Printf("Triggering Python script: %s %s", scriptPath, s.resultsDBPath)
	
	pythonBin := "python3"
	if _, err := os.Stat(".venv/bin/python3"); err == nil {
		pythonBin = ".venv/bin/python3"
	}

	cmd := exec.Command(pythonBin, scriptPath, s.resultsDBPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("python script failed: %w", err)
	}

	log.Println("Study execution complete. Results saved to:", s.resultsDBPath)
	return nil
}
