# 📈 Strategy Specification: BB-Capitulation + Reversal Bounce

The Bollinger Band Capitulation strategy identifies deep exhaustion capitulation candles followed by immediate buyer confirmation.

## Strategy Rules

1. **Capitulation Trigger (Day T-1)**:
   - Price low pierces below Lower Bollinger Band (20, 2.0).
   - Wilder's 5-period RSI drops below 30 (`RSI(5) < 30`).
2. **Reversal Confirmation (Day T)**:
   - Close on Day T is higher than Close on Day T-1 (bullish green candle).
3. **Execution**:
   - Limit entry placed at Close of Day T.
   - Profit Target: +18%.
   - Protective Stop Loss: -7%.
   - Maximum holding period: 10 trading days.

## Running the Strategy

```bash
# Universe-wide backtest
./bin/backtest -strategy bb-capitulation -capital 100000

# Single symbol backtest
./bin/backtest -strategy bb-capitulation -symbol SOXL -capital 100000
```
