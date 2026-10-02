package strategy

import (
	"testing"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
)

func TestPriceActionReclaim(t *testing.T) {
	bars := make([]models.Bar, 300)
	for i := 0; i < 300; i++ {
		bars[i] = models.Bar{
			Close: 100 + float64(i%10),
			Low:   95 + float64(i%10),
			SMA200: 90,
		}
	}
	// trigger a reclaim
	bars[290].Low = 50
	bars[290].Close = 50
	bars[291].Close = 100 // reclaimed

	barsBySymbol := map[string][]models.Bar{
		"GOOGL": bars,
	}

	strat := &PriceActionReclaimStrategy{}
	signals := strat.GenerateSignals(barsBySymbol)
	t.Logf("Generated %d signals", len(signals))
}
