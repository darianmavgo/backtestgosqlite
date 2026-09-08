package datasource

import (
	"strings"
	"testing"
)

func TestParseCSVReader_StandardFormat(t *testing.T) {
	csvData := `Date,Open,High,Low,Close,Adj Close,Volume
2023-01-03,100.0,105.0,99.0,104.0,104.0,1000000
2023-01-04,104.0,108.0,103.0,107.0,107.0,1200000
2023-01-05,107.0,107.5,102.0,103.0,103.0,900000`

	bars, err := ParseCSVReader(strings.NewReader(csvData), "AAPL", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(bars) != 3 {
		t.Fatalf("expected 3 bars, got %d", len(bars))
	}

	if bars[0].Symbol != "AAPL" || bars[0].Date != "2023-01-03" || bars[0].Close != 104.0 {
		t.Errorf("unexpected first bar: %+v", bars[0])
	}
	if bars[2].Volume != 900000 {
		t.Errorf("expected volume 900000, got %d", bars[2].Volume)
	}
}

func TestParseCSVReader_WithSymbolColumn(t *testing.T) {
	csvData := `Ticker,Timestamp,Open,High,Low,Close,Vol
SPY,2023-01-03 09:30:00,380.0,385.0,379.0,382.0,50000000
QQQ,2023-01-03 09:30:00,265.0,270.0,264.0,268.0,30000000`

	bars, err := ParseCSVReader(strings.NewReader(csvData), "", nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(bars) != 2 {
		t.Fatalf("expected 2 bars, got %d", len(bars))
	}

	if bars[0].Symbol != "SPY" || bars[0].Close != 382.0 {
		t.Errorf("unexpected bar 0: %+v", bars[0])
	}
	if bars[1].Symbol != "QQQ" || bars[1].Close != 268.0 {
		t.Errorf("unexpected bar 1: %+v", bars[1])
	}
}
