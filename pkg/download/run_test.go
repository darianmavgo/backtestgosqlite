package download

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

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
