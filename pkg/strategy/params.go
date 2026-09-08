package strategy

import "fmt"

// ParamSet represents a key-value dictionary of numeric strategy parameters.
type ParamSet map[string]float64

// Get retrieves a float parameter with a fallback default.
func (p ParamSet) Get(key string, defaultVal float64) float64 {
	if v, ok := p[key]; ok {
		return v
	}
	return defaultVal
}

// GetInt retrieves an integer parameter with a fallback default.
func (p ParamSet) GetInt(key string, defaultVal int) int {
	if v, ok := p[key]; ok {
		return int(v)
	}
	return defaultVal
}

// ParamRange defines a parameter range for optimization / grid search.
type ParamRange struct {
	Name string  `json:"name"`
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

// Values generates all parameter values defined by this range.
func (r ParamRange) Values() []float64 {
	if r.Step <= 0 {
		return []float64{r.Min}
	}
	var vals []float64
	for v := r.Min; v <= r.Max+1e-9; v += r.Step {
		vals = append(vals, v)
	}
	return vals
}

// ValidateConfig performs standard validation checks on StrategyConfig.
func ValidateConfig(cfg StrategyConfig) error {
	if cfg.HoldingWindow < 1 {
		return fmt.Errorf("holding window must be >= 1, got %d", cfg.HoldingWindow)
	}
	if cfg.PositionCap < 1 {
		return fmt.Errorf("position cap must be >= 1, got %d", cfg.PositionCap)
	}
	if cfg.AllocationPct <= 0 || cfg.AllocationPct > 1.0 {
		return fmt.Errorf("allocation percentage must be between 0 and 1.0, got %f", cfg.AllocationPct)
	}
	return nil
}
