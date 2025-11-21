package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/perfect-nt-bot/pkg/config"
	"github.com/perfect-nt-bot/pkg/feed"
	"github.com/perfect-nt-bot/pkg/risk"
	"github.com/perfect-nt-bot/pkg/scanner"
	"github.com/perfect-nt-bot/pkg/strategy"
)

// RealisticBacktestEngine runs a day-by-day backtest
type RealisticBacktestEngine struct {
	cfg      *config.Config
	scanner  *scanner.Scanner
	riskPct  float64
	evalMode bool
	location *time.Location

	// Account state
	buyingPower *risk.BuyingPowerManager
	riskLimits  *risk.RiskLimitsManager

	// Strategy
	strategyEngine *strategy.StrategyEngine

	// Current bar tracking per ticker
	currentBars  map[string]*feed.Bar // Ticker -> latest bar
	previousBars map[string]*feed.Bar // Ticker -> previous bar for pattern detection

	// Results
	trades         []*strategy.TradeResult
	accountBalance float64
	totalDays      int // Track total days processed for CSV filename
	runNumber      int // Track which run this is (for multiple simultaneous backtests)
}

// NewRealisticBacktestEngine creates a new backtest engine
func NewRealisticBacktestEngine(
	cfg *config.Config,
	scanner *scanner.Scanner,
	riskPct float64,
	evalMode bool,
	location *time.Location,
) *RealisticBacktestEngine {
	buyingPower := risk.NewBuyingPowerManager(cfg.AccountSize, true)
	riskLimits := risk.NewRiskLimitsManager(
		cfg.AccountSize,
		cfg.MaxDailyLossLimit,
		cfg.HardStopLossLimit,
		cfg.ProfitTarget,
		cfg.AccountCloseLimit,
	)

	// Get market open time (9:30 AM ET)
	now := time.Now().In(location)
	marketOpen := time.Date(now.Year(), now.Month(), now.Day(), 9, 30, 0, 0, location)

	strategyEngine := strategy.NewStrategyEngine(location, marketOpen)

	return &RealisticBacktestEngine{
		cfg:            cfg,
		scanner:        scanner,
		riskPct:        riskPct,
		evalMode:       evalMode,
		location:       location,
		buyingPower:    buyingPower,
		riskLimits:     riskLimits,
		strategyEngine: strategyEngine,
		currentBars:    make(map[string]*feed.Bar),
		previousBars:   make(map[string]*feed.Bar),
		trades:         make([]*strategy.TradeResult, 0),
		accountBalance: cfg.AccountSize,
		totalDays:      0,
		runNumber:      1, // Default to 1 if not set
	}
}

// Run runs the backtest day-by-day
func (rbe *RealisticBacktestEngine) Run(barsByDate map[time.Time]map[string][]feed.Bar) error {
	// Sort dates
	dates := make([]time.Time, 0, len(barsByDate))
	for date := range barsByDate {
		dates = append(dates, date)
	}
	sort.Slice(dates, func(i, j int) bool {
		return dates[i].Before(dates[j])
	})

	fmt.Printf("Processing %d trading days...\n", len(dates))

	for dayIdx, date := range dates {
		rbe.totalDays++

		// Reset daily state
		marketOpen := time.Date(date.Year(), date.Month(), date.Day(), 9, 30, 0, 0, rbe.location)
		eodTime := scanner.GetEODTime(date, rbe.location)

		rbe.strategyEngine.ResetDailyState(marketOpen)
		rbe.riskLimits.ResetDailyPnL()
		rbe.buyingPower.SetInRegularHours(true)

		// Reset current bars for new day (keep previous day's last bar for continuity)
		// We'll update them as we process bars

		fmt.Printf("\nDay %d/%d: %s\n", dayIdx+1, len(dates), date.Format("2006-01-02"))

		// Process this day's bars
		dayBars := barsByDate[date]

		// Check if trading is allowed
		if !rbe.riskLimits.CanTrade() {
			fmt.Printf("  Trading stopped: Daily loss hit or account closed\n")
			continue
		}

		// Process minute-by-minute across all tickers
		allMinuteBars := rbe.organizeByMinute(dayBars, date)

		for _, minuteBars := range allMinuteBars {
			if len(minuteBars) == 0 {
				continue
			}
			minuteTime := minuteBars[0].Bar.Time

			// Check if we've reached EOD (3:50 PM) - close all positions
			if minuteTime.After(eodTime) || minuteTime.Equal(eodTime) {
				// Close all positions at EOD
				rbe.closeAllPositionsAtEOD(eodTime)
				break
			}

			// Update indicators and track current bars for all tickers
			for _, tickerBar := range minuteBars {
				strategyBar := rbe.convertBar(tickerBar.Bar)
				rbe.strategyEngine.UpdateTicker(tickerBar.Ticker, strategyBar)

				// Store current bar for this ticker
				barCopy := tickerBar.Bar
				if prevBar, exists := rbe.currentBars[tickerBar.Ticker]; exists {
					rbe.previousBars[tickerBar.Ticker] = prevBar
				}
				rbe.currentBars[tickerBar.Ticker] = &barCopy
			}

			// Check total daily P&L (realized + unrealized) - close all positions if limit exceeded
			if rbe.checkDailyLossLimit(minuteBars, minuteTime, eodTime) {
				// Daily loss limit hit - all positions closed, stop processing this day
				break
			}

			// Check exits first (manage existing positions)
			rbe.checkExits(minuteTime, eodTime, minuteBars)

			// Check entries (only if we have buying power and positions available)
			if rbe.strategyEngine.GetPositionCount() < 3 && rbe.buyingPower.GetAvailableBuyingPower() > 0 {
				rbe.checkEntries(minuteTime, eodTime, minuteBars)
			}
		}

		// Update account balance
		rbe.accountBalance = rbe.riskLimits.GetAccountBalance()

		// Print daily summary
		dailyPnL := rbe.riskLimits.GetDailyPnL()
		fmt.Printf("  Daily P&L: $%.2f, Account: $%.2f\n", dailyPnL, rbe.accountBalance)

		// Check if profit target reached
		if rbe.riskLimits.IsProfitTargetHit() {
			fmt.Printf("\n*** PROFIT TARGET REACHED! Account: $%.2f ***\n", rbe.accountBalance)
			break
		}

		// Check if account closed
		if rbe.riskLimits.IsAccountClosed() {
			fmt.Printf("\n*** ACCOUNT CLOSED! Account: $%.2f ***\n", rbe.accountBalance)
			break
		}
	}

	// Print final results
	rbe.printResults()

	// Export CSV results
	if err := rbe.exportCSV(); err != nil {
		fmt.Printf("Warning: Failed to export CSV: %v\n", err)
	}

	return nil
}

// TickerBar represents a bar with its ticker
type TickerBar struct {
	Ticker string
	Bar    feed.Bar
}

// organizeByMinute organizes bars by minute timestamp
func (rbe *RealisticBacktestEngine) organizeByMinute(
	dayBars map[string][]feed.Bar,
	date time.Time,
) map[time.Time][]TickerBar {
	minuteMap := make(map[time.Time][]TickerBar)

	for ticker, bars := range dayBars {
		for _, bar := range bars {
			// Round to minute
			minuteTime := bar.Time.Truncate(time.Minute)
			minuteMap[minuteTime] = append(minuteMap[minuteTime], TickerBar{
				Ticker: ticker,
				Bar:    bar,
			})
		}
	}

	return minuteMap
}

// convertBar converts feed.Bar to strategy.Bar
func (rbe *RealisticBacktestEngine) convertBar(fb feed.Bar) strategy.Bar {
	return strategy.Bar{
		Time:   fb.Time,
		Open:   fb.Open,
		High:   fb.High,
		Low:    fb.Low,
		Close:  fb.Close,
		Volume: fb.Volume,
	}
}

// checkEntries checks for entry signals and executes trades
func (rbe *RealisticBacktestEngine) checkEntries(currentTime time.Time, eodTime time.Time, minuteBars []TickerBar) {
	// Collect entry signals from current minute bars
	signals := make([]*strategy.EntrySignal, 0)

	for _, tickerBar := range minuteBars {
		ticker := tickerBar.Ticker

		// Skip if already in a position
		if rbe.strategyEngine.HasPosition(ticker) {
			continue
		}

		// Get current bar
		currentBar := tickerBar.Bar
		strategyBar := rbe.convertBar(currentBar)

		// Get ticker state (indicators)
		tickerState, exists := rbe.strategyEngine.GetTickerState(ticker)
		if !exists {
			continue
		}

		// Check if indicators are ready (need VWAP, ATR, RSI calculated)
		// VWAP resets daily so it's available after first bar
		// ATR needs 14 periods, RSI needs 14 periods
		if tickerState.VWAP == 0 || tickerState.ATR == 0 || tickerState.RSI == 0 {
			continue // Indicators not ready yet
		}

		// Check if this bar was just processed (indicators updated)
		if !tickerState.LastUpdate.Equal(currentBar.Time) {
			continue // Indicators not updated for this bar yet
		}

		// Get previous bar for pattern detection
		var previousBar strategy.Bar
		if prevBar, exists := rbe.previousBars[ticker]; exists {
			previousBar = rbe.convertBar(*prevBar)
		}

		// Check entry conditions
		openPositions := rbe.strategyEngine.GetPositionCount()

		signal, err := rbe.strategyEngine.CheckEntry(ticker, strategyBar, eodTime, openPositions)
		if err != nil {
			// Entry conditions not met, skip
			continue
		}

		// Re-check with previous bar for pattern detection if available
		if !previousBar.Time.IsZero() {
			// Try checking with previous bar for better pattern detection
			entryChecker := strategy.NewEntryChecker()
			if updatedSignal, err := entryChecker.CheckEntryConditionsWithPrevious(
				ticker,
				strategyBar,
				previousBar,
				tickerState,
				openPositions,
				eodTime,
			); err == nil {
				signal = updatedSignal
			}
		}

		// Add to signals list
		signals = append(signals, signal)
	}

	// Score and select best signals (up to 3 since we can hold 3 positions)
	maxSignals := 3
	if len(signals) < maxSignals {
		maxSignals = len(signals)
	}
	bestSignals := rbe.scanner.SelectBestSignals(signals, maxSignals)

	// Execute entries (can take multiple signals per minute up to max positions)
	for _, signal := range bestSignals {
		if rbe.strategyEngine.GetPositionCount() >= 3 {
			break // Max positions reached (3 for more opportunities)
		}
		rbe.executeEntry(signal, currentTime)
	}
}

// executeEntry executes an entry trade
func (rbe *RealisticBacktestEngine) executeEntry(signal *strategy.EntrySignal, entryTime time.Time) {
	// Get current bar for slippage calculation
	currentBar, exists := rbe.currentBars[signal.Ticker]
	if !exists {
		// Fallback to signal price if bar not available
		currentBar = &feed.Bar{
			Time:   entryTime,
			Open:   signal.EntryPrice,
			High:   signal.EntryPrice,
			Low:    signal.EntryPrice,
			Close:  signal.EntryPrice,
			Volume: 0,
		}
	}
	strategyBar := rbe.convertBar(*currentBar)

	// Simulate realistic fill price with slippage for entry
	fillPrice := strategy.GetFillPrice(strategyBar, signal.Direction, true)

	// Calculate position size based on fill price (not signal price) to account for slippage
	// Use riskPct (0.3% = 0.003) of account balance, but use a stable base to prevent
	// position sizes from shrinking too much after losses
	// Use max of current balance or 80% of initial to maintain reasonable position sizes
	stableBalance := rbe.accountBalance
	if stableBalance < rbe.cfg.AccountSize*0.8 {
		stableBalance = rbe.cfg.AccountSize * 0.8
	}
	riskAmount := stableBalance * rbe.riskPct // 0.3% of stable balance (e.g., $75 for $25k)

	// Log account balance and risk amount for debugging
	fmt.Printf("  [POSITION SIZING] Account: $%.2f, Stable: $%.2f, Risk Amount: $%.2f\n",
		rbe.accountBalance, stableBalance, riskAmount)

	shares, err := risk.CalculatePositionSize(
		riskAmount,
		fillPrice, // Use fill price with slippage for position sizing
		signal.StopLoss,
		2500, // Max shares
	)
	if err != nil {
		fmt.Printf("  Error calculating position size: %v\n", err)
		return
	}

	// Check if we can afford it (using fill price with slippage)
	if !rbe.buyingPower.CanAfford(shares, fillPrice, signal.Direction) {
		fmt.Printf("  Cannot afford position: %s %d shares @ $%.2f\n",
			signal.Ticker, shares, fillPrice)
		return
	}

	// Reserve buying power (using fill price with slippage)
	rbe.buyingPower.ReserveBuyingPower(shares, fillPrice, signal.Direction)

	// Update signal with fill price for position opening
	signal.EntryPrice = fillPrice

	// Open position
	rbe.strategyEngine.OpenPosition(signal, shares)

	// Calculate risk per share for logging
	riskPerShare := math.Abs(fillPrice - signal.StopLoss)
	totalRisk := riskPerShare * float64(shares)

	fmt.Printf("  ENTRY: %s SHORT %d shares @ $%.2f (Stop: $%.2f, Risk/share: $%.2f, Total Risk: $%.2f) [Fill w/ slippage: $%.2f]\n",
		signal.Ticker, shares, fillPrice, signal.StopLoss, riskPerShare, totalRisk, fillPrice)
}

// checkDailyLossLimit checks if total daily P&L (realized + unrealized) exceeds limit
// Returns true if daily loss limit was hit and all positions were closed
func (rbe *RealisticBacktestEngine) checkDailyLossLimit(minuteBars []TickerBar, currentTime time.Time, eodTime time.Time) bool {
	// Calculate realized daily P&L
	realizedPnL := rbe.riskLimits.GetDailyPnL()

	// Calculate unrealized P&L for all open positions
	unrealizedPnL := 0.0
	positions := rbe.strategyEngine.GetPositions()

	// Create map of current bars by ticker for quick lookup
	barMap := make(map[string]*feed.Bar)
	for _, tickerBar := range minuteBars {
		barCopy := tickerBar.Bar
		barMap[tickerBar.Ticker] = &barCopy
	}

	for _, position := range positions {
		// Get current bar for this position
		currentBar, exists := barMap[position.Ticker]
		if !exists {
			// Use stored current bar if available
			if storedBar, hasStored := rbe.currentBars[position.Ticker]; hasStored {
				currentBar = storedBar
			} else {
				continue // No bar available, skip this position
			}
		}

		// Calculate unrealized P&L at current price (before commissions)
		currentPrice := currentBar.Close
		grossUnrealizedPnL := strategy.CalculatePnL(
			position.EntryPrice,
			currentPrice,
			position.RemainingShares,
			position.Direction,
		)

		// Estimate commissions (entry + exit)
		estimatedCommission := strategy.CalculateCommission(position.RemainingShares) * 2

		// Calculate net unrealized P&L (gross - estimated commissions)
		netUnrealizedPnL := grossUnrealizedPnL - estimatedCommission

		unrealizedPnL += netUnrealizedPnL
	}

	// Total daily P&L = realized + unrealized
	totalDailyPnL := realizedPnL + unrealizedPnL
	maxAllowedLoss := rbe.cfg.MaxDailyLossLimit

	// Check if total exceeds the limit (only check losses)
	if totalDailyPnL < -maxAllowedLoss {
		// Daily loss limit exceeded! Close all positions immediately
		fmt.Printf("  [DAILY LOSS LIMIT] Total daily P&L (realized $%.2f + unrealized $%.2f = $%.2f) exceeds limit ($%.2f)\n",
			realizedPnL, unrealizedPnL, totalDailyPnL, -maxAllowedLoss)
		fmt.Printf("  [DAILY LOSS LIMIT] Closing all %d open positions immediately\n", len(positions))

		// Close all positions at their current prices
		// executeExit will automatically cap each position's loss to stay within the daily limit
		// as each position closes, the remaining allowed loss shrinks for subsequent positions
		for i, position := range positions {
			currentBar, exists := barMap[position.Ticker]
			if !exists {
				// Use stored current bar if available
				if storedBar, hasStored := rbe.currentBars[position.Ticker]; hasStored {
					currentBar = storedBar
				} else {
					// Use entry price as fallback
					currentBar = &feed.Bar{
						Time:   currentTime,
						Open:   position.EntryPrice,
						High:   position.EntryPrice,
						Low:    position.EntryPrice,
						Close:  position.EntryPrice,
						Volume: 0,
					}
				}
			}

			// Close at current price with Max Daily Loss reason
			// executeExit will check daily loss limit and cap this position's loss appropriately
			fmt.Printf("  [DAILY LOSS LIMIT] Closing position %d/%d: %s\n", i+1, len(positions), position.Ticker)
			rbe.executeExit(position, currentBar.Close, strategy.ExitReasonMaxDailyLoss, currentTime)
		}

		return true // Daily loss limit hit, stop trading for the day
	}

	return false // Still within limits
}

// checkExits checks all positions for exit conditions
func (rbe *RealisticBacktestEngine) checkExits(currentTime time.Time, eodTime time.Time, minuteBars []TickerBar) {
	// Create map of current bars by ticker for quick lookup
	barMap := make(map[string]*feed.Bar)
	for _, tickerBar := range minuteBars {
		barCopy := tickerBar.Bar
		barMap[tickerBar.Ticker] = &barCopy
	}

	// Get all positions
	positions := rbe.strategyEngine.GetPositions()

	for _, position := range positions {
		// Get current bar for this ticker
		currentBar, exists := barMap[position.Ticker]
		if !exists {
			// Use stored current bar if available
			if storedBar, hasStored := rbe.currentBars[position.Ticker]; hasStored {
				currentBar = storedBar
			} else {
				continue // No bar available for this ticker
			}
		}

		strategyBar := rbe.convertBar(*currentBar)

		// Check for exit conditions for this specific position
		exitChecker := strategy.NewExitChecker()
		shouldExit, reason, exitPrice := exitChecker.CheckExitConditions(position, strategyBar, eodTime)

		if shouldExit {
			// Handle exit
			rbe.executeExit(position, exitPrice, reason, currentTime)
			continue // Position closed, move to next
		}

		// Check for partial exits (target 1, target 2)
		rbe.checkPartialExits(position, currentBar, currentTime)
	}
}

// checkPartialExits checks for partial profit targets
func (rbe *RealisticBacktestEngine) checkPartialExits(position *strategy.Position, currentBar *feed.Bar, currentTime time.Time) {
	// Check target 1 (take 50% profit) - using bar close for signal check, slippage applied in execute
	if !position.FilledTarget1 {
		// Check against bar close for signal (before slippage)
		var signalPnLPerShare float64
		if position.Direction == "SHORT" {
			signalPnLPerShare = position.EntryPrice - currentBar.Close
		} else {
			signalPnLPerShare = currentBar.Close - position.EntryPrice
		}

		if signalPnLPerShare >= 0.08 { // $0.08/share (matched to entry checker - faster profits)
			sharesToClose := position.RemainingShares / 2
			if sharesToClose > 0 {
				rbe.executePartialExit(position, sharesToClose, currentBar.Close, strategy.ExitReasonTarget1, currentTime)
				rbe.strategyEngine.MarkTarget1Filled(position.Ticker)
			}
		}
	}

	// Check target 2 (close remaining 50%)
	if position.FilledTarget1 && !position.FilledTarget2 {
		var signalPnLPerShare float64
		if position.Direction == "SHORT" {
			signalPnLPerShare = position.EntryPrice - currentBar.Close
		} else {
			signalPnLPerShare = currentBar.Close - position.EntryPrice
		}

		if signalPnLPerShare >= 0.15 { // $0.15/share (matched to entry checker - faster exits)
			rbe.executeExit(position, currentBar.Close, strategy.ExitReasonTarget2, currentTime)
		}
	}
}

// closeAllPositionsAtEOD closes all positions at EOD
func (rbe *RealisticBacktestEngine) closeAllPositionsAtEOD(eodTime time.Time) {
	positions := rbe.strategyEngine.CloseAllPositions()

	// Close all positions at EOD using last known price
	for _, position := range positions {
		// Get last bar for this ticker
		lastBar, exists := rbe.currentBars[position.Ticker]
		if !exists {
			// Use entry price as fallback (would be a scratch trade)
			lastBar = &feed.Bar{
				Time:   eodTime,
				Open:   position.EntryPrice,
				High:   position.EntryPrice,
				Low:    position.EntryPrice,
				Close:  position.EntryPrice,
				Volume: 0,
			}
		}

		exitPrice := lastBar.Close
		rbe.executeExit(position, exitPrice, strategy.ExitReasonEOD, eodTime)
	}
}

// executeExit executes a full exit for a position
func (rbe *RealisticBacktestEngine) executeExit(position *strategy.Position, exitPrice float64, reason strategy.ExitReason, exitTime time.Time) {
	shares := position.RemainingShares
	if shares <= 0 {
		return
	}

	// Get current bar for slippage calculation
	currentBar, exists := rbe.currentBars[position.Ticker]
	if !exists {
		// Fallback to exit price if bar not available
		currentBar = &feed.Bar{
			Time:   exitTime,
			Open:   exitPrice,
			High:   exitPrice,
			Low:    exitPrice,
			Close:  exitPrice,
			Volume: 0,
		}
	}
	strategyBar := rbe.convertBar(*currentBar)

	// Simulate realistic fill price with slippage
	fillPrice := strategy.GetFillPrice(strategyBar, position.Direction, false)

	// Calculate gross P&L first (before commissions) for eval rule check
	grossPnL := strategy.CalculatePnL(
		position.EntryPrice,
		fillPrice,
		shares,
		position.Direction,
	)

	// Eval rule: No single trade can account for more than 30% of profit target
	// Check against gross P&L (before commissions)
	if rbe.evalMode && grossPnL > 0 && grossPnL > rbe.cfg.MaxProfitPerTrade {
		// Cap profit at 30% of profit target
		maxAllowedPnL := rbe.cfg.MaxProfitPerTrade

		// Calculate what exit price would give us the max allowed profit
		var cappedExitPrice float64
		if position.Direction == "SHORT" {
			// For shorts: P&L = (entry - exit) * shares
			// So: maxAllowedPnL = (entry - exit) * shares
			// exit = entry - (maxAllowedPnL / shares)
			cappedExitPrice = position.EntryPrice - (maxAllowedPnL / float64(shares))
		} else {
			// For longs: P&L = (exit - entry) * shares
			// exit = entry + (maxAllowedPnL / shares)
			cappedExitPrice = position.EntryPrice + (maxAllowedPnL / float64(shares))
		}

		// Use the capped exit price (but still apply slippage)
		// Recalculate fill price based on capped price
		cappedBar := strategy.Bar{
			Time:   exitTime,
			Open:   cappedExitPrice,
			High:   cappedExitPrice * 1.001, // Small range for slippage calc
			Low:    cappedExitPrice * 0.999,
			Close:  cappedExitPrice,
			Volume: 0,
		}
		fillPrice = strategy.GetFillPrice(cappedBar, position.Direction, false)

		// Recalculate gross P&L with slippage after capping
		grossPnL = strategy.CalculatePnL(
			position.EntryPrice,
			fillPrice,
			shares,
			position.Direction,
		)

		fmt.Printf("  [EVAL RULE] Trade profit capped at $%.2f (30%% of profit target)\n", maxAllowedPnL)
	}

	// Now calculate net P&L (after commissions and profit threshold rule)
	netPnL, commission := strategy.CalculateNetPnL(
		position.EntryPrice,
		fillPrice,
		shares,
		position.Direction,
	)

	// Check daily loss limit BEFORE updating - if this trade would exceed it, cap it
	currentDailyPnL := rbe.riskLimits.GetDailyPnL()
	if netPnL < 0 { // Only check for losses
		maxAllowedLoss := rbe.cfg.MaxDailyLossLimit
		projectedDailyPnL := currentDailyPnL + netPnL

		if projectedDailyPnL < -maxAllowedLoss {
			// This trade would exceed the daily loss limit - cap it
			// Calculate how much loss we can still take from this trade
			maxAllowedTradeNetLoss := -maxAllowedLoss - currentDailyPnL
			// maxAllowedTradeNetLoss should be negative (a loss)

			// For net P&L: netPnL = grossPnL - commissions
			// So: grossPnL = netPnL + commissions
			// We want grossPnL such that netPnL = maxAllowedTradeNetLoss
			// But we need to know commissions first. Let's estimate: 2 * CalculateCommission(shares)
			estimatedCommission := strategy.CalculateCommission(shares) * 2
			requiredGrossPnL := maxAllowedTradeNetLoss + estimatedCommission

			// Calculate what exit price would give us the required gross P&L
			var cappedExitPrice float64
			if position.Direction == "SHORT" {
				// For shorts: grossPnL = (entry - exit) * shares
				// exit = entry - (grossPnL / shares)
				cappedExitPrice = position.EntryPrice - (requiredGrossPnL / float64(shares))
			} else {
				// For longs: grossPnL = (exit - entry) * shares
				// exit = entry + (grossPnL / shares)
				cappedExitPrice = position.EntryPrice + (requiredGrossPnL / float64(shares))
			}

			// Apply slippage to capped price
			cappedBar := strategy.Bar{
				Time:   exitTime,
				Open:   cappedExitPrice,
				High:   cappedExitPrice * 1.001,
				Low:    cappedExitPrice * 0.999,
				Close:  cappedExitPrice,
				Volume: 0,
			}
			fillPrice = strategy.GetFillPrice(cappedBar, position.Direction, false)

			// Recalculate P&L after capping
			grossPnL = strategy.CalculatePnL(
				position.EntryPrice,
				fillPrice,
				shares,
				position.Direction,
			)
			netPnL, commission = strategy.CalculateNetPnL(
				position.EntryPrice,
				fillPrice,
				shares,
				position.Direction,
			)

			// Final check: ensure we didn't exceed the limit after recalculating
			if currentDailyPnL+netPnL < -maxAllowedLoss {
				// Still exceeded, need to adjust further
				// Take exactly the remaining allowed loss
				netPnL = -maxAllowedLoss - currentDailyPnL
				// This might be slightly off due to commissions, but it's the best we can do
			}

			// Update reason to indicate daily loss limit was hit
			reason = strategy.ExitReasonMaxDailyLoss

			fmt.Printf("  [DAILY LOSS LIMIT] Trade loss capped at $%.2f (daily limit: $%.2f, current daily: $%.2f)\n", -netPnL, maxAllowedLoss, currentDailyPnL)
		}
	}

	// Update risk limits (daily P&L tracking) - use net P&L for account balance
	rbe.riskLimits.UpdateDailyPnL(netPnL, exitTime)

	// Update buying power
	rbe.buyingPower.ReleaseBuyingPower(shares, position.EntryPrice, position.Direction)
	rbe.buyingPower.UpdateAccountBalance(netPnL)

	// Update account balance
	oldBalance := rbe.accountBalance
	rbe.accountBalance = rbe.riskLimits.GetAccountBalance()
	fmt.Printf("  [ACCOUNT UPDATE] Balance: $%.2f -> $%.2f (Change: $%.2f)\n",
		oldBalance, rbe.accountBalance, rbe.accountBalance-oldBalance)

	// Record trade result
	trade := &strategy.TradeResult{
		Ticker:     position.Ticker,
		EntryTime:  position.EntryTime,
		ExitTime:   exitTime,
		EntryPrice: position.EntryPrice,
		ExitPrice:  fillPrice, // Use fill price (with slippage)
		Shares:     shares,
		Direction:  position.Direction,
		Reason:     reason,
		PnL:        grossPnL, // Store gross P&L for reference
		Commission: commission,
		NetPnL:     netPnL, // Store net P&L (already has commissions and profit threshold applied)
	}
	rbe.trades = append(rbe.trades, trade)

	// Close position
	rbe.strategyEngine.ClosePosition(position.Ticker)

	fmt.Printf("  EXIT: %s %d shares @ $%.2f (%s) - Net P&L: $%.2f (Commission: $%.2f) [Fill: $%.2f]\n",
		position.Ticker, shares, exitPrice, reason, trade.NetPnL, commission, fillPrice)

	// If daily loss limit was hit, close all remaining positions at their current prices
	// This must be done AFTER closing the current position to avoid infinite recursion
	if rbe.riskLimits.IsDailyLossHit() {
		remainingPositions := rbe.strategyEngine.GetPositions()
		for _, remainingPosition := range remainingPositions {
			// Get current bar for remaining position
			currentBar, exists := rbe.currentBars[remainingPosition.Ticker]
			if !exists {
				// Use entry price as fallback (scratch trade)
				currentBar = &feed.Bar{
					Time:   exitTime,
					Open:   remainingPosition.EntryPrice,
					High:   remainingPosition.EntryPrice,
					Low:    remainingPosition.EntryPrice,
					Close:  remainingPosition.EntryPrice,
					Volume: 0,
				}
			}
			// Close at current price with Max Daily Loss reason
			// Note: executeExit will check daily loss limit again and cap losses appropriately
			rbe.executeExit(remainingPosition, currentBar.Close, strategy.ExitReasonMaxDailyLoss, exitTime)
		}
	}
}

// executePartialExit executes a partial exit (e.g., target 1)
func (rbe *RealisticBacktestEngine) executePartialExit(position *strategy.Position, shares int, exitPrice float64, reason strategy.ExitReason, exitTime time.Time) {
	if shares <= 0 || shares >= position.RemainingShares {
		return
	}

	// Get current bar for slippage calculation
	currentBar, exists := rbe.currentBars[position.Ticker]
	if !exists {
		currentBar = &feed.Bar{
			Time:   exitTime,
			Open:   exitPrice,
			High:   exitPrice,
			Low:    exitPrice,
			Close:  exitPrice,
			Volume: 0,
		}
	}
	strategyBar := rbe.convertBar(*currentBar)

	// Simulate realistic fill price with slippage
	fillPrice := strategy.GetFillPrice(strategyBar, position.Direction, false)

	// Calculate gross P&L first (before commissions) for eval rule check
	grossPnL := strategy.CalculatePnL(
		position.EntryPrice,
		fillPrice,
		shares,
		position.Direction,
	)

	// Eval rule: Check if this partial exit would exceed the limit
	// We need to check total GROSS profit from this position (before commissions)
	totalGrossProfit := grossPnL
	// Calculate total gross profit from previous partial exits (need to recalculate from PnL field)
	// Note: TradeResult.PnL is gross P&L, TradeResult.NetPnL is net P&L
	for _, prevTrade := range rbe.trades {
		if prevTrade.Ticker == position.Ticker &&
			prevTrade.EntryTime.Equal(position.EntryTime) &&
			prevTrade.PnL > 0 { // Use gross P&L for eval rule check
			totalGrossProfit += prevTrade.PnL
		}
	}

	// If total GROSS profit exceeds limit, cap this exit
	if rbe.evalMode && totalGrossProfit > 0 && totalGrossProfit > rbe.cfg.MaxProfitPerTrade {
		// Calculate how much gross profit we can still take from this position
		maxAllowedRemaining := rbe.cfg.MaxProfitPerTrade - (totalGrossProfit - grossPnL)
		if maxAllowedRemaining < 0 {
			maxAllowedRemaining = 0
		}

		if grossPnL > maxAllowedRemaining {
			// Cap this partial exit
			var cappedExitPrice float64
			if position.Direction == "SHORT" {
				cappedExitPrice = position.EntryPrice - (maxAllowedRemaining / float64(shares))
			} else {
				cappedExitPrice = position.EntryPrice + (maxAllowedRemaining / float64(shares))
			}

			// Apply slippage to capped price
			cappedBar := strategy.Bar{
				Time:   exitTime,
				Open:   cappedExitPrice,
				High:   cappedExitPrice * 1.001,
				Low:    cappedExitPrice * 0.999,
				Close:  cappedExitPrice,
				Volume: 0,
			}
			fillPrice = strategy.GetFillPrice(cappedBar, position.Direction, false)

			// Recalculate gross P&L after capping
			grossPnL = strategy.CalculatePnL(
				position.EntryPrice,
				fillPrice,
				shares,
				position.Direction,
			)

			fmt.Printf("  [EVAL RULE] Partial exit profit capped to stay within $%.2f limit\n", rbe.cfg.MaxProfitPerTrade)
		}
	}

	// Now calculate net P&L (after commissions and profit threshold rule)
	netPnL, commission := strategy.CalculateNetPnL(
		position.EntryPrice,
		fillPrice,
		shares,
		position.Direction,
	)

	// Update risk limits - use net P&L
	rbe.riskLimits.UpdateDailyPnL(netPnL, exitTime)

	// Update buying power
	rbe.buyingPower.ReleaseBuyingPower(shares, position.EntryPrice, position.Direction)
	rbe.buyingPower.UpdateAccountBalance(netPnL)

	// Update account balance
	oldBalance := rbe.accountBalance
	rbe.accountBalance = rbe.riskLimits.GetAccountBalance()
	fmt.Printf("  [ACCOUNT UPDATE] Balance: $%.2f -> $%.2f (Change: $%.2f)\n",
		oldBalance, rbe.accountBalance, rbe.accountBalance-oldBalance)

	// Record partial trade (we'll record full trade on final exit)
	// For now, just record it as a separate trade
	trade := &strategy.TradeResult{
		Ticker:     position.Ticker,
		EntryTime:  position.EntryTime,
		ExitTime:   exitTime,
		EntryPrice: position.EntryPrice,
		ExitPrice:  fillPrice, // Use fill price (with slippage)
		Shares:     shares,
		Direction:  position.Direction,
		Reason:     reason,
		PnL:        grossPnL, // Store gross P&L
		Commission: commission,
		NetPnL:     netPnL, // Store net P&L (already has commissions applied)
	}
	rbe.trades = append(rbe.trades, trade)

	// Update position
	rbe.strategyEngine.ClosePartial(position.Ticker, shares)

	fmt.Printf("  PARTIAL EXIT: %s %d shares @ $%.2f (%s) - Net P&L: $%.2f (Commission: $%.2f) [Fill: $%.2f]\n",
		position.Ticker, shares, exitPrice, reason, trade.NetPnL, commission, fillPrice)
}

// printResults prints backtest results
func (rbe *RealisticBacktestEngine) printResults() {
	fmt.Println("\n=== BACKTEST RESULTS ===")
	fmt.Printf("Total Trades: %d\n", len(rbe.trades))
	fmt.Printf("Final Account Balance: $%.2f\n", rbe.accountBalance)
	fmt.Printf("Total P&L: $%.2f\n", rbe.accountBalance-rbe.cfg.AccountSize)

	if len(rbe.trades) > 0 {
		wins := 0
		totalPnL := 0.0
		for _, trade := range rbe.trades {
			if trade.NetPnL > 0 {
				wins++
			}
			totalPnL += trade.NetPnL
		}
		winRate := float64(wins) / float64(len(rbe.trades)) * 100
		fmt.Printf("Win Rate: %.2f%%\n", winRate)
		fmt.Printf("Total Net P&L: $%.2f\n", totalPnL)
	}
}

// exportCSV exports backtest results to CSV file
func (rbe *RealisticBacktestEngine) exportCSV() error {
	if len(rbe.trades) == 0 {
		return nil // No trades to export
	}

	// Create results directory if it doesn't exist
	resultsDir := "cmd/backtest/results"
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return fmt.Errorf("failed to create results directory: %v", err)
	}

	// Generate filename: backtest_YYYYMMDD_HHMMSS_runN_Nd_Npct.csv
	now := time.Now()
	totalPct := ((rbe.accountBalance - rbe.cfg.AccountSize) / rbe.cfg.AccountSize) * 100
	filename := fmt.Sprintf("backtest_%s_run%d_%dd_%.1fpct.csv",
		now.Format("20060102_150405"),
		rbe.runNumber,
		rbe.totalDays,
		totalPct,
	)
	filepath := filepath.Join(resultsDir, filename)

	// Create CSV file
	file, err := os.Create(filepath)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{
		"Ticker",
		"EntryTime",
		"ExitTime",
		"Direction",
		"EntryPrice",
		"ExitPrice",
		"Shares",
		"Reason",
		"GrossPnL",
		"Commission",
		"NetPnL",
	}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("failed to write header: %v", err)
	}

	// Write trade data
	for _, trade := range rbe.trades {
		record := []string{
			trade.Ticker,
			trade.EntryTime.Format(time.RFC3339),
			trade.ExitTime.Format(time.RFC3339),
			trade.Direction, // Direction (SHORT/LONG)
			strconv.FormatFloat(trade.EntryPrice, 'f', 2, 64),
			strconv.FormatFloat(trade.ExitPrice, 'f', 2, 64),
			strconv.Itoa(trade.Shares),
			string(trade.Reason),
			strconv.FormatFloat(trade.PnL, 'f', 2, 64),        // Gross P&L
			strconv.FormatFloat(trade.Commission, 'f', 2, 64), // Commission
			strconv.FormatFloat(trade.NetPnL, 'f', 2, 64),     // Net P&L
		}
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("failed to write record: %v", err)
		}
	}

	fmt.Printf("\nResults exported to: %s\n", filepath)
	return nil
}
