# 📈 Strategy Specification: Whitings Creek (WC)

Whitings Creek is an institutional mean-reversion strategy engineered to capture short-term panic selling and oversold cliff events.

## Strategy Logic

| Parameter | Default Value | Description |
| :--- | :---: | :--- |
| **Cliff Drop Ratio** | `0.90` ($-10\%$) | Asset must close $\ge 10\%$ below its trailing minimum low |
| **Lookback Window** | `3` to `12` days | Historical low reference period (`Day -3` to `Day -12`) |
| **Entry Rule** | Limit Buy at Close | Limit order placed at or below signal-day closing price |
| **Profit Target** | `1.20` ($+20\%$) | Take-profit limit exit target |
| **Stop-Loss Floor** | `0.93` ($-7\%$) | Protective stop-loss exit |
| **Max Holding Period**| `10` trading days | Time-up market exit if neither target nor stop is hit |
| **Confidence Threshold** | `90%` win rate | Minimum historical win rate required for live qualification |
| **Sample Size Floor** | $\ge 5$ signals | Minimum historical trade occurrences required |

## Running the Strategy

```bash
# Run Go-based implementation
./bin/backtest -strategy wc -capital 100000 -target 1.20 -stoploss 0.93

# Run SQL pipeline implementation
./bin/backtest -strategy whitings_creek-sql -capital 100000
```
