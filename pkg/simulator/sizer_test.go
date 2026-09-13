package simulator

import (
	"testing"

	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

func TestFixedSharesSizer_CalculateShares(t *testing.T) {
	sizer := &FixedSharesSizer{}

	tests := []struct {
		name        string
		accountCash float64
		totalEquity float64
		entryPrice  float64
		fixedShares int
		expected    int
	}{
		{
			name:        "happy path - sufficient cash",
			accountCash: 10000.0,
			totalEquity: 10000.0,
			entryPrice:  100.0,
			fixedShares: 50,
			expected:    50, // 50 * 100 = 5000 <= 10000
		},
		{
			name:        "insufficient cash - hits limit",
			accountCash: 1000.0,
			totalEquity: 1000.0,
			entryPrice:  100.0,
			fixedShares: 50,
			expected:    9, // (1000 * 0.95) / 100 = 9.5 -> 9
		},
		{
			name:        "entry price is zero",
			accountCash: 1000.0,
			totalEquity: 1000.0,
			entryPrice:  0.0,
			fixedShares: 50,
			expected:    0,
		},
		{
			name:        "entry price is negative",
			accountCash: 1000.0,
			totalEquity: 1000.0,
			entryPrice:  -10.0,
			fixedShares: 50,
			expected:    0,
		},
		{
			name:        "fixed shares is zero",
			accountCash: 1000.0,
			totalEquity: 1000.0,
			entryPrice:  100.0,
			fixedShares: 0,
			expected:    0,
		},
		{
			name:        "fixed shares is negative",
			accountCash: 1000.0,
			totalEquity: 1000.0,
			entryPrice:  100.0,
			fixedShares: -10,
			expected:    0,
		},
		{
			name:        "low cash limit hit",
			accountCash: 100.0,
			totalEquity: 100.0,
			entryPrice:  200.0,
			fixedShares: 50,
			expected:    0, // (100 * 0.95) / 200 = 0.475 -> 0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := strategy.StrategyConfig{
				FixedShares: tt.fixedShares,
			}
			got := sizer.CalculateShares(tt.accountCash, tt.totalEquity, tt.entryPrice, cfg)
			if got != tt.expected {
				t.Errorf("CalculateShares() = %v, want %v", got, tt.expected)
			}
		})
	}
}
