package download

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestParseSymbolArgs(t *testing.T) {
	got := parseSymbolArgs([]string{"VOO,", "IEF,", "GLD,", "USO,", "HYG"})
	want := []string{"VOO", "IEF", "GLD", "USO", "HYG"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	got = parseSymbolArgs([]string{"voo,ief", " gld "})
	want = []string{"VOO", "IEF", "GLD"}
	if len(got) != len(want) || got[0] != "VOO" || got[1] != "IEF" || got[2] != "GLD" {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(parseSymbolArgs(nil)) != 0 {
		t.Fatal("empty args should yield no symbols")
	}
}

func TestRunRequiresDBAndPolygonKey(t *testing.T) {
	if _, err := Run(context.Background(), Config{}); err == nil {
		t.Fatal("expected error for empty DB")
	}
	// Run must not fall back to POLYGON_API_KEY from the environment.
	t.Setenv("POLYGON_API_KEY", "from-env")
	_, err := Run(context.Background(), Config{DB: filepath.Join(t.TempDir(), "m.db"), Source: "polygon", Symbols: "SPY"})
	if err == nil {
		t.Fatal("expected error: polygon source without Config.PolygonKey")
	}
}

func TestRunStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	_, err := Run(ctx, Config{DB: filepath.Join(t.TempDir(), "m.db"), Symbols: "SPY,QQQ", Out: &out})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestRunNilOutIsSilentAndSafe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// nil Out must not panic.
	_, _ = Run(ctx, Config{DB: filepath.Join(t.TempDir(), "m.db"), Symbols: "SPY"})
}
