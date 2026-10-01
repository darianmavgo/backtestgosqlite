package simulator

import (
	"math"
	"sort"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/analytics"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

// StrategyPriorityEntry maps a strategy to its execution priority tier.
type StrategyPriorityEntry struct {
	Strategy strategy.Strategy
	Priority int // 0 = primary (highest), 1 = secondary, 2 = tertiary, etc.
	Config   strategy.StrategyConfig
}

// SharedAccountSimulator coordinates multiple strategies trading inside a single shared cash account.
// Only the primary (Priority 0) may preempt: if it needs capital or the same symbol, subordinate
// positions are liquidated at market. Secondaries never evict each other — they size to leftover cash.
// When DefaultAsset is set, leftover cash is held in that symbol. The park lot is separate from
// Positions, so a sleeve can hold the same symbol at the same time.
type SharedAccountSimulator struct {
	InitialCapital      float64
	Cash                float64
	Positions           map[string]*models.Position
	ClosedTrades        []models.Trade
	EquityCurve         []models.DailyEquityPoint
	BenchmarkBars       map[string]models.Bar
	Strategies          []StrategyPriorityEntry
	Configs             map[string]strategy.StrategyConfig
	Sizers              map[string]PositionSizer
	PreemptedTradeCount int
	dailyCashYieldRate  float64
	tradeIDCounter      int

	defaultAsset     string
	park             *parkLot
	parkDividends    map[string]float64
	parkSlippage     float64
	parkCommission   float64
	parkDividendCash float64
	parkLastPrice    float64
	daysUnparked     int
	sumParkWeight    float64
	parkDays         int
}

// parkLot is leftover cash invested in the default asset. It is not a sleeve position.
type parkLot struct {
	Symbol string
	Shares int
}

// DefaultAssetResult summarizes the parked residual book.
type DefaultAssetResult struct {
	Symbol       string
	AvgWeight    float64
	Dividends    float64
	DaysUnparked int
}

// NewSharedAccountSimulator creates a multi-strategy simulator with a shared cash ledger.
func NewSharedAccountSimulator(entries []StrategyPriorityEntry, initialCapital float64) *SharedAccountSimulator {
	if initialCapital <= 0 {
		initialCapital = 100000.0
	}

	configs := make(map[string]strategy.StrategyConfig)
	sizers := make(map[string]PositionSizer)
	var maxCashYield float64

	for _, entry := range entries {
		sID := entry.Strategy.ID()
		cfg := entry.Config
		if cfg.ID == "" {
			cfg = entry.Strategy.DefaultConfig()
		}
		configs[sID] = cfg
		sizers[sID] = GetSizer(cfg)

		if cfg.CashYieldAnnual > maxCashYield {
			maxCashYield = cfg.CashYieldAnnual
		}
	}

	var dailyYield float64
	if maxCashYield > 0 {
		dailyYield = math.Pow(1.0+maxCashYield, 1.0/252.0) - 1.0
	}

	return &SharedAccountSimulator{
		InitialCapital:     initialCapital,
		Cash:               initialCapital,
		Positions:          make(map[string]*models.Position),
		Strategies:         entries,
		Configs:            configs,
		Sizers:             sizers,
		dailyCashYieldRate: dailyYield,
	}
}

// SetBenchmarkBars sets the reference benchmark bars (e.g. VOO or SPY).
func (s *SharedAccountSimulator) SetBenchmarkBars(bars map[string]models.Bar) {
	s.BenchmarkBars = bars
}

// SetDefaultAsset parks leftover cash in symbol. dividends maps an ex-date to
// cash per share. Slippage and commission come from the primary config.
// An empty symbol leaves the cash yield as the only use of leftover cash.
func (s *SharedAccountSimulator) SetDefaultAsset(symbol string, dividends map[string]float64) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return
	}
	s.defaultAsset = symbol
	s.parkDividends = dividends
	for _, entry := range s.Strategies {
		if entry.Priority != 0 {
			continue
		}
		cfg := s.Configs[entry.Strategy.ID()]
		s.parkSlippage = cfg.SlippagePct
		s.parkCommission = cfg.CommissionPerShare
		break
	}
}

// DefaultAssetResult returns the parked-book summary. AvgWeight is zero when
// no default asset was set.
func (s *SharedAccountSimulator) DefaultAssetResult() DefaultAssetResult {
	avg := 0.0
	if s.parkDays > 0 {
		avg = s.sumParkWeight / float64(s.parkDays)
	}
	return DefaultAssetResult{
		Symbol:       s.defaultAsset,
		AvgWeight:    avg,
		Dividends:    s.parkDividendCash,
		DaysUnparked: s.daysUnparked,
	}
}

// Run executes the chronological multi-strategy simulation.
func (s *SharedAccountSimulator) Run(
	signals []models.Signal,
	barsBySymbol map[string][]models.Bar,
	sortedDates []string,
) (models.PerformanceReport, map[string]models.PerformanceReport, []models.Trade, []models.DailyEquityPoint) {
	signals = ApplyLiveEntryModel(signals, barsBySymbol, func(sig models.Signal) strategy.StrategyConfig { return s.Configs[sig.StrategyID] })

	// Index signals by date
	signalsByDate := make(map[string][]models.Signal)
	for _, sig := range signals {
		signalsByDate[sig.Date] = append(signalsByDate[sig.Date], sig)
	}

	// Index bars by symbol and date
	barsBySymbolDate := make(map[string]map[string]models.Bar)
	for sym, bars := range barsBySymbol {
		barsBySymbolDate[sym] = make(map[string]models.Bar)
		for _, b := range bars {
			barsBySymbolDate[sym][b.Date] = b
		}
	}

	peakEquity := s.InitialCapital

	for _, date := range sortedDates {
		// 0. Accrue T-bill yield on residual cash. Parked shares earn the asset, not this yield.
		if s.dailyCashYieldRate > 0 && s.Cash > 0 {
			s.Cash += s.Cash * s.dailyCashYieldRate
		}
		// Dividends are paid on park shares held at the open, then the close sweep reinvests them.
		s.payParkDividend(date)

		// 1. Evaluate open positions for stops / targets / max holding period
		for sym, pos := range s.Positions {
			bar, hasBar := barsBySymbolDate[sym][date]
			if !hasBar {
				continue
			}

			pos.HoldDays++
			pos.CurrentPrice = bar.Close

			if bar.Low < pos.MinLowSince {
				pos.MinLowSince = bar.Low
			}
			if bar.High > pos.MaxHighSince {
				pos.MaxHighSince = bar.High
			}

			cfg := s.Configs[pos.StrategyID]

			// Trailing Stop Trigger
			if cfg.UseTrailingStop && cfg.TrailingStopPct > 0 {
				potentialTrail := pos.MaxHighSince * (1.0 - cfg.TrailingStopPct)
				if potentialTrail > pos.TrailingStopPrice {
					pos.TrailingStopPrice = potentialTrail
				}
			}
			if cfg.UseTrailingStop && pos.TrailingStopPrice > 0 && bar.Low <= pos.TrailingStopPrice {
				exitPrice := pos.TrailingStopPrice * (1.0 - cfg.SlippagePct)
				if bar.Open < pos.TrailingStopPrice {
					exitPrice = bar.Open * (1.0 - cfg.SlippagePct)
				}
				s.closePosition(sym, date, exitPrice, models.ExitReasonTrailingStop)
				continue
			}

			// ATR Stop Trigger
			if cfg.UseATRStop && pos.ATRStopPrice > 0 && bar.Low <= pos.ATRStopPrice {
				exitPrice := pos.ATRStopPrice * (1.0 - cfg.SlippagePct)
				if bar.Open < pos.ATRStopPrice {
					exitPrice = bar.Open * (1.0 - cfg.SlippagePct)
				}
				s.closePosition(sym, date, exitPrice, models.ExitReasonATRStop)
				continue
			}

			// Stop-Loss Trigger
			if pos.StopLossPrice > 0 && bar.Low <= pos.StopLossPrice {
				exitPrice := pos.StopLossPrice * (1.0 - cfg.SlippagePct)
				if bar.Open < pos.StopLossPrice {
					exitPrice = bar.Open * (1.0 - cfg.SlippagePct)
				}
				s.closePosition(sym, date, exitPrice, models.ExitReasonStopLoss)
				continue
			}

			// Profit-Target Trigger
			if pos.TargetPrice > 0 && bar.High >= pos.TargetPrice {
				exitPrice := pos.TargetPrice * (1.0 - cfg.SlippagePct)
				if bar.Open > pos.TargetPrice {
					exitPrice = bar.Open * (1.0 - cfg.SlippagePct)
				}
				s.closePosition(sym, date, exitPrice, models.ExitReasonProfitTarget)
				continue
			}

			// Max Holding Days Exceeded (Time-Up Market Exit)
			effectiveHoldDays := cfg.HoldingWindow
			if pos.HoldDaysOverride > 0 {
				effectiveHoldDays = pos.HoldDaysOverride
			}
			if effectiveHoldDays > 0 && pos.HoldDays >= effectiveHoldDays {
				exitPrice := bar.Close * (1.0 - cfg.SlippagePct)
				if cfg.ExitAtMarketOpen {
					exitPrice = bar.Open * (1.0 - cfg.SlippagePct)
				}
				s.closePosition(sym, date, exitPrice, models.ExitReasonTimeUp)
				continue
			}
		}

		// 2. Process new entry signals on current date
		if daySignals, hasSignals := signalsByDate[date]; hasSignals {
			// Sort day signals by Priority ascending (Primary = 0 first, then Secondary = 1...)
			// Within same priority, sort alphabetically by symbol
			sort.Slice(daySignals, func(i, j int) bool {
				iExit := daySignals[i].Entry < 0
				jExit := daySignals[j].Entry < 0
				if iExit != jExit {
					return iExit
				}
				if daySignals[i].Priority != daySignals[j].Priority {
					return daySignals[i].Priority < daySignals[j].Priority
				}
				return daySignals[i].Symbol < daySignals[j].Symbol
			})

			for _, sig := range daySignals {
				cfg, ok := s.Configs[sig.StrategyID]
				if !ok {
					continue
				}
				// Explicit exit (biggest-winner year-end). The shared ledger
				// is long-only, so a SHORT entry is not opened here.
				if sig.Entry < 0 {
					if _, held := s.Positions[sig.Symbol]; held {
						raw := sig.Close
						if bar, ok := barsBySymbolDate[sig.Symbol][date]; ok && bar.Close > 0 {
							raw = bar.Close
						}
						if raw > 0 {
							s.closePosition(sig.Symbol, date, raw*(1.0-cfg.SlippagePct), models.ExitReasonSignal)
						}
					}
					continue
				}
				if strings.EqualFold(sig.Direction, "SHORT") {
					continue
				}

				// Check per-strategy position cap
				stratCount := 0
				for _, p := range s.Positions {
					if p.StrategyID == sig.StrategyID {
						stratCount++
					}
				}
				if cfg.PositionCap > 0 && stratCount >= cfg.PositionCap {
					continue
				}

				// Check if symbol is already held. Only the primary may kick a
				// subordinate off a symbol; overlays skip if anything holds it.
				if existingPos, alreadyHeld := s.Positions[sig.Symbol]; alreadyHeld {
					if sig.Priority == 0 && existingPos.Priority > 0 {
						bar := barsBySymbolDate[sig.Symbol][date]
						exitPrice := bar.Close * (1.0 - cfg.SlippagePct)
						s.closePosition(sig.Symbol, date, exitPrice, models.ExitReasonPreempted)
						s.PreemptedTradeCount++
					} else {
						continue
					}
				}

				totalEquity := s.calculateTotalEquity(barsBySymbolDate, date)

				// Determine entry price
				entryPrice := sig.Close * (1.0 + cfg.SlippagePct)
				orderType := strings.ToLower(sig.OrderType)
				if orderType == "" {
					orderType = "limit"
				}
				if orderType == "market" {
					entryPrice = sig.Close * (1.0 + cfg.SlippagePct)
				} else if orderType == "limit" && sig.BuyLimit > 0 {
					entryPrice = sig.BuyLimit * (1.0 + cfg.SlippagePct)
				}
				if entryPrice <= 0 {
					continue
				}

				sizer := s.Sizers[sig.StrategyID]

				var shares int
				var requiredCapital float64

				if sig.Priority == 0 {
					// Primary sizes against full equity, then sells the park
					// and, if still short, liquidates subordinate positions.
					shares = sizer.CalculateShares(totalEquity, totalEquity, entryPrice, cfg, sig)
					if shares <= 0 {
						continue
					}
					cost := float64(shares) * entryPrice
					commission := float64(shares) * cfg.CommissionPerShare
					requiredCapital = cost + commission

					if px, ok := s.parkClose(barsBySymbolDate, date); ok {
						s.raiseCashFromPark(requiredCapital, px)
					}
					if requiredCapital > s.Cash {
						var subordinates []*models.Position
						for _, p := range s.Positions {
							if p.Priority > 0 {
								subordinates = append(subordinates, p)
							}
						}
						sort.Slice(subordinates, func(i, j int) bool {
							return subordinates[i].HoldDays > subordinates[j].HoldDays
						})

						for _, lp := range subordinates {
							bar, hasBar := barsBySymbolDate[lp.Symbol][date]
							exitPrice := lp.CurrentPrice * (1.0 - cfg.SlippagePct)
							if hasBar {
								exitPrice = bar.Close * (1.0 - cfg.SlippagePct)
							}
							s.closePosition(lp.Symbol, date, exitPrice, models.ExitReasonPreempted)
							s.PreemptedTradeCount++

							if s.Cash >= requiredCapital {
								break
							}
						}

						if s.Cash < requiredCapital {
							availShares := int(s.Cash / (entryPrice + cfg.CommissionPerShare))
							if availShares > 0 {
								shares = availShares
								cost = float64(shares) * entryPrice
								commission = float64(shares) * cfg.CommissionPerShare
								requiredCapital = cost + commission
							} else {
								continue // Cannot afford even 1 share
							}
						}
					}
				} else {
					// Subordinates never preempt. They trade leftover cash only.
					// Parked shares are that leftover cash: sell just enough
					// of the park to fund the order. A full 10% slot can miss
					// by a few cents of commission; shrink to the shares cash
					// can afford instead of skipping the position.
					budget := s.Cash
					if px, ok := s.parkClose(barsBySymbolDate, date); ok {
						budget += s.parkBuyingPower(px)
					}
					shares = sizer.CalculateShares(budget, totalEquity, entryPrice, cfg, sig)
					if shares <= 0 {
						continue
					}
					cost := float64(shares) * entryPrice
					commission := float64(shares) * cfg.CommissionPerShare
					requiredCapital = cost + commission
					if px, ok := s.parkClose(barsBySymbolDate, date); ok {
						s.raiseCashFromPark(requiredCapital, px)
					}
					if requiredCapital > s.Cash {
						shares = int(s.Cash / (entryPrice + cfg.CommissionPerShare))
						if shares <= 0 {
							continue
						}
						cost = float64(shares) * entryPrice
						commission = float64(shares) * cfg.CommissionPerShare
						requiredCapital = cost + commission
						if requiredCapital > s.Cash {
							continue
						}
					}
				}

				// Calculate target and stop prices
				targetPrice := 0.0
				if cfg.TargetPct > 1.0 {
					targetPrice = entryPrice * cfg.TargetPct
				} else if cfg.TakeProfitPct > 0 {
					targetPrice = entryPrice * (1.0 + cfg.TakeProfitPct)
				}
				if sig.TakeProfit > 0 {
					targetPrice = sig.TakeProfit
				}

				stopLossPrice := 0.0
				if cfg.StopLossPct > 0 {
					if cfg.StopLossPct < 1.0 {
						stopLossPrice = entryPrice * cfg.StopLossPct
					} else {
						stopLossPrice = entryPrice * (1.0 - cfg.StopLossPct)
					}
				}
				if sig.StopLoss > 0 {
					stopLossPrice = sig.StopLoss
				}

				var atrStopPrice float64
				if cfg.UseATRStop && cfg.ATRStopMultiplier > 0 {
					if atrVal, ok := sig.Metadata["atr"]; ok && atrVal > 0 {
						atrStopPrice = entryPrice - cfg.ATRStopMultiplier*atrVal
					}
				}

				var trailingStopPrice float64
				if cfg.UseTrailingStop && cfg.TrailingStopPct > 0 {
					trailingStopPrice = entryPrice * (1.0 - cfg.TrailingStopPct)
				}

				s.Cash -= requiredCapital
				s.Positions[sig.Symbol] = &models.Position{
					StrategyID:        sig.StrategyID,
					Priority:          sig.Priority,
					Symbol:            sig.Symbol,
					Shares:            shares,
					OrderType:         orderType,
					EntryPrice:        entryPrice,
					EntryDate:         date,
					CurrentPrice:      entryPrice,
					TargetPrice:       targetPrice,
					StopLossPrice:     stopLossPrice,
					TrailingStopPrice: trailingStopPrice,
					ATRStopPrice:      atrStopPrice,
					HoldDays:          0,
					MinLowSince:       entryPrice,
					MaxHighSince:      entryPrice,
					HoldDaysOverride:  sig.HoldDaysOverride,
				}
			}
		}

		// 2b. Buy the default asset with cash that cannot fund another sleeve share.
		if px, ok := s.parkClose(barsBySymbolDate, date); ok {
			s.sweepIntoPark(px)
		}

		// 3. Mark-to-market end-of-day portfolio valuation
		totalEquity := s.calculateTotalEquity(barsBySymbolDate, date)
		s.recordParkDay(barsBySymbolDate, date, totalEquity)
		if totalEquity > peakEquity {
			peakEquity = totalEquity
		}
		drawdownPct := 0.0
		if peakEquity > 0 {
			drawdownPct = (peakEquity - totalEquity) / peakEquity
		}

		dailyReturn := 0.0
		if len(s.EquityCurve) > 0 {
			prevEq := s.EquityCurve[len(s.EquityCurve)-1].TotalEquity
			if prevEq > 0 {
				dailyReturn = (totalEquity - prevEq) / prevEq
			}
		}

		s.EquityCurve = append(s.EquityCurve, models.DailyEquityPoint{
			Date:           date,
			Cash:           s.Cash,
			PositionsValue: totalEquity - s.Cash,
			TotalEquity:    totalEquity,
			OpenPositions:  s.openCount(),
			DailyReturn:    dailyReturn,
			DrawdownPct:    drawdownPct,
		})
	}

	// 4. Force-close remaining open positions at final date.
	// The park is turned back into cash here and is not a ClosedTrade, so
	// sleeve trade counts stay comparable to a cash run.
	if len(sortedDates) > 0 {
		lastDate := sortedDates[len(sortedDates)-1]
		s.liquidatePark(barsBySymbolDate, lastDate)
		for sym, pos := range s.Positions {
			cfg := s.Configs[pos.StrategyID]
			if bar, ok := barsBySymbolDate[sym][lastDate]; ok {
				s.closePosition(sym, lastDate, bar.Close*(1.0-cfg.SlippagePct), models.ExitReasonEndBacktest)
			}
		}
	}

	// 5. Calculate combined performance report
	combinedReport := analytics.CalculatePerformanceMetricsWithBenchmark(
		s.InitialCapital, s.ClosedTrades, s.EquityCurve, s.BenchmarkBars,
	)

	// 6. Calculate per-strategy performance reports
	perStrategyReports := make(map[string]models.PerformanceReport)
	for _, entry := range s.Strategies {
		sID := entry.Strategy.ID()
		var stratTrades []models.Trade
		var stratNetPnL float64
		tradePnLByDate := make(map[string]float64)
		for _, t := range s.ClosedTrades {
			if t.StrategyID == sID {
				stratTrades = append(stratTrades, t)
				stratNetPnL += t.NetPnL
				tradePnLByDate[t.ExitDate] += t.NetPnL
			}
		}

		// Reconstruct an accurate strategy-specific equity curve from its trade PnL history
		var stratCurve []models.DailyEquityPoint
		cumPnL := 0.0
		peakStratEq := s.InitialCapital
		for _, pt := range s.EquityCurve {
			if pnl, ok := tradePnLByDate[pt.Date]; ok {
				cumPnL += pnl
			}
			currEq := math.Max(0.0, s.InitialCapital+cumPnL)
			if currEq > peakStratEq {
				peakStratEq = currEq
			}
			dd := 0.0
			if peakStratEq > 0 {
				dd = (peakStratEq - currEq) / peakStratEq
			}
			stratCurve = append(stratCurve, models.DailyEquityPoint{
				Date:        pt.Date,
				TotalEquity: currEq,
				DrawdownPct: dd,
			})
		}

		stratReport := analytics.CalculatePerformanceMetricsWithBenchmark(
			s.InitialCapital, stratTrades, stratCurve, s.BenchmarkBars,
		)
		stratReport.NetProfit = stratNetPnL
		stratReport.FinalEquity = s.InitialCapital + stratNetPnL
		if s.InitialCapital > 0 {
			stratReport.TotalReturnPct = stratNetPnL / s.InitialCapital
		}
		years := stratReport.TotalCalendarYears
		if years >= 0.5 && stratReport.FinalEquity > 0 && stratReport.InitialCapital > 0 {
			stratReport.CAGR = math.Pow(stratReport.FinalEquity/stratReport.InitialCapital, 1.0/years) - 1.0
		}
		perStrategyReports[sID] = stratReport
	}

	return combinedReport, perStrategyReports, s.ClosedTrades, s.EquityCurve
}

func (s *SharedAccountSimulator) closePosition(symbol, date string, exitPrice float64, reason models.ExitReason) {
	pos, ok := s.Positions[symbol]
	if !ok {
		return
	}

	cfg := s.Configs[pos.StrategyID]

	s.tradeIDCounter++
	grossProceeds := float64(pos.Shares) * exitPrice
	commission := float64(pos.Shares) * cfg.CommissionPerShare
	netProceeds := grossProceeds - commission
	netPnL := netProceeds - (float64(pos.Shares) * pos.EntryPrice)
	returnPct := 0.0
	if pos.EntryPrice > 0 {
		returnPct = (exitPrice - pos.EntryPrice) / pos.EntryPrice
	}

	s.Cash += netProceeds

	mae := 0.0
	if pos.EntryPrice > 0 {
		mae = (pos.MinLowSince - pos.EntryPrice) / pos.EntryPrice
	}
	mfe := 0.0
	if pos.EntryPrice > 0 {
		mfe = (pos.MaxHighSince - pos.EntryPrice) / pos.EntryPrice
	}

	trade := models.Trade{
		ID:                    s.tradeIDCounter,
		StrategyID:            pos.StrategyID,
		Symbol:                symbol,
		OrderType:             pos.OrderType,
		EntryDate:             pos.EntryDate,
		EntryPrice:            pos.EntryPrice,
		TargetPrice:           pos.TargetPrice,
		StopLossPrice:         pos.StopLossPrice,
		ExitDate:              date,
		ExitPrice:             exitPrice,
		ExitReason:            reason,
		Shares:                pos.Shares,
		HoldDays:              pos.HoldDays,
		InvestedCapital:       float64(pos.Shares) * pos.EntryPrice,
		GrossPnL:              grossProceeds - (float64(pos.Shares) * pos.EntryPrice),
		NetPnL:                netPnL,
		ReturnPct:             returnPct,
		CommissionPaid:        commission * 2,
		MaxAdverseExcursion:   mae,
		MaxFavorableExcursion: mfe,
	}

	s.ClosedTrades = append(s.ClosedTrades, trade)
	delete(s.Positions, symbol)
}

func (s *SharedAccountSimulator) calculateTotalEquity(barsBySymbolDate map[string]map[string]models.Bar, date string) float64 {
	equity := s.Cash
	for sym, pos := range s.Positions {
		if bar, ok := barsBySymbolDate[sym][date]; ok {
			equity += float64(pos.Shares) * bar.Close
		} else {
			equity += float64(pos.Shares) * pos.CurrentPrice
		}
	}
	if s.park != nil && s.park.Shares > 0 {
		if bar, ok := barsBySymbolDate[s.defaultAsset][date]; ok && bar.Close > 0 {
			s.parkLastPrice = bar.Close
			equity += float64(s.park.Shares) * bar.Close
		} else if s.parkLastPrice > 0 {
			equity += float64(s.park.Shares) * s.parkLastPrice
		}
	}
	return math.Max(0.0, equity)
}

func (s *SharedAccountSimulator) openCount() int {
	n := len(s.Positions)
	if s.park != nil && s.park.Shares > 0 {
		n++
	}
	return n
}

func (s *SharedAccountSimulator) parkClose(bars map[string]map[string]models.Bar, date string) (float64, bool) {
	if s.defaultAsset == "" {
		return 0, false
	}
	bar, ok := bars[s.defaultAsset][date]
	if !ok || bar.Close <= 0 {
		return 0, false
	}
	return bar.Close, true
}

func (s *SharedAccountSimulator) parkBuyingPower(close float64) float64 {
	if s.park == nil || s.park.Shares <= 0 || close <= 0 {
		return 0
	}
	perShare := close*(1.0-s.parkSlippage) - s.parkCommission
	if perShare <= 0 {
		return 0
	}
	return float64(s.park.Shares) * perShare
}

func (s *SharedAccountSimulator) raiseCashFromPark(need, close float64) {
	if s.park == nil || s.park.Shares <= 0 || close <= 0 || need <= s.Cash {
		return
	}
	perShare := close*(1.0-s.parkSlippage) - s.parkCommission
	if perShare <= 0 {
		return
	}
	shares := int(math.Ceil((need - s.Cash) / perShare))
	s.sellParkShares(shares, close)
}

func (s *SharedAccountSimulator) sellParkShares(shares int, close float64) {
	if s.park == nil || shares <= 0 || close <= 0 {
		return
	}
	if shares > s.park.Shares {
		shares = s.park.Shares
	}
	exitPx := close * (1.0 - s.parkSlippage)
	proceeds := float64(shares)*exitPx - float64(shares)*s.parkCommission
	s.Cash += proceeds
	s.park.Shares -= shares
}

func (s *SharedAccountSimulator) sweepIntoPark(close float64) {
	if s.defaultAsset == "" || close <= 0 {
		return
	}
	unit := close*(1.0+s.parkSlippage) + s.parkCommission
	if unit <= 0 {
		return
	}
	shares := int(s.Cash / unit)
	if shares <= 0 {
		return
	}
	s.Cash -= float64(shares) * unit
	if s.park == nil {
		s.park = &parkLot{Symbol: s.defaultAsset}
	}
	s.park.Shares += shares
	s.parkLastPrice = close
}

func (s *SharedAccountSimulator) payParkDividend(date string) {
	if s.park == nil || s.park.Shares <= 0 || len(s.parkDividends) == 0 {
		return
	}
	perShare := s.parkDividends[date]
	if perShare <= 0 {
		return
	}
	pay := float64(s.park.Shares) * perShare
	s.Cash += pay
	s.parkDividendCash += pay
}

func (s *SharedAccountSimulator) liquidatePark(bars map[string]map[string]models.Bar, date string) {
	if s.park == nil || s.park.Shares <= 0 {
		return
	}
	px := s.parkLastPrice
	if bar, ok := bars[s.defaultAsset][date]; ok && bar.Close > 0 {
		px = bar.Close
	}
	s.sellParkShares(s.park.Shares, px)
}

func (s *SharedAccountSimulator) recordParkDay(bars map[string]map[string]models.Bar, date string, totalEquity float64) {
	if s.defaultAsset == "" {
		return
	}
	s.parkDays++
	var marketValue float64
	if px, ok := s.parkClose(bars, date); ok {
		s.parkLastPrice = px
		if s.park != nil {
			marketValue = float64(s.park.Shares) * px
		}
	} else if s.park != nil && s.parkLastPrice > 0 {
		marketValue = float64(s.park.Shares) * s.parkLastPrice
	}
	if totalEquity > 0 {
		s.sumParkWeight += marketValue / totalEquity
	}
	if s.park == nil || s.park.Shares == 0 {
		s.daysUnparked++
	}
}
