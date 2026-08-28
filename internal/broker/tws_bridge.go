// Package broker provides TWS Socket bridge and automated order execution for Interactive Brokers.
package broker

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// TWSAccountPosition represents an open position reported by TWS.
type TWSAccountPosition struct {
	Symbol   string  `json:"symbol"`
	SecType  string  `json:"secType"`
	Shares   float64 `json:"shares"`
	AvgCost  float64 `json:"avgCost"`
}

// TWSAccountSummary contains live account metrics retrieved directly from TWS socket.
type TWSAccountSummary struct {
	Success        bool                 `json:"success"`
	Connected      bool                 `json:"connected"`
	Port           int                  `json:"port"`
	AccountID      string               `json:"account_id"`
	IsPaper        bool                 `json:"is_paper"`
	NetLiquidation float64              `json:"net_liquidation"`
	BuyingPower    float64              `json:"buying_power"`
	CashBalance    float64              `json:"cash_balance"`
	Positions      []TWSAccountPosition `json:"positions"`
	Error          string               `json:"error,omitempty"`
}

// TWSExecutionResult contains order tickets and fill statuses returned from TWS socket.
type TWSExecutionResult struct {
	Success           bool    `json:"success"`
	ConnectedPort     int     `json:"connected_port"`
	AccountID         string  `json:"account_id"`
	Symbol            string  `json:"symbol"`
	Quantity          float64 `json:"quantity"`
	EstPrice          float64 `json:"est_price"`
	TotalCost         float64 `json:"total_cost"`
	TargetPrice       float64 `json:"target_price"`
	StopPrice         string  `json:"stop_price"`
	ParentOrderID     int     `json:"parent_order_id"`
	TakeProfitOrderID int     `json:"take_profit_order_id"`
	StopOrderID       int     `json:"stop_order_id"`
	ParentStatus      string  `json:"parent_status"`
	TPStatus          string  `json:"tp_status"`
	StopStatus        string  `json:"stop_status"`
	Message           string  `json:"message"`
	Error             string  `json:"error,omitempty"`
}

// QueryTWSStatus calls the Python TWS bridge to check connection and retrieve live account summary.
func QueryTWSStatus() (*TWSAccountSummary, error) {
	pythonPath := ".venv/bin/python"
	if _, err := os.Stat(pythonPath); os.IsNotExist(err) {
		pythonPath = "python3"
	}

	scriptPath := "scripts/ibkr_tws_executor.py"
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		scriptPath = filepath.Join("..", "..", "scripts", "ibkr_tws_executor.py")
	}

	cmd := exec.Command(pythonPath, scriptPath, "--status")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to run TWS bridge: %v (output: %s)", err, string(out))
	}

	var summary TWSAccountSummary
	if err := json.Unmarshal(out, &summary); err != nil {
		// Attempt to extract JSON from output
		start := -1
		for i, c := range out {
			if c == '{' {
				start = i
				break
			}
		}
		if start >= 0 {
			if err := json.Unmarshal(out[start:], &summary); err == nil {
				return &summary, nil
			}
		}
		return nil, fmt.Errorf("failed to parse TWS response: %v (raw: %s)", err, string(out))
	}

	return &summary, nil
}

// ExecuteTWSOrder submits the complete fractional bracket order directly into Trader Workstation.
func ExecuteTWSOrder(cfg ComplexOrderConfig) (*TWSExecutionResult, error) {
	pythonPath := ".venv/bin/python"
	if _, err := os.Stat(pythonPath); os.IsNotExist(err) {
		pythonPath = "python3"
	}

	scriptPath := "scripts/ibkr_tws_executor.py"

	args := []string{
		scriptPath,
		"--symbol", cfg.Symbol,
		"--amount", fmt.Sprintf("%.2f", cfg.DollarAmount),
		"--tp", fmt.Sprintf("%.4f", cfg.TakeProfitPct),
		"--sl", fmt.Sprintf("%.4f", cfg.StopLossPct),
		"--trail-pct", fmt.Sprintf("%.4f", cfg.TrailingStopPct),
		"--algo-priority", cfg.AlgoPriority,
		"--fallback-price", fmt.Sprintf("%.2f", cfg.EstPrice),
	}

	if cfg.AccountID != "" && cfg.AccountID != "YOUR_IBKR_ACCOUNT_ID" {
		args = append(args, "--account", cfg.AccountID)
	}

	cmd := exec.Command(pythonPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("TWS execution command failed: %v (output: %s)", err, string(out))
	}

	var res TWSExecutionResult
	if err := json.Unmarshal(out, &res); err != nil {
		start := -1
		for i, c := range out {
			if c == '{' {
				start = i
				break
			}
		}
		if start >= 0 {
			if err := json.Unmarshal(out[start:], &res); err == nil {
				return &res, nil
			}
		}
		return nil, fmt.Errorf("failed to parse TWS execution result: %v (raw: %s)", err, string(out))
	}

	return &res, nil
}
