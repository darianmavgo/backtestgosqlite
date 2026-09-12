package strategy

import (
	"database/sql"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

var GeneticMomentumSymbols = []string{
	"TSLA", "NVDA", "AMD", "AAPL", "MSFT", "META", "GOOGL", "AMZN", "NFLX", "PLTR",
	"COIN", "MSTR", "UPST", "ENPH", "SEDG", "ROKU", "DKNG", "PYPL", "SOFI", "HOOD",
	"SNOW", "CRWD", "ZS", "DDOG", "NET", "RBLX", "U", "AI", "TSM", "ASML",
	"INTC", "MU", "AVGO", "MARA", "RIOT", "AFRM", "PTON", "LCID", "RIVN", "NIO",
	"XPEV", "SHOP", "MELI", "SE", "BABA", "JD", "PDD", "TTD", "DOCU", "ZM",
}

type GeneticMomentumStrategy struct {
	marketDBPath string
	calcDBPath   string
}

func init() {
	Register(&GeneticMomentumStrategy{})
}

func (s *GeneticMomentumStrategy) ID() string {
	return "genetic-momentum"
}

func (s *GeneticMomentumStrategy) Name() string {
	return "DEAP Genetic Momentum (Weekly Top-Pick)"
}

func (s *GeneticMomentumStrategy) Description() string {
	return "Scans 50 high-beta stocks weekly using Python DEAP genetic algorithm to predict the stock with the highest momentum probability. Enters top pick and exits at end of week or after +25% gains."
}

func (s *GeneticMomentumStrategy) RequiredSymbols() []string {
	return GeneticMomentumSymbols
}

func (s *GeneticMomentumStrategy) DefaultConfig() StrategyConfig {
	return StrategyConfig{
		ID:                 s.ID(),
		Name:               s.Name(),
		Description:        s.Description(),
		TargetPct:          1.25,   // Exit after 25% gains (+25% profit target)
		StopLossPct:        0.0,    // No fixed stop-loss; exits at end of week or after 25% gains
		HoldingWindow:      4,      // Default holding window (end of standard 5-day trading week)
		PositionCap:        1,      // Create a position in the highest probability stock only
		AllocationPct:      0.10,   // Allocate 10% of portfolio equity per week
		SlippagePct:        0.0005, // 0.05% slippage
		CommissionPerShare: 0.0001,
	}
}

func (s *GeneticMomentumStrategy) Validate() error {
	return ValidateConfig(s.DefaultConfig())
}

func (s *GeneticMomentumStrategy) SetDatabases(marketDBPath, calcDBPath string) {
	s.marketDBPath = marketDBPath
	s.calcDBPath = calcDBPath
}

func (s *GeneticMomentumStrategy) GenerateSignals(barsBySymbol map[string][]models.Bar) []models.Signal {
	if s.marketDBPath == "" || s.calcDBPath == "" {
		log.Printf("Warning: %s requires SetDatabases to be called", s.ID())
		return nil
	}

	// 1. Check if calcDBPath already has completed weekly predictions
	db, err := sql.Open("sqlite3", s.calcDBPath)
	needRun := true
	if err == nil {
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM predictions WHERE probability IS NOT NULL AND probability > 0").Scan(&count)
		if err == nil && count > 0 {
			needRun = false
		}
		db.Close()
	}

	if needRun {
		symString := strings.Join(s.RequiredSymbols(), ",")
		fmt.Printf("\n🧠 %s: Launching DEAP weekly momentum scan across all %d stocks...\n", s.ID(), len(s.RequiredSymbols()))
		cmd := exec.Command("python/.venv/bin/python3", "python/deap_optimizer.py",
			"--market-db", s.marketDBPath,
			"--calc-db", s.calcDBPath,
			"--symbols", symString,
			"--mode", "weekly",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Error executing DEAP optimizer: %v\nOutput: %s", err, string(out))
			return nil
		}
		fmt.Println(strings.TrimSpace(string(out)))
	}

	// 2. Read weekly predictions from calcDBPath
	db, err = sql.Open("sqlite3", s.calcDBPath)
	if err != nil {
		log.Printf("Failed to open strategy DB %s: %v", s.calcDBPath, err)
		return nil
	}
	defer db.Close()

	rows, err := db.Query("SELECT date, end_date, trading_days, predicted_symbol, probability, momentum_score FROM predictions ORDER BY date ASC")
	if err != nil {
		log.Printf("Failed to query predictions from %s: %v", s.calcDBPath, err)
		return nil
	}
	defer rows.Close()

	var signals []models.Signal
	for rows.Next() {
		var (
			date            string
			endDate         string
			tradingDays     int
			predictedSymbol string
			prob            float64
			score           float64
		)
		if err := rows.Scan(&date, &endDate, &tradingDays, &predictedSymbol, &prob, &score); err != nil {
			continue
		}

		// Find entry bar for predictedSymbol on date
		bars := barsBySymbol[predictedSymbol]
		var entryPrice float64
		for _, b := range bars {
			if b.Date == date {
				entryPrice = b.Close
				break
			}
		}

		if entryPrice <= 0 {
			continue
		}

		// Holding window to exit at end of week:
		// If a week has N trading days, holding from day 1 close to day N close is (N - 1) days.
		holdDays := tradingDays - 1
		if holdDays < 1 {
			holdDays = 1
		}

		signals = append(signals, models.Signal{
			Date:             date,
			Symbol:           predictedSymbol,
			Entry:            1,
			OrderType:        "market",
			BuyLimit:         entryPrice,
			Close:            entryPrice,
			TakeProfit:       entryPrice * 1.25, // Exit after 25% gains
			HoldDaysOverride: holdDays,           // Exit at end of week
			Metadata: map[string]float64{
				"probability": prob,
				"score":       score,
			},
		})
	}

	fmt.Printf("✅ %s: Generated %d weekly signals (top probability pick per week).\n", s.ID(), len(signals))
	return signals
}
