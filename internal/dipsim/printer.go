package dipsim

import (
	"fmt"
	"os"

	"github.com/olekukonko/tablewriter"
)

// PrintResult prints a single simulation result as a formatted table.
func PrintResult(res DipSimResult) {
	fmt.Printf("\n📊 %s\n\n", res.Config.Label)

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Metric", "Value"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	table.Append([]string{"Starting Capital", fmt.Sprintf("$%.2f", res.Config.InitialCapital)})
	table.Append([]string{"Ending Capital", fmt.Sprintf("💰 $%.2f", res.EndingCap)})
	table.Append([]string{"Net Profit", fmt.Sprintf("+$%.2f", res.NetProfit)})
	table.Append([]string{"Total Return", fmt.Sprintf("+%.2f%%", res.TotalReturn)})
	table.Append([]string{"CAGR (Annualized)", fmt.Sprintf("%.2f%% / yr", res.CAGR)})
	table.Append([]string{"Max MTM Drawdown", fmt.Sprintf("%.2f%%", res.MTMMaxDD)})
	table.Append([]string{"Calmar Ratio", fmt.Sprintf("%.2f", res.CalmarRatio)})
	table.Append([]string{"Win Rate", fmt.Sprintf("%.1f%% (%dW / %dL)", res.WinRate, res.Wins, res.Losses)})
	table.Append([]string{"Profit Factor", fmt.Sprintf("%.2f", res.ProfitFactor)})
	table.Append([]string{"Total Trades", fmt.Sprintf("%d", res.TotalTrades)})
	table.Append([]string{"Avg Hold Days", fmt.Sprintf("%.1f", res.AvgHoldDays)})

	table.Render()
}

// PrintResultsRanked prints a slice of results as a ranked comparison table.
func PrintResultsRanked(title string, results []DipSimResult) {
	fmt.Printf("\n%s\n\n", title)

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Rank", "Config", "5-Yr Profit ($)", "Ending Cap", "CAGR", "Max MTM DD", "Calmar", "Win Rate", "Trades"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	for i, r := range results {
		table.Append([]string{
			fmt.Sprintf("#%d", i+1),
			r.Config.Label,
			fmt.Sprintf("+$%.2f", r.NetProfit),
			fmt.Sprintf("💰 $%.2f", r.EndingCap),
			fmt.Sprintf("%.2f%%/yr", r.CAGR),
			fmt.Sprintf("%.2f%%", r.MTMMaxDD),
			fmt.Sprintf("⭐ %.2f", r.CalmarRatio),
			fmt.Sprintf("%.1f%%", r.WinRate),
			fmt.Sprintf("%d", r.TotalTrades),
		})
	}
	table.Render()
}

// PrintComboResult prints a combo simulation result as a comparison against VOO.
func PrintComboResult(res ComboResult, vooFinal, vooProfit, vooMaxDD float64) {
	fmt.Printf("\n👑 %s + %s Combo Strategy\n\n", res.LongConfig.TradeSymbol, res.ShortConfig.TradeSymbol)

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Metric", "👑 Combo Strategy", "🏛️ VOO Benchmark", "Edge"})
	table.SetBorder(true)
	table.SetAutoWrapText(false)

	table.Append([]string{"Ending Capital", fmt.Sprintf("💰 $%.2f", res.EndingCap), fmt.Sprintf("$%.2f", vooFinal), fmt.Sprintf("+$%.2f", res.NetProfit-vooProfit)})
	table.Append([]string{"Net Profit", fmt.Sprintf("+$%.2f", res.NetProfit), fmt.Sprintf("+$%.2f", vooProfit), fmt.Sprintf("%.1fx", res.NetProfit/vooProfit)})
	table.Append([]string{"CAGR", fmt.Sprintf("%.2f%%/yr", res.CAGR), fmt.Sprintf("%.2f%%/yr", (vooFinal/res.LongConfig.InitialCapital-1)*100/5), ""})
	table.Append([]string{"Max Drawdown", fmt.Sprintf("%.2f%%", res.MTMMaxDD), fmt.Sprintf("%.2f%%", vooMaxDD), fmt.Sprintf("%.1fx less", vooMaxDD/res.MTMMaxDD)})
	table.Append([]string{"Calmar Ratio", fmt.Sprintf("⭐ %.2f", res.CalmarRatio), "", ""})
	table.Append([]string{"Trades", fmt.Sprintf("%d (%dL / %dS)", res.TotalTrades, res.LongTrades, res.ShortTrades), "1 (passive)", ""})
	table.Append([]string{"Win Rate", fmt.Sprintf("%.1f%%", res.WinRate), "", ""})
	table.Append([]string{"Profit Factor", fmt.Sprintf("%.2f", res.ProfitFactor), "", ""})

	table.Render()
}
