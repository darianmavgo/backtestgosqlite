package main

import (
	"fmt"
	"log"
	"math"
	"os"

	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/darianmavgo/backtestgosqlite/internal/storage"
	"github.com/darianmavgo/backtestgosqlite/internal/strategy"
	"github.com/olekukonko/tablewriter"
)

func main() {
	db, err := storage.OpenSQLite("data/live_scan.db")
	if err != nil {
		log.Fatalf("Failed to open DB: %v", err)
	}
	defer db.Close()

	barsBySymbol, _, err := storage.FetchAllBarsChronological(db)
	if err != nil {
		log.Fatalf("Failed to fetch bars: %v", err)
	}

	dfenBars, ok := barsBySymbol["DFEN"]
	if !ok || len(dfenBars) < 30 {
		log.Fatalf("DFEN bars not found or insufficient")
	}

	n := len(dfenBars)
	fmt.Printf("Analyzing %d trading days of DFEN history...\n\n", n)

	// 1. Calculate general 10-day Max Adverse Excursion (MAE) across ALL rolling 10-day periods
	var all10dMaxDrops []float64
	hitDropCountAll := map[float64]int{
		0.05: 0,
		0.07: 0,
		0.10: 0,
		0.12: 0,
		0.15: 0,
	}

	for i := 0; i < n-10; i++ {
		entryPrice := dfenBars[i].Close
		minLowNext10 := dfenBars[i+1].Low
		for j := 1; j <= 10; j++ {
			if dfenBars[i+j].Low < minLowNext10 {
				minLowNext10 = dfenBars[i+j].Low
			}
		}
		maxDrop := (minLowNext10 - entryPrice) / entryPrice
		all10dMaxDrops = append(all10dMaxDrops, maxDrop)

		for threshold := range hitDropCountAll {
			if maxDrop <= -threshold {
				hitDropCountAll[threshold]++
			}
		}
	}

	// 2. Calculate 10-day Max Adverse Excursion specifically on Whitings Creek Oversold Signals
	wcSignals := (&strategy.WhitingsCreekStrategy{}).GenerateSignals(map[string][]models.Bar{"DFEN": dfenBars})
	
	// Map date to bar index
	dateToIdx := make(map[string]int)
	for idx, b := range dfenBars {
		dateToIdx[b.Date] = idx
	}

	type WCSignalOutcome struct {
		Date        string
		EntryPrice  float64
		MinLow10d   float64
		MaxHigh10d  float64
		MaxDropPct  float64
		MaxGainPct  float64
		Close10dPct float64
		Hit7PctStop bool
		Hit20PctTP  bool
	}

	var wcOutcomes []WCSignalOutcome
	wcHitStop7 := 0
	wcHitTP20 := 0

	for _, sig := range wcSignals {
		idx, ok := dateToIdx[sig.Date]
		if !ok || idx >= n-1 {
			continue
		}

		entryPrice := sig.Close
		endLookahead := int(math.Min(float64(idx+10), float64(n-1)))
		minLow := dfenBars[idx+1].Low
		maxHigh := dfenBars[idx+1].High

		for k := idx + 1; k <= endLookahead; k++ {
			if dfenBars[k].Low < minLow {
				minLow = dfenBars[k].Low
			}
			if dfenBars[k].High > maxHigh {
				maxHigh = dfenBars[k].High
			}
		}

		maxDrop := (minLow - entryPrice) / entryPrice
		maxGain := (maxHigh - entryPrice) / entryPrice
		close10d := (dfenBars[endLookahead].Close - entryPrice) / entryPrice

		hitStop := maxDrop <= -0.07
		hitTP := maxGain >= 0.20

		if hitStop {
			wcHitStop7++
		}
		if hitTP {
			wcHitTP20++
		}

		wcOutcomes = append(wcOutcomes, WCSignalOutcome{
			Date:        sig.Date,
			EntryPrice:  entryPrice,
			MinLow10d:   minLow,
			MaxHigh10d:  maxHigh,
			MaxDropPct:  maxDrop * 100.0,
			MaxGainPct:  maxGain * 100.0,
			Close10dPct: close10d * 100.0,
			Hit7PctStop: hitStop,
			Hit20PctTP:  hitTP,
		})
	}

	fmt.Printf("========================================================================================\n")
	fmt.Printf("📊 DFEN EMPIRICAL DRAWDOWN & STOP-LOSS PROBABILITY ANALYSIS (10-DAY WINDOW)\n")
	fmt.Printf("========================================================================================\n\n")

	fmt.Printf("1. GENERAL PROBABILITY ACROSS ALL 10-DAY WINDOWS IN DFEN (Sample: %d periods):\n", len(all10dMaxDrops))
	probTable := tablewriter.NewWriter(os.Stdout)
	probTable.SetHeader([]string{"Stop Level", "Times Triggered", "Probability of Hit within 10 Days", "Survival Rate"})
	probTable.SetBorder(true)

	for _, th := range []float64{0.05, 0.07, 0.10, 0.12, 0.15} {
		cnt := hitDropCountAll[th]
		prob := float64(cnt) / float64(len(all10dMaxDrops)) * 100.0
		probTable.Append([]string{
			fmt.Sprintf("-%.0f%%", th*100),
			fmt.Sprintf("%d / %d", cnt, len(all10dMaxDrops)),
			fmt.Sprintf("%.1f%%", prob),
			fmt.Sprintf("%.1f%%", 100.0-prob),
		})
	}
	probTable.Render()

	fmt.Printf("\n2. ON WHITINGS CREEK OVERSOLD SIGNALS IN DFEN (Sample: %d historical triggers):\n", len(wcOutcomes))
	fmt.Printf("   - Probability of hitting -7%% stop within 10 days: %.1f%% (%d of %d triggers)\n",
		float64(wcHitStop7)/float64(len(wcOutcomes))*100.0, wcHitStop7, len(wcOutcomes))
	fmt.Printf("   - Probability of hitting +20%% target within 10 days: %.1f%% (%d of %d triggers)\n\n",
		float64(wcHitTP20)/float64(len(wcOutcomes))*100.0, wcHitTP20, len(wcOutcomes))

	sigTable := tablewriter.NewWriter(os.Stdout)
	sigTable.SetHeader([]string{"Signal Date", "Entry Price", "Max Drop in 10d", "Max Gain in 10d", "Close after 10d", "Hit -7% Stop?", "Hit +20% Target?"})
	sigTable.SetBorder(true)

	for _, oc := range wcOutcomes {
		hitStopStr := "No"
		if oc.Hit7PctStop {
			hitStopStr = "YES ❌"
		}
		hitTPStr := "No"
		if oc.Hit20PctTP {
			hitTPStr = "YES 🎯"
		}
		sigTable.Append([]string{
			oc.Date,
			fmt.Sprintf("$%.2f", oc.EntryPrice),
			fmt.Sprintf("%.2f%%", oc.MaxDropPct),
			fmt.Sprintf("+%.2f%%", oc.MaxGainPct),
			fmt.Sprintf("%.2f%%", oc.Close10dPct),
			hitStopStr,
			hitTPStr,
		})
	}
	sigTable.Render()
}
