package datasource

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePolygonTimeframe(t *testing.T) {
	tests := []struct {
		input        string
		expectedMult int
		expectedSpan string
	}{
		{"1d", 1, "day"},
		{"day", 1, "day"},
		{"", 1, "day"},
		{"1h", 1, "hour"},
		{"hour", 1, "hour"},
		{"5m", 5, "minute"},
		{"15m", 15, "minute"},
		{"1m", 1, "minute"},
		{"1w", 1, "week"},
		{"week", 1, "week"},
	}

	for _, tt := range tests {
		mult, span := parsePolygonTimeframe(tt.input)
		if mult != tt.expectedMult || span != tt.expectedSpan {
			t.Errorf("parsePolygonTimeframe(%q) = (%d, %q), expected (%d, %q)",
				tt.input, mult, span, tt.expectedMult, tt.expectedSpan)
		}
	}
}

func TestResolvePolygonAPIKey_EnvFile(t *testing.T) {
	tempDir := t.TempDir()
	envPath := filepath.Join(tempDir, ".env")
	envContent := `
# Sample comment
POLYGON_API_KEY="test_key_from_env"
SOME_OTHER_VAR=123
`
	if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to write temp .env: %v", err)
	}

	envMap, err := parseSimpleEnvFile(envPath)
	if err != nil {
		t.Fatalf("failed to parse env file: %v", err)
	}

	if envMap["POLYGON_API_KEY"] != "test_key_from_env" {
		t.Errorf("expected test_key_from_env, got %s", envMap["POLYGON_API_KEY"])
	}
}
