package runner

import (
	"fmt"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

// ResolveStrategies parses a strategy arg the same way cmd/backtest and
// cmd/livescan always have: "all", comma/space-separated IDs, or empty →
// defaultID when provided.
func ResolveStrategies(arg, defaultID string) ([]strategy.Strategy, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		arg = strings.TrimSpace(defaultID)
	}
	if arg == "" {
		return nil, fmt.Errorf("no strategies selected")
	}
	if strings.EqualFold(arg, "all") {
		list := strategy.List()
		if len(list) == 0 {
			return nil, fmt.Errorf("no strategies registered")
		}
		return list, nil
	}
	var out []strategy.Strategy
	for _, token := range strings.FieldsFunc(arg, func(r rune) bool { return r == ',' || r == ' ' }) {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		s, ok := strategy.Get(token)
		if !ok {
			return nil, fmt.Errorf("strategy %q not found in registry (use -list)", token)
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no valid strategies selected")
	}
	return out, nil
}
