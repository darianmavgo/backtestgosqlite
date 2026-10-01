package simulator

import (
	"math"
	"sort"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/pkg/analytics"
	"github.com/darianmavgo/backtestgosqlite/pkg/models"
	"github.com/darianmavgo/backtestgosqlite/pkg/strategy"
)

// PortfolioSimulator runs a chronological multi-asset event simulation with capital constraints.
type PortfolioSimulator struct {
	Config         strategy.StrategyConfig
	InitialCapital float64
	Cash           float64
	Positions      map[string]*models.Position
	ClosedTrades   []models.Trade
	EquityCurve    []models.DailyEquityPoint
	BenchmarkBars  map[string]models.Bar
	// Dividends optionally maps symbol → date → cash dividend per share. Held
	// shares are paid on that date into Cash (not reinvested). Used with raw
	// price bars to model "take dividends as cash".
	Dividends            map[string]map[string]float64
	DividendCash         float64 // total dividends received during Run
	Sizer                PositionSizer
	tradeIDCounter       int
	dailyCashYieldRate   float64 // pre-computed daily compound factor from CashYieldAnnual
	dailyMarginRate      float64 // pre-computed daily compound factor from MarginInterestAnnual
	dailyShortBorrowRate float64 // pre-computed daily compound factor from ShortBorrowAnnual
	TotalMarginInterest  float64 // cumulative margin interest debited
	// allowNegativeEquity is set once a short is opened. A short can lose more
	// than its collateral; flooring equity at zero would hide that.
	allowNegativeEquity bool
	lastExitDay         map[string]int
}

// NewPortfolioSimulator initializes a simulator instance.
func NewPortfolioSimulator(config strategy.StrategyConfig, initialCapital float64) *PortfolioSimulator {
	if initialCapital <= 0 {
		initialCapital = 100000.0 // Default $100k account
	}

	// Note: We no longer enforce arbitrary default fallbacks for TargetPct and StopLossPct.
	// If a strategy wants these values, it must explicitly configure them.

	// Pre-compute the daily compounding factor for T-bill yield accrual.
	// Uses 252 trading days per year: dailyRate = (1 + annual)^(1/252) - 1
	var dailyCashYieldRate float64
	if config.CashYieldAnnual > 0 {
		dailyCashYieldRate = math.Pow(1.0+config.CashYieldAnnual, 1.0/252.0) - 1.0
	}

	var dailyMarginRate float64
	if config.MarginInterestAnnual > 0 {
		dailyMarginRate = math.Pow(1.0+config.MarginInterestAnnual, 1.0/252.0) - 1.0
	}

	var dailyShortBorrowRate float64
	if config.ShortBorrowAnnual > 0 {
		dailyShortBorrowRate = math.Pow(1.0+config.ShortBorrowAnnual, 1.0/252.0) - 1.0
	}

	return &PortfolioSimulator{
		Config:               config,
		InitialCapital:       initialCapital,
		Cash:                 initialCapital,
		Positions:            make(map[string]*models.Position),
		Sizer:                GetSizer(config),
		dailyCashYieldRate:   dailyCashYieldRate,
		dailyMarginRate:      dailyMarginRate,
		dailyShortBorrowRate: dailyShortBorrowRate,
		lastExitDay:          make(map[string]int),
	}
}

// SetBenchmarkBars sets historical bars for the benchmark asset (e.g. SPY).
func (s *PortfolioSimulator) SetBenchmarkBars(bars map[string]models.Bar) {
	s.BenchmarkBars = bars
}

// Run executes the day-by-day chronological portfolio simulation across the market timeline.
func (s *PortfolioSimulator) Run(
	signals []models.Signal,
	barsBySymbol map[string][]models.Bar,
	sortedDates []string,
) (models.PerformanceReport, []models.Trade, []models.DailyEquityPoint) {
	signals = ApplyLiveEntryModel(signals, barsBySymbol, func(models.Signal) strategy.StrategyConfig { return s.Config })

	// Index signals by date for O(1) daily lookup
	signalsByDate := make(map[string][]models.Signal)
	for _, sig := range signals {
		signalsByDate[sig.Date] = append(signalsByDate[sig.Date], sig)
	}

	// Index bars by symbol and date for O(1) price checks
	barsBySymbolDate := make(map[string]map[string]models.Bar)
	for sym, bars := range barsBySymbol {
		barsBySymbolDate[sym] = make(map[string]models.Bar)
		for _, b := range bars {
			barsBySymbolDate[sym][b.Date] = b
		}
	}

	peakEquity := s.InitialCapital

	for currentDayIdx, date := range sortedDates {
		var todayDiv float64
		var todayMarginInterest float64

		// 0. Accrue T-bill yield on uninvested cash OR debit margin interest on borrowed funds
		if s.dailyCashYieldRate > 0 && s.Cash > 0 {
			s.Cash += s.Cash * s.dailyCashYieldRate
		} else if s.dailyMarginRate > 0 && s.Cash < 0 {
			// Cash is negative (margin loan debit balance) -> interest leaves cash
			todayMarginInterest = math.Abs(s.Cash) * s.dailyMarginRate
			s.Cash -= todayMarginInterest
			s.TotalMarginInterest += todayMarginInterest
		}

		// 0b. Pay dividends on shares held going into the ex-date.
		// A short owes the dividend.
		for sym, pos := range s.Positions {
			if d := s.Dividends[sym][date]; d > 0 {
				pay := float64(pos.Shares) * d
				if pos.Direction == "SHORT" {
					pay = -pay
				}
				s.Cash += pay
				s.DividendCash += pay
				todayDiv += pay
			}
		}

		// 1. Evaluate and update existing open positions
		for sym, pos := range s.Positions {
			bar, hasBar := barsBySymbolDate[sym][date]
			if !hasBar {
				continue
			}

			pos.HoldDays++
			pos.CurrentPrice = bar.Close

			if pos.Direction == "SHORT" && s.dailyShortBorrowRate > 0 {
				s.Cash -= float64(pos.Shares) * bar.Close * s.dailyShortBorrowRate
			}

			if bar.Low < pos.MinLowSince {
				pos.MinLowSince = bar.Low
			}
			if bar.High > pos.MaxHighSince {
				pos.MaxHighSince = bar.High
			}

			// Update trailing stop if enabled
			if s.Config.UseTrailingStop && s.Config.TrailingStopPct > 0 {
				potentialTrail := pos.MaxHighSince * (1.0 - s.Config.TrailingStopPct)
				if potentialTrail > pos.TrailingStopPrice {
					pos.TrailingStopPrice = potentialTrail
				}
			}

			// A. Trailing-Stop Trigger
			if s.Config.UseTrailingStop && pos.TrailingStopPrice > 0 && bar.Low <= pos.TrailingStopPrice {
				exitPrice := pos.TrailingStopPrice * (1.0 - s.Config.SlippagePct)
				if bar.Open < pos.TrailingStopPrice {
					exitPrice = bar.Open * (1.0 - s.Config.SlippagePct)
				}
				s.closePosition(sym, date, exitPrice, models.ExitReasonTrailingStop, currentDayIdx)
				continue
			}

			// B. ATR-Stop Trigger
			if s.Config.UseATRStop && pos.ATRStopPrice > 0 && bar.Low <= pos.ATRStopPrice {
				exitPrice := pos.ATRStopPrice * (1.0 - s.Config.SlippagePct)
				if bar.Open < pos.ATRStopPrice {
					exitPrice = bar.Open * (1.0 - s.Config.SlippagePct)
				}
				s.closePosition(sym, date, exitPrice, models.ExitReasonATRStop, currentDayIdx)
				continue
			}

			// C. Fixed Stop-Loss Trigger (Conservative Check: Low <= StopLossPrice)
			if bar.Low <= pos.StopLossPrice {
				exitPrice := pos.StopLossPrice * (1.0 - s.Config.SlippagePct)
				if bar.Open < pos.StopLossPrice {
					exitPrice = bar.Open * (1.0 - s.Config.SlippagePct)
				}
				s.closePosition(sym, date, exitPrice, models.ExitReasonStopLoss, currentDayIdx)
				continue
			}

			// D. Profit-Target Trigger (High >= TargetPrice)
			if pos.TargetPrice > 0 && bar.High >= pos.TargetPrice {
				exitPrice := pos.TargetPrice * (1.0 - s.Config.SlippagePct)
				if bar.Open > pos.TargetPrice {
					exitPrice = bar.Open * (1.0 - s.Config.SlippagePct)
				}
				s.closePosition(sym, date, exitPrice, models.ExitReasonProfitTarget, currentDayIdx)
				continue
			}

			// E. Max Holding Days Exceeded (Time-Up Market Exit)
			// Respect per-signal HoldDaysOverride if set, else fall back to config.
			effectiveHoldDays := s.Config.HoldingWindow
			if pos.HoldDaysOverride > 0 {
				effectiveHoldDays = pos.HoldDaysOverride
			}
			if pos.HoldDays >= effectiveHoldDays {
				raw := bar.Close
				if s.Config.ExitAtMarketOpen {
					raw = bar.Open
				}
				s.closePosition(sym, date, slipped(raw, s.Config.SlippagePct, pos.Direction == "SHORT"), models.ExitReasonTimeUp, currentDayIdx)
				continue
			}
		}

		// 2. Process new entry signals on current date
		if daySignals, hasSignals := signalsByDate[date]; hasSignals {
			sort.Slice(daySignals, func(i, j int) bool {
				iExit := daySignals[i].Entry < 0
				jExit := daySignals[j].Entry < 0
				if iExit != jExit {
					return iExit
				}
				return daySignals[i].Symbol < daySignals[j].Symbol
			})

			for _, sig := range daySignals {
				if sig.Entry < 0 {
					if pos, held := s.Positions[sig.Symbol]; held {
						raw := sig.Close
						if bar, ok := barsBySymbolDate[sig.Symbol][date]; ok && bar.Close > 0 {
							raw = bar.Close
						}
						if raw > 0 {
							s.closePosition(sig.Symbol, date, slipped(raw, s.Config.SlippagePct, pos.Direction == "SHORT"), models.ExitReasonSignal, currentDayIdx)
						}
					}
					continue
				}
				if len(s.Positions) >= s.Config.PositionCap {
					break // Max positions reached
				}
				if _, alreadyHeld := s.Positions[sig.Symbol]; alreadyHeld {
					continue // Already holding this symbol
				}
				if s.Config.ReentryCooldownDays > 0 {
					if exitDay, exited := s.lastExitDay[sig.Symbol]; exited {
						if currentDayIdx-exitDay <= s.Config.ReentryCooldownDays {
							continue // Cooldown period active after exit
						}
					}
				}

				totalEquity := s.calculateTotalEquity(barsBySymbolDate, date)

				// Longs pay the offer (slippage up). Shorts sell the bid
				// (slippage down). Both are adverse.
				isShort := strings.EqualFold(sig.Direction, "SHORT")
				entryPrice := slipped(sig.Close, s.Config.SlippagePct, !isShort)
				orderType := strings.ToLower(sig.OrderType)
				if orderType == "" {
					orderType = "limit"
				}

				if orderType == "market" {
					entryPrice = slipped(sig.Close, s.Config.SlippagePct, !isShort)
				} else if orderType == "limit" && sig.BuyLimit > 0 {
					entryPrice = slipped(sig.BuyLimit, s.Config.SlippagePct, !isShort)
				}

				if entryPrice <= 0 {
					continue
				}

				// Available buying power
				availBP := s.Cash
				if s.Config.UseMargin {
					lev := s.Config.MarginLeverage
					if lev <= 0 {
						lev = 2.0
					}
					// Buying power = Equity * Leverage - PositionValue
					// If all cash, BP = Cash * Leverage.
					posVal := totalEquity - s.Cash
					availBP = math.Max(0.0, totalEquity*lev-posVal)
				}

				shares := s.Sizer.CalculateShares(availBP, totalEquity, entryPrice, s.Config)
				if shares <= 0 {
					continue
				}

				cost := float64(shares) * entryPrice
				commission := float64(shares) * s.Config.CommissionPerShare

				if s.Config.UseMargin {
					if cost+commission > availBP {
						continue
					}
				} else {
					if cost+commission > s.Cash {
						continue
					}
				}

				// Mirrors shared_account.go's identical fallback: TargetPct (legacy
				// multiplier, e.g. 1.18) wins when set, else TakeProfitPct (preferred
				// fractional offset, e.g. 0.08) is converted to a multiplier. Without
				// the TakeProfitPct branch, a strategy that only sets TakeProfitPct
				// (no TargetPct) would silently get an entryPrice*0 target — an
				// immediately-hit $0 take-profit — whenever a signal didn't carry its
				// own override (gld-decline, voo-tecl-combo and voo-tecl-spxu-combo all
				// used to rely entirely on a per-signal override for exactly this
				// reason).
				targetPrice := 0.0
				if s.Config.TargetPct > 1.0 {
					targetPrice = entryPrice * s.Config.TargetPct
				} else if s.Config.TakeProfitPct > 0 {
					targetPrice = entryPrice * (1.0 + s.Config.TakeProfitPct)
				}
				if sig.TakeProfit > 0 {
					targetPrice = sig.TakeProfit
				}

				// StopLossPct is always a direct multiplier (e.g. 0.93 for -7%), not an
				// offset — <1.0 is the normal case; >=1.0 is legacy input tolerance also
				// present in shared_account.go.
				stopLossPrice := 0.0
				if s.Config.StopLossPct > 0 {
					if s.Config.StopLossPct < 1.0 {
						stopLossPrice = entryPrice * s.Config.StopLossPct
					} else {
						stopLossPrice = entryPrice * (1.0 - s.Config.StopLossPct)
					}
				}
				if sig.StopLoss > 0 {
					stopLossPrice = sig.StopLoss
				}

				var atrStopPrice float64
				if s.Config.UseATRStop && s.Config.ATRStopMultiplier > 0 {
					if atrVal, ok := sig.Metadata["atr"]; ok && atrVal > 0 {
						atrStopPrice = entryPrice - s.Config.ATRStopMultiplier*atrVal
					}
				}

				var trailingStopPrice float64
				if s.Config.UseTrailingStop && s.Config.TrailingStopPct > 0 {
					trailingStopPrice = entryPrice * (1.0 - s.Config.TrailingStopPct)
				}

				s.Cash -= (cost + commission)
				direction := ""
				if isShort {
					direction = "SHORT"
					s.allowNegativeEquity = true
				}
				s.Positions[sig.Symbol] = &models.Position{
					Symbol:            sig.Symbol,
					Shares:            shares,
					Direction:         direction,
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

		// 3. Calculate and record end-of-day equity
		totalEquity := s.calculateTotalEquity(barsBySymbolDate, date)
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

		posVal := totalEquity - s.Cash
		bp := s.Cash
		debt := 0.0
		if s.Cash < 0 {
			debt = -s.Cash
		}
		if s.Config.UseMargin {
			lev := s.Config.MarginLeverage
			if lev <= 0 {
				lev = 2.0
			}
			bp = math.Max(0.0, totalEquity*lev-posVal)
		}

		s.EquityCurve = append(s.EquityCurve, models.DailyEquityPoint{
			Date:           date,
			Cash:           s.Cash,
			PositionsValue: posVal,
			TotalEquity:    totalEquity,
			OpenPositions:  len(s.Positions),
			DailyReturn:    dailyReturn,
			DrawdownPct:    drawdownPct,
			BuyingPower:    bp,
			MarginDebt:     debt,
			MarginInterest: todayMarginInterest,
			DividendIncome: todayDiv,
		})
	}

	// 4. Force-close any open positions at end of backtest timeline
	if len(sortedDates) > 0 {
		lastDate := sortedDates[len(sortedDates)-1]
		for sym, pos := range s.Positions {
			if bar, ok := barsBySymbolDate[sym][lastDate]; ok {
				s.closePosition(sym, lastDate, slipped(bar.Close, s.Config.SlippagePct, pos.Direction == "SHORT"), models.ExitReasonEndBacktest, len(sortedDates)-1)
			}
		}
	}

	// 5. Compute institutional performance metrics
	report := analytics.CalculatePerformanceMetricsWithBenchmark(s.InitialCapital, s.ClosedTrades, s.EquityCurve, s.BenchmarkBars)

	return report, s.ClosedTrades, s.EquityCurve
}

func (s *PortfolioSimulator) closePosition(symbol, date string, exitPrice float64, reason models.ExitReason, dayIdx int) {
	pos, ok := s.Positions[symbol]
	if !ok {
		return
	}

	s.tradeIDCounter++
	commission := float64(pos.Shares) * s.Config.CommissionPerShare
	var netProceeds, netPnL, returnPct, mae, mfe float64
	if pos.Direction == "SHORT" {
		// Collateral of shares*entry was locked at the open. Returning it
		// plus (entry-exit) is shares*(2*entry-exit), then the cover commission.
		netProceeds = float64(pos.Shares)*(2*pos.EntryPrice-exitPrice) - commission
		netPnL = float64(pos.Shares)*(pos.EntryPrice-exitPrice) - commission
		if pos.EntryPrice > 0 {
			returnPct = (pos.EntryPrice - exitPrice) / pos.EntryPrice
			mae = (pos.EntryPrice - pos.MaxHighSince) / pos.EntryPrice
			mfe = (pos.EntryPrice - pos.MinLowSince) / pos.EntryPrice
		}
	} else {
		grossProceeds := float64(pos.Shares) * exitPrice
		netProceeds = grossProceeds - commission
		netPnL = netProceeds - (float64(pos.Shares) * pos.EntryPrice)
		returnPct = (exitPrice - pos.EntryPrice) / pos.EntryPrice
		if pos.EntryPrice > 0 {
			mae = (pos.MinLowSince - pos.EntryPrice) / pos.EntryPrice
			mfe = (pos.MaxHighSince - pos.EntryPrice) / pos.EntryPrice
		}
	}

	s.Cash += netProceeds

	trade := models.Trade{
		ID:                    s.tradeIDCounter,
		Symbol:                symbol,
		OrderType:             pos.OrderType,
		EntryDate:             pos.EntryDate,
		EntryPrice:            pos.EntryPrice,
		TargetPrice:           pos.TargetPrice,
		StopLossPrice:         pos.StopLossPrice,
		ExitDate:              date,
		ExitPrice:             exitPrice,
		Shares:                pos.Shares,
		HoldDays:              pos.HoldDays,
		NetPnL:                netPnL,
		ReturnPct:             returnPct,
		ExitReason:            reason,
		CommissionPaid:        commission * 2, // Entry + exit
		MaxAdverseExcursion:   mae,
		MaxFavorableExcursion: mfe,
	}

	s.ClosedTrades = append(s.ClosedTrades, trade)
	delete(s.Positions, symbol)
	if s.lastExitDay != nil {
		s.lastExitDay[symbol] = dayIdx
	}
}

func (s *PortfolioSimulator) calculateTotalEquity(barsBySymbolDate map[string]map[string]models.Bar, date string) float64 {
	equity := s.Cash
	for sym, pos := range s.Positions {
		px := pos.CurrentPrice
		if bar, ok := barsBySymbolDate[sym][date]; ok {
			px = bar.Close
		}
		if pos.Direction == "SHORT" {
			// Locked collateral marked by (entry - price): shares*(2*entry - price).
			equity += float64(pos.Shares) * (2*pos.EntryPrice - px)
		} else {
			equity += float64(pos.Shares) * px
		}
	}
	if equity < 0 && s.allowNegativeEquity {
		return equity
	}
	return math.Max(0.0, equity)
}

// slipped moves a fill against the trader. adverseUp is true when the fill
// is a purchase (long entry, short cover).
func slipped(raw, slippage float64, adverseUp bool) float64 {
	if adverseUp {
		return raw * (1.0 + slippage)
	}
	return raw * (1.0 - slippage)
}
