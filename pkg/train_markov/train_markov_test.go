package train_markov

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/darianmavgo/backtestgosqlite/pkg/storage"
)

// wave is a close series that swings between trends so all three states occur.
func wave(n int) []float64 {
	out := make([]float64, n)
	p := 100.0
	for i := range out {
		switch (i / 15) % 4 {
		case 0:
			p *= 1.012
		case 1:
			p *= 0.998
		case 2:
			p *= 0.988
		default:
			p *= 1.0005
		}
		out[i] = p
	}
	return out
}

type want struct {
	state              int
	probBull, probBear float64
}

// expected walks the series the plain way: state from the 20-bar return,
// transitions to the next state, counts over earlier bars only.
func expected(closes []float64) map[int]want {
	var idx, states []int
	for i := 20; i < len(closes); i++ {
		r := (closes[i] - closes[i-20]) / closes[i-20]
		s := 0
		if r >= 0.05 {
			s = 1
		} else if r <= -0.05 {
			s = -1
		}
		idx = append(idx, i)
		states = append(states, s)
	}
	out := map[int]want{}
	for j := range idx {
		var bull, bear, total int
		for k := 0; k < j; k++ { // earlier bars with a known next state
			if states[k] != states[j] {
				continue
			}
			total++
			switch states[k+1] {
			case 1:
				bull++
			case -1:
				bear++
			}
		}
		w := want{state: states[j]}
		if total > 0 {
			w.probBull = float64(bull) / float64(total)
			w.probBear = float64(bear) / float64(total)
		}
		out[idx[j]] = w
	}
	return out
}

func marketDB(t *testing.T, closes map[string][]float64) (string, time.Time) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "market.db")
	db, err := storage.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE backtest_start (idx INTEGER, symbol TEXT, Date TEXT, timeframe TEXT, open REAL, high REAL, low REAL, close REAL, volume INTEGER)`); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	for sym, cs := range closes {
		for i, c := range cs {
			d := day.AddDate(0, 0, i).Format("2006-01-02")
			if _, err := db.Exec(`INSERT INTO backtest_start VALUES (?, ?, ?, '1d', ?, ?, ?, ?, 1000)`, i, sym, d, c, c, c, c); err != nil {
				t.Fatal(err)
			}
		}
	}
	return path, day
}

func TestTrainPersistsWalkForwardModel(t *testing.T) {
	closes := wave(150)
	market, day := marketDB(t, map[string][]float64{"ZZZ": closes, "SHORT": closes[:15]})
	modelPath := filepath.Join(t.TempDir(), "markov.db")

	res, err := Train(context.Background(), Config{MarketDB: market, ModelDB: modelPath, Symbols: []string{"ZZZ", "short", "ZZZ"}, Batch: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Requested != 2 || res.Trained != 1 || res.Skipped != 1 || res.Rows != 130 {
		t.Fatalf("result %+v", res)
	}

	db, err := storage.OpenSQLiteReadOnly(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var rows []struct {
		Date     string  `db:"date"`
		State    int     `db:"state"`
		ProbBull float64 `db:"prob_bull"`
		ProbBear float64 `db:"prob_bear"`
		Signal   float64 `db:"signal"`
	}
	if err := db.Select(&rows, "SELECT date, state, prob_bull, prob_bear, signal FROM markov_prediction WHERE symbol = 'ZZZ' ORDER BY date"); err != nil {
		t.Fatal(err)
	}
	exp := expected(closes)
	if len(rows) != len(exp) {
		t.Fatalf("%d rows, want %d", len(rows), len(exp))
	}
	states := map[int]bool{}
	for k, r := range rows {
		w := exp[20+k]
		if r.Date != day.AddDate(0, 0, 20+k).Format("2006-01-02") || r.State != w.state ||
			math.Abs(r.ProbBull-w.probBull) > 1e-9 || math.Abs(r.ProbBear-w.probBear) > 1e-9 ||
			math.Abs(r.Signal-(w.probBull-w.probBear)) > 1e-9 {
			t.Fatalf("row %d: got %+v want %+v", k, r, w)
		}
		states[r.State] = true
	}
	if len(states) != 3 {
		t.Fatalf("the series only exercised states %v", states)
	}

	var meta int
	if err := db.Get(&meta, "SELECT COUNT(*) FROM markov_model_meta WHERE symbol = 'ZZZ' AND bars = 130"); err != nil || meta != 1 {
		t.Fatalf("meta rows %d err %v", meta, err)
	}
	if err := db.Get(&meta, "SELECT COUNT(*) FROM markov_model_meta WHERE symbol = 'SHORT'"); err != nil || meta != 0 {
		t.Fatalf("a symbol with too little history must have no model, got %d (err %v)", meta, err)
	}

	// Retraining replaces rather than duplicates.
	if _, err := Train(context.Background(), Config{MarketDB: market, ModelDB: modelPath, Symbols: []string{"ZZZ"}}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.Get(&n, "SELECT COUNT(*) FROM markov_prediction WHERE symbol = 'ZZZ'"); err != nil || n != 130 {
		t.Fatalf("after retrain %d rows (err %v)", n, err)
	}
}
