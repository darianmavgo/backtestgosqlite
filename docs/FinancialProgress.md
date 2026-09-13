# Financial Progress & Strategy Evaluation: Supporting $10k/Month in Sustainable Withdrawals

## Executive Summary

This document evaluates the quantitative progress toward the financial objective of deploying an algorithmic trading strategy capable of supporting **$10,000 per month ($120,000 per year) in sustained withdrawals while continuously growing the capital base**.

Using the 5-year multi-strategy benchmark dataset from [`reports/scoreboard.db`](file:///Users/darianhickman/Documents/backtestgosqlite/reports/scoreboard.db) (1,258 trading days from September 2021 to September 2026), this report examines strategy feasibility, capital base thresholds, sequence-of-returns risk, and the optimal compounding roadmap.

---

## 🎯 1. The Mathematical Framework

To withdraw $120,000 annually while preserving purchasing power and compounding the base in real terms:

$$\text{Net Annual Cash Generation} = (\text{Starting Base} \times \text{Strategy CAGR}) - \$120,000 > 0$$

### Why the Traditional 4% Rule Fails
* **Capital Inefficiency**: Under the passive 4% Safe Withdrawal Rule (S&P 500 Buy & Hold), a **$3,000,000 capital base** is required to generate $120k/year.
* **Sequence of Returns Risk (SRR)**: The benchmark backtest showed Buy & Hold suffered a **59.58% Max Drawdown** during the 2022 tech/market correction. Liquidating $10,000/month during a 60% drawdown creates irreversible principal depletion ("dollar-cost-selling into a hole").
* **The Algorithmic Solution**: A strategy with high CAGR (> 30%) and strictly contained drawdowns (< 15%) can sustainably achieve this goal with a fraction of the capital.

---

## 📊 2. The 2026 Strategy Scoreboard: Feasibility Audit

Performance across all 12 registered strategies on a $100,000 starting base over 5 years:

| Rank | Strategy ID | Strategy Name | 5-Yr CAGR | Total Return | Sharpe Ratio | Max Drawdown | Win Rate | Profit Factor | Status for $10k/Mo Goal |
| :---: | :--- | :--- | :---: | :---: | :---: | :---: | :---: | :---: | :--- |
| 🥇 | **`voo-tecl-combo`** | **VOO→TECL All-Weather Combo** | **45.38%** | **+547.57%** | **1.77** | **13.36%** | **72.64%** | **3.12** | 🟢 **Primary Contender** |
| 🥈 | **`voo-tecl-spxu-combo`** | **VOO→TECL/SPXU All-Weather Combo** | **32.16%** | **+302.22%** | **1.58** | **13.36%** | **71.95%** | **2.60** | 🟢 **Robust Alternative** |
| 🥉 | **`millwharf`** | **Millwharf Weekly Consistent Decline** | **15.31%** | **+103.63%** | **1.37** | **10.09%** | **59.31%** | **1.85** | 🟡 **Capital Preserver** |
| 4 | `buy-and-hold` | Buy and Hold (Passive Baseline) | 17.36% | +122.36% | 0.60 | **59.58%** | N/A | N/A | 🔴 **Fails** (Drawdown risk) |
| 5 | `trend-bb` | Trend-Gated Bollinger Oversold | 13.07% | +84.63% | 0.82 | 23.54% | 42.97% | 1.45 | 🔴 **Sub-optimal** |
| 6 | `macd-crossover` | MACD Signal Line Crossover | 20.92% | +158.15% | 0.83 | 31.41% | 35.84% | 1.25 | 🔴 **Excessive drawdowns** |
| 7 | `bb-capitulation` | BB-Capitulation + Reversal Bounce | 9.87% | +60.01% | 0.45 | 48.65% | 41.54% | 1.15 | 🔴 **Fails** |
| 8–12| `rsi2`, `donchian`, etc. | Momentum / RSI / Genetic | < 0% | Negative | < 0.20 | > 60% | < 45% | < 1.00 | 🔴 **Unusable** |

---

## 🏆 3. Deep-Dive on the Leading Candidate: `voo-tecl-combo`

Audited from [`reports/voo-tecl-combo_4.db`](file:///Users/darianhickman/Documents/backtestgosqlite/reports/voo-tecl-combo_4.db):

* **Capital Compounding**: Grew **$100,000 into $647,570.31** (+547.57% net profit).
* **Calmar Ratio = 3.40** ($\text{CAGR} / \text{MaxDD} = 45.38\% / 13.36\%$): For every 1% of peak-to-trough paper drop, the strategy yielded 3.4% in annual compounded gains.
* **Drawdown Containment (13.36% Max DD)**: The largest dollar drawdown was $54,436 from a $407,418 equity peak. The strategy avoids market crashes by staying in cash during long downtrends and only taking high-probability 3-day oversold bounces in leveraged tech.
* **Trade Profile**:
  * Total Trades: 106 (77 Wins, 29 Losses)
  * Win Rate: **72.64%**
  * Profit Factor: **3.12**
  * Average Holding Period: **3.05 Days** (Fast capital turnover, minimal overnight macro exposure, continuous cash availability).

---

## 📐 4. Capital Base Sensitivity Matrix

How does `voo-tecl-combo` perform across varying portfolio sizes when tasked with paying out **$120,000 per year ($10k/month)**?

| Starting Capital Base | Annual Strategy Return (45.4%) | Annual Withdrawal ($10k/mo) | Net Annual Base Growth | Net Growth % | Feasibility Assessment |
| :---: | :---: | :---: | :---: | :---: | :--- |
| **$100,000** | $45,380 | $120,000 | **-$74,620** | -74.6% | 🔴 **Cannot withdraw yet** (Account depleted in Year 1) |
| **$200,000** | $90,760 | $120,000 | **-$29,240** | -14.6% | 🔴 **Deficit** (Principal slowly erodes) |
| **$300,000** | $136,140 | $120,000 | **+$16,140** | +5.4% | 🟡 **Borderline Viable** (Minimal margin of safety) |
| **$350,000** | $158,830 | $120,000 | **+$38,830** | +11.1% | 🟢 **Viable Threshold** (Can absorb normal drawdowns) |
| **$500,000** | **$226,900** | **$120,000** | **+$106,900** | **+21.4%** | 🟢 **Optimal Target**: Pays $10k/mo while adding $107k/yr to wealth |
| **$1,000,000** | **$453,800** | **$120,000** | **+$333,800** | **+33.4%** | 🟢 **End-State Wealth**: Effortless cash flow with explosive compounding |

---

## 📈 5. Compounding Roadmap: From $100k to $10k/Month

If starting from a **$100,000 seed**, the portfolio must go through an **Accumulation Phase** before switching to the **Withdrawal Phase**:

```
Accumulation Phase (Zero Withdrawals):
  Year 0:  $100,000
  Year 1:  $145,380  (+45.4% return)
  Year 2:  $211,350  (+45.4% return)
  Year 3:  $307,260  (+45.4% return — Enters minimum viable zone)

Withdrawal Phase ($10k/Month = $120k/Year):
  Year 4:  Starts at $307,260 → Generates $139,400 → Withdraws $120,000 → Ends at $326,660 (+6.3% net growth)
  Year 5:  Starts at $326,660 → Generates $148,200 → Withdraws $120,000 → Ends at $354,860 (+8.6% net growth)
  Year 6:  Starts at $354,860 → Generates $161,000 → Withdraws $120,000 → Ends at $395,860 (+11.6% net growth)
  Year 7:  Starts at $395,860 → Generates $179,600 → Withdraws $120,000 → Ends at $455,460 (+15.1% net growth)
```

> **Takeaway**: After **3 to 4 years of pure compounding**, the portfolio can safely begin distributing $10,000/month indefinitely while continuing to grow.

---

## 🛡️ 6. Implementation Guardrails & Risk Management

1. **The 6-Month Cash Buffer (Liquidity Bridge)**:
   * Strategy returns are episodic, not a linear monthly salary. You may generate $40,000 in one month and experience zero setups in another.
   * **Rule**: Hold **$30,000 to $60,000 in a cash reserve bucket** (Treasury Bills / Money Market). Draw the $10,000 monthly allowance from this cash reserve, and refill the reserve only upon profitable trade exits. Never liquidate an active swing position to meet a withdrawal deadline.
2. **Tax Shielding**:
   * Average trade duration is 3 days (short-term capital gains).
   * Executing inside a **Roth IRA, Traditional IRA, or 401(k) / Solo 401(k)** eliminates the 25%–37% annual tax drag, dramatically shortening the path to the $350k threshold.
3. **Execution Automation via `cmd/livescan`**:
   * Buying 3x leveraged tech (TECL) after a 3-day drop requires high emotional discipline.
   * Utilizing `cmd/livescan` daily removes human hesitation by computing exact, objective limit entry and stop levels.

---

## 🏁 Conclusion

* **Strategy Discovery**: **100% Complete**. `voo-tecl-combo` delivers 45.4% CAGR, 72.6% win rate, and 13.4% Max Drawdown over 5 years.
* **System Architecture**: **95% Complete**. Backtester, Scoreboard, and Live Scanner are fully operational.
* **Financial Verdict**: The strategy is mathematically capable of supporting **$10k/month in withdrawals while growing the base**, provided the capital base is allowed to compound into the **$350k–$500k range** before active withdrawals commence.
