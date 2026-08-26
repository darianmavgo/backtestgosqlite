package simulator

import (
	"math"
	"sort"
	"strings"

	"github.com/darianmavgo/backtestgosqlite/internal/analytics"
	"github.com/darianmavgo/backtestgosqlite/internal/models"
	"github.com/darianmavgo/backtestgosqlite/internal/strategy"
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
	Sizer          PositionSizer
	tradeIDCounter int
}

// NewPortfolioSimulator initializes a simulator instance.
func NewPortfolioSimulator(config strategy.StrategyConfig, initialCapital float64) *PortfolioSimulator {
	if initialCapital <= 0 {
		initialCapital = 100000.0 // Default $100k account
	}
	if config.HoldingWindow <= 0 {
		config.HoldingWindow = 10
	}
	if config.PositionCap <= 0 {
		config.PositionCap = 5
	}
	if config.AllocationPct <= 0 {
		config.AllocationPct = 0.20
	}
	if config.TargetPct <= 0 {
		config.TargetPct = 1.18
	}
	if config.StopLossPct <= 0 {
		config.StopLossPct = 0.93
	}

	return &PortfolioSimulator{
		Config:         config,
		InitialCapital: initialCapital,
		Cash:           initialCapital,
		Positions:      make(map[string]*models.Position),
		Sizer:          GetSizer(config),
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

	for _, date := range sortedDates {
		// 1. Evaluate and update existing open positions
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
				s.closePosition(sym, date, exitPrice, models.ExitReasonTrailingStop)
				continue
			}

			// B. ATR-Stop Trigger
			if s.Config.UseATRStop && pos.ATRStopPrice > 0 && bar.Low <= pos.ATRStopPrice {
				exitPrice := pos.ATRStopPrice * (1.0 - s.Config.SlippagePct)
				if bar.Open < pos.ATRStopPrice {
					exitPrice = bar.Open * (1.0 - s.Config.SlippagePct)
				}
				s.closePosition(sym, date, exitPrice, models.ExitReasonATRStop)
				continue
			}

			// C. Fixed Stop-Loss Trigger (Conservative Check: Low <= StopLossPrice)
			if bar.Low <= pos.StopLossPrice {
				exitPrice := pos.StopLossPrice * (1.0 - s.Config.SlippagePct)
				if bar.Open < pos.StopLossPrice {
					exitPrice = bar.Open * (1.0 - s.Config.SlippagePct)
				}
				s.closePosition(sym, date, exitPrice, models.ExitReasonStopLoss)
				continue
			}

			// D. Profit-Target Trigger (High >= TargetPrice)
			if bar.High >= pos.TargetPrice {
				exitPrice := pos.TargetPrice * (1.0 - s.Config.SlippagePct)
				if bar.Open > pos.TargetPrice {
					exitPrice = bar.Open * (1.0 - s.Config.SlippagePct)
				}
				s.closePosition(sym, date, exitPrice, models.ExitReasonProfitTarget)
				continue
			}

			// E. Max Holding Days Exceeded (Time-Up Market Exit)
			if pos.HoldDays >= s.Config.HoldingWindow {
				exitPrice := bar.Close * (1.0 - s.Config.SlippagePct)
				if s.Config.ExitAtMarketOpen {
					exitPrice = bar.Open * (1.0 - s.Config.SlippagePct)
				}
				s.closePosition(sym, date, exitPrice, models.ExitReasonTimeUp)
				continue
			}
		}

		// 2. Process new entry signals on current date
		if daySignals, hasSignals := signalsByDate[date]; hasSignals {
			sort.Slice(daySignals, func(i, j int) bool {
				return daySignals[i].Symbol < daySignals[j].Symbol
			})

			for _, sig := range daySignals {
				if len(s.Positions) >= s.Config.PositionCap {
					break // Max positions reached
				}
				if _, alreadyHeld := s.Positions[sig.Symbol]; alreadyHeld {
					continue // Already holding this symbol
				}

				totalEquity := s.calculateTotalEquity(barsBySymbolDate, date)

				// Determine entry price based on OrderType
				entryPrice := sig.Close * (1.0 + s.Config.SlippagePct)
				orderType := strings.ToLower(sig.OrderType)
				if orderType == "" {
					orderType = "limit"
				}

				if orderType == "market" {
					entryPrice = sig.Close * (1.0 + s.Config.SlippagePct)
				} else if orderType == "limit" && sig.BuyLimit > 0 {
					entryPrice = sig.BuyLimit * (1.0 + s.Config.SlippagePct)
				}

				if entryPrice <= 0 {
					continue
				}

				shares := s.Sizer.CalculateShares(s.Cash, totalEquity, entryPrice, s.Config)
				if shares <= 0 {
					continue
				}

				cost := float64(shares) * entryPrice
				commission := float64(shares) * s.Config.CommissionPerShare

				if cost+commission > s.Cash {
					continue
				}

				targetPrice := entryPrice * s.Config.TargetPct
				if sig.TakeProfit > 0 {
					targetPrice = sig.TakeProfit
				}
				stopLossPrice := entryPrice * s.Config.StopLossPct
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
				s.Positions[sig.Symbol] = &models.Position{
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

		s.EquityCurve = append(s.EquityCurve, models.DailyEquityPoint{
			Date:           date,
			Cash:           s.Cash,
			PositionsValue: totalEquity - s.Cash,
			TotalEquity:    totalEquity,
			OpenPositions:  len(s.Positions),
			DailyReturn:    dailyReturn,
			DrawdownPct:    drawdownPct,
		})
	}

	// 4. Force-close any open positions at end of backtest timeline
	if len(sortedDates) > 0 {
		lastDate := sortedDates[len(sortedDates)-1]
		for sym := range s.Positions {
			if bar, ok := barsBySymbolDate[sym][lastDate]; ok {
				s.closePosition(sym, lastDate, bar.Close*(1.0-s.Config.SlippagePct), models.ExitReasonEndBacktest)
			}
		}
	}

	// 5. Compute institutional performance metrics
	report := analytics.CalculatePerformanceMetricsWithBenchmark(s.InitialCapital, s.ClosedTrades, s.EquityCurve, s.BenchmarkBars)

	return report, s.ClosedTrades, s.EquityCurve
}

func (s *PortfolioSimulator) closePosition(symbol, date string, exitPrice float64, reason models.ExitReason) {
	pos, ok := s.Positions[symbol]
	if !ok {
		return
	}

	s.tradeIDCounter++
	grossProceeds := float64(pos.Shares) * exitPrice
	commission := float64(pos.Shares) * s.Config.CommissionPerShare
	netProceeds := grossProceeds - commission
	netPnL := netProceeds - (float64(pos.Shares) * pos.EntryPrice)
	returnPct := (exitPrice - pos.EntryPrice) / pos.EntryPrice

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
}

func (s *PortfolioSimulator) calculateTotalEquity(barsBySymbolDate map[string]map[string]models.Bar, date string) float64 {
	equity := s.Cash
	for sym, pos := range s.Positions {
		if bar, ok := barsBySymbolDate[sym][date]; ok {
			equity += float64(pos.Shares) * bar.Close
		} else {
			equity += float64(pos.Shares) * pos.CurrentPrice
		}
	}
	return math.Max(0.0, equity)
}
