# 📉 Millwharf Strategy: Weekly Consistent Decline Reversal

The **Millwharf Strategy** is an institutional mean-reversion trading model tailored for leveraged and high-beta ETF universes. It exploits exhaustion in severe, unbroken daily sell-offs.

---

## 1. Quantitative Rules & Architecture

1. **Universe Screening**: Every week, evaluate all available ETFs for consecutive declining daily closes ($\text{Close}_t < \text{Close}_{t-1}$).
2. **Decline Qualification**: Declines must be unbroken for at least **5 consecutive trading sessions** ($\text{Streak} \ge 5$).
3. **Weekly Stock Selection**: Every week, select exactly **one ETF** exhibiting the **longest consistent decline streak** across the entire universe (tie-broken by largest cumulative drop percentage from peak).
4. **Dynamic Take-Profit**:
   $$\text{Target Price} = \min\left(\max_{j=t-5\dots t}(\text{High}_j), \; \text{Close}_t \times 1.20\right)$$
   *Exits immediately on any intraday touch where $\text{High} \ge \text{Target Price}$.*
5. **Protective Stop Loss**: **None** ($0\%$). The position holds through adverse drawdowns.
6. **Time-Based Exit**: **4 trading days**. If the profit target is not triggered during the 4-day holding window, the position is closed unconditionally at **market open** on Day 4.

---

## 2. 4-Year Performance Tear Sheet (2022 – 2026)

Simulated over **1,004 trading days** (August 22, 2022 ➔ August 21, 2026) across 17 leveraged/index ETFs (including SOXL, TQQQ, CONL, BITX, TSLL, LABU, DPST, FAS, NUGT, DFEN, QQQ, SPY):

| Metric | Value | Benchmark / Institutional Context |
| :--- | :--- | :--- |
| **Initial Capital** | **\$100,000.00** | Starting cash allocation |
| **Ending Equity** | **\$173,289.33** | Total realized & open portfolio equity |
| **Net Realized Profit** | **+\$73,289.33 (+73.29%)** | Total return over 4-year backtest |
| **CAGR** | **14.80%** | Compound Annual Growth Rate |
| **Sharpe Ratio** | **1.24** | Risk-adjusted annualized return (Rf = 0%) |
| **Sortino Ratio** | **1.91** | Downside volatility adjusted return |
| **Calmar Ratio** | **1.28** | CAGR / Maximum Drawdown |
| **Max Drawdown (MDD %)** | **11.58%** | Worst peak-to-trough account decline |
| **Max Drawdown (\$ Loss)** | **-\$19,713.43** | Peak: \$170,234.61 ➔ Trough: \$150,521.18 |
| **Total Completed Trades** | **87** | **58 Wins / 29 Losses** |
| **Trade Win Rate** | **66.67%** | 2 out of every 3 trades closed in profit |
| **Profit Factor** | **1.96** | Gross Profit / Gross Loss |
| **Average Holding Period** | **3.7 days** | Capital turns over rapidly (< 4 days) |

---

## 3. Top Longest Declines Recorded (4-Year Universe)

| Rank | Symbol | Consecutive Down Days | Peak Date | Trough Date | Peak Close | Trough Close | Total Drop % |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **#1** | **CONL** | **13 days** | 2026-01-16 | 2026-02-05 | \$16.30 | \$5.72 | **-64.91%** |
| **#2** | **CONL** | **9 days** | 2024-08-23 | 2024-09-06 | \$31.73 | \$17.49 | **-44.88%** |
| **#3** | **CONL** | **8 days** | 2023-08-08 | 2023-08-18 | \$18.49 | \$10.13 | **-45.21%** |
| **#4** | **NUGT** | **8 days** | 2023-02-13 | 2023-02-24 | \$36.07 | \$29.75 | **-17.52%** |
| **#5** | **GUSH** | **8 days** | 2024-08-28 | 2024-09-11 | \$33.62 | \$26.17 | **-22.16%** |
| **#6** | **GUSH** | **8 days** | 2023-10-18 | 2023-10-30 | \$45.71 | \$36.37 | **-20.43%** |
| **#7** | **BITX** | **7 days** | 2026-01-27 | 2026-02-05 | \$28.28 | \$13.87 | **-50.95%** |
| **#8** | **SOXL** | **7 days** | 2022-08-25 | 2022-09-06 | \$19.33 | \$12.42 | **-35.75%** |
| **#9** | **LABU** | **7 days** | 2022-09-15 | 2022-09-26 | \$184.80 | \$120.60 | **-34.74%** |
| **#10** | **CONL** | **7 days** | 2022-10-25 | 2022-11-03 | \$14.93 | \$9.75 | **-34.70%** |

---

## 4. Execution & CLI Commands

```bash
# 1. Run Millwharf scanner & backtest across 4-year ETF history:
./bin/millwharf -db data/leveraged_backtest.db -capital 100000

# 2. Run standard backtest CLI:
./bin/backtest -strategy millwharf -db data/leveraged_backtest.db -capital 100000

# 3. Benchmark Millwharf against other registered strategies:
./bin/compare -db data/leveraged_backtest.db -capital 100000
```
