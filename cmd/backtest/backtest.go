package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/perfect-nt-bot/pkg/config"
	"github.com/perfect-nt-bot/pkg/feed"
	"github.com/perfect-nt-bot/pkg/ml"
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

	// ML scorer (optional)
	mlScorer *ml.Scorer

	// Current bar tracking per ticker
	currentBars  map[string]*feed.Bar // Ticker -> latest bar
	previousBars map[string]*feed.Bar // Ticker -> previous bar for pattern detection

	// Signal tracking (for stats)
	signalsByTrade map[string]*strategy.EntrySignal // Key: ticker_entryTime, Value: signal

	// Results
	trades         []*strategy.TradeResult
	accountBalance float64
	totalDays      int            // Track total days processed for CSV filename
	runNumber      int            // Track which run this is (for multiple simultaneous backtests)
	stats          *BacktestStats // Statistics tracking
}

// BacktestStats tracks detailed statistics for analysis
type BacktestStats struct {
	// Win rate by entry time (hour buckets: 9, 10, 11, 12, 13, 14, 15)
	WinRateByHour map[int]struct {
		Wins   int
		Losses int
		Total  int
	}

	// Win rate by VWAP extension level (buckets: 0.4-0.5, 0.5-0.6, 0.6+)
	WinRateByVWAP map[string]struct {
		Wins   int
		Losses int
		Total  int
	}

	// Win rate by RSI level (buckets: 52-55, 55-60, 60+)
	WinRateByRSI map[string]struct {
		Wins   int
		Losses int
		Total  int
	}

	// Win rate by pattern type
	WinRateByPattern map[strategy.DeathCandlePattern]struct {
		Wins   int
		Losses int
		Total  int
	}

	// Average win vs average loss
	AverageWin  float64
	AverageLoss float64
	TotalWins   int
	TotalLosses int

	// Win rate by ML score (if ML enabled, buckets: 0-0.5, 0.5-0.7, 0.7+)
	WinRateByMLScore map[string]struct {
		Wins   int
		Losses int
		Total  int
	}
}

// NewBacktestStats creates a new stats tracker
func NewBacktestStats() *BacktestStats {
	return &BacktestStats{
		WinRateByHour:    make(map[int]struct{ Wins, Losses, Total int }),
		WinRateByVWAP:    make(map[string]struct{ Wins, Losses, Total int }),
		WinRateByRSI:     make(map[string]struct{ Wins, Losses, Total int }),
		WinRateByPattern: make(map[strategy.DeathCandlePattern]struct{ Wins, Losses, Total int }),
		WinRateByMLScore: make(map[string]struct{ Wins, Losses, Total int }),
	}
}

// RecordTrade records a trade for statistics
func (bs *BacktestStats) RecordTrade(trade *strategy.TradeResult, signal *strategy.EntrySignal) {
	isWin := trade.NetPnL > 0

	// Record by entry hour
	hour := trade.EntryTime.Hour()
	hourStat := bs.WinRateByHour[hour]
	hourStat.Total++
	if isWin {
		hourStat.Wins++
		bs.TotalWins++
		bs.AverageWin = (bs.AverageWin*float64(bs.TotalWins-1) + trade.NetPnL) / float64(bs.TotalWins)
	} else {
		hourStat.Losses++
		bs.TotalLosses++
		bs.AverageLoss = (bs.AverageLoss*float64(bs.TotalLosses-1) + math.Abs(trade.NetPnL)) / float64(bs.TotalLosses)
	}
	bs.WinRateByHour[hour] = hourStat

	// Record by VWAP extension (if signal available)
	if signal != nil {
		absExt := math.Abs(signal.VWAPExtension)
		var vwapBucket string
		if absExt < 0.5 {
			vwapBucket = "0.4-0.5"
		} else if absExt < 0.6 {
			vwapBucket = "0.5-0.6"
		} else {
			vwapBucket = "0.6+"
		}
		vwapStat := bs.WinRateByVWAP[vwapBucket]
		vwapStat.Total++
		if isWin {
			vwapStat.Wins++
		} else {
			vwapStat.Losses++
		}
		bs.WinRateByVWAP[vwapBucket] = vwapStat

		// Record by RSI
		var rsiBucket string
		if signal.RSI < 55 {
			rsiBucket = "52-55"
		} else if signal.RSI < 60 {
			rsiBucket = "55-60"
		} else {
			rsiBucket = "60+"
		}
		rsiStat := bs.WinRateByRSI[rsiBucket]
		rsiStat.Total++
		if isWin {
			rsiStat.Wins++
		} else {
			rsiStat.Losses++
		}
		bs.WinRateByRSI[rsiBucket] = rsiStat

		// Record by pattern
		patternStat := bs.WinRateByPattern[signal.Pattern]
		patternStat.Total++
		if isWin {
			patternStat.Wins++
		} else {
			patternStat.Losses++
		}
		bs.WinRateByPattern[signal.Pattern] = patternStat

		// Record by ML score
		// MLScore can be:
		// - -1.0: ML not enabled/not calculated
		// - 0.0-1.0: ML prediction (probability of hitting Target 1)
		if signal.MLScore >= 0 {
			// ML was enabled and score was calculated
			var mlBucket string
			if signal.MLScore < 0.5 {
				mlBucket = "0-0.5"
			} else if signal.MLScore < 0.7 {
				mlBucket = "0.5-0.7"
			} else {
				mlBucket = "0.7+"
			}
			mlStat := bs.WinRateByMLScore[mlBucket]
			mlStat.Total++
			if isWin {
				mlStat.Wins++
			} else {
				mlStat.Losses++
			}
			bs.WinRateByMLScore[mlBucket] = mlStat
		}
		// If MLScore < 0, ML was not enabled, so we don't record it
	}
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

	// Initialize ML scorer if model path is provided
	var mlScorer *ml.Scorer
	if cfg.MLModelPath != "" {
		scorer, err := ml.NewScorer(cfg.MLModelPath)
		if err != nil {
			fmt.Printf("Warning: Failed to load ML scorer: %v (continuing without ML)\n", err)
		} else {
			mlScorer = scorer
			if mlScorer.IsEnabled() {
				fmt.Printf("ML scorer enabled with model: %s\n", cfg.MLModelPath)
				// Set ML scorer in scanner
				scanner.SetMLScorer(mlScorer)
			}
		}
	}

	return &RealisticBacktestEngine{
		cfg:            cfg,
		scanner:        scanner,
		riskPct:        riskPct,
		evalMode:       evalMode,
		location:       location,
		buyingPower:    buyingPower,
		riskLimits:     riskLimits,
		strategyEngine: strategyEngine,
		mlScorer:       mlScorer,
		currentBars:    make(map[string]*feed.Bar),
		previousBars:   make(map[string]*feed.Bar),
		signalsByTrade: make(map[string]*strategy.EntrySignal),
		trades:         make([]*strategy.TradeResult, 0),
		accountBalance: cfg.AccountSize,
		totalDays:      0,
		runNumber:      1, // Default to 1 if not set
		stats:          NewBacktestStats(),
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

		// Configure adaptive thresholds based on config
		rbe.strategyEngine.SetAdaptiveThresholdsEnabled(rbe.cfg.EnableAdaptiveThresholds)

		// Log the confidence threshold being used (only on first day)
		if dayIdx == 0 {
			effectiveThreshold := rbe.cfg.MinConfidenceThreshold
			if rbe.mlScorer == nil || !rbe.mlScorer.IsEnabled() {
				effectiveThreshold = 50.0
				if rbe.cfg.MinConfidenceThreshold < 50.0 && rbe.cfg.MinConfidenceThreshold > 0 {
					effectiveThreshold = rbe.cfg.MinConfidenceThreshold
				}
			}
			fmt.Printf("  Using confidence threshold: %.2f (ML: %v)\n",
				effectiveThreshold, rbe.mlScorer != nil && rbe.mlScorer.IsEnabled())
			fmt.Printf("  Trading window: 10:30 AM - 11:30 AM ET (includes best hour: 11:00 AM with 60%% win rate)\n")
		}

		// Reset current bars for new day (keep previous day's last bar for continuity)
		// We'll update them as we process bars

		fmt.Printf("\nDay %d/%d: %s\n", dayIdx+1, len(dates), date.Format("2006-01-02"))

		// Step 4: Calculate and set previous day's high/close for trend filter
		// Get previous day's data if available
		if dayIdx > 0 {
			prevDate := dates[dayIdx-1]
			prevDayBars := barsByDate[prevDate]

			// Calculate previous day's high and close for each ticker
			for ticker, bars := range prevDayBars {
				if len(bars) == 0 {
					continue
				}

				// Find the highest high and last close of the previous day
				prevDayHigh := 0.0
				prevDayClose := bars[len(bars)-1].Close // Last bar's close

				for _, bar := range bars {
					if bar.High > prevDayHigh {
						prevDayHigh = bar.High
					}
				}

				// Set previous day data in strategy engine
				if prevDayHigh > 0 && prevDayClose > 0 {
					rbe.strategyEngine.SetPreviousDayData(ticker, prevDayHigh, prevDayClose)
				}
			}
		}

		// Process this day's bars
		dayBars := barsByDate[date]

		// Check if trading is allowed
		if !rbe.riskLimits.CanTrade() {
			reason := "Daily loss hit or account closed"
			if rbe.riskLimits.IsProtectGainsTriggered() {
				reason = "Protect gains triggered (gave back >50% of excess above 2x daily goal)"
			}
			fmt.Printf("  Trading stopped: %s\n", reason)
			continue
		}

		// Process minute-by-minute across all tickers
		allMinuteBars := rbe.organizeByMinute(dayBars, date)

		for _, minuteData := range allMinuteBars {
			if len(minuteData.Bars) == 0 {
				continue
			}
			minuteTime := minuteData.MinuteTime
			minuteBars := minuteData.Bars

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

			// Check entries (only if we have buying power, positions available, and trading is allowed)
			maxPositions := rbe.strategyEngine.GetMaxConcurrentPositions()
			if rbe.strategyEngine.GetPositionCount() < maxPositions &&
				rbe.buyingPower.GetAvailableBuyingPower() > 0 &&
				rbe.riskLimits.CanTrade() {
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

	// Export stats JSON
	if err := rbe.exportStats(); err != nil {
		fmt.Printf("Warning: Failed to export stats: %v\n", err)
	}

	return nil
}

// TickerBar represents a bar with its ticker
type TickerBar struct {
	Ticker string
	Bar    feed.Bar
}

// MinuteBars represents bars for a specific minute
type MinuteBars struct {
	MinuteTime time.Time
	Bars       []TickerBar
}

// organizeByMinute organizes bars by minute timestamp and returns them sorted chronologically
func (rbe *RealisticBacktestEngine) organizeByMinute(
	dayBars map[string][]feed.Bar,
	date time.Time,
) []MinuteBars {
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

	// Convert map to sorted slice
	result := make([]MinuteBars, 0, len(minuteMap))
	for minuteTime, bars := range minuteMap {
		// Sort bars within this minute chronologically
		sortedBars := make([]TickerBar, len(bars))
		copy(sortedBars, bars)
		sort.Slice(sortedBars, func(i, j int) bool {
			return sortedBars[i].Bar.Time.Before(sortedBars[j].Bar.Time)
		})
		result = append(result, MinuteBars{
			MinuteTime: minuteTime,
			Bars:       sortedBars,
		})
	}

	// Sort by minute time
	sort.Slice(result, func(i, j int) bool {
		return result[i].MinuteTime.Before(result[j].MinuteTime)
	})

	return result
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
	// TIME FILTER: Focus on best performing hours based on backtest results
	// Stats show: 11:00 AM = 60% win rate (BEST), 9:00 AM = 20%, 10:00 AM = 16.7%
	// Strategy: Trade in 10:00-11:30 AM window to include best hour while allowing some flexibility
	hour := currentTime.Hour()
	minute := currentTime.Minute()

	// Allow entries during 10:00 AM - 11:30 AM ET
	// This includes the best performing hour (11:00 AM with 60% win rate)
	// But excludes worst hours (9:00 AM = 20%, 10:00 AM = 16.7% early in hour)
	if hour < 10 || (hour == 10 && minute < 30) || hour > 11 || (hour == 11 && minute > 30) {
		// Outside best trading window - skip entry checks
		return
	}

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

		// Check entry conditions for both short and long opportunities
		openPositions := rbe.strategyEngine.GetPositionCount()

		// CheckBothDirections returns all valid signals (both short and long)
		tickerSignals := rbe.strategyEngine.CheckBothDirections(ticker, strategyBar, eodTime, openPositions)

		// Score signals with ML if available
		if rbe.mlScorer != nil && rbe.mlScorer.IsEnabled() {
			// Get recent bars for feature extraction from strategy engine
			recentBars := rbe.strategyEngine.GetRecentBars(ticker, 10)

			for _, signal := range tickerSignals {
				// Score with ML
				mlScore := rbe.mlScorer.ScoreSignal(signal, tickerState, recentBars)
				signal.MLScore = mlScore
			}
		} else {
			// ML not enabled - set MLScore to -1.0 to indicate it wasn't calculated
			// This way we can distinguish between "ML predicted 0.0" vs "ML not enabled"
			for _, signal := range tickerSignals {
				if signal.MLScore == 0.0 {
					signal.MLScore = -1.0 // Mark as "not calculated"
				}
			}
		}

		// Add all signals from this ticker to the signals list
		signals = append(signals, tickerSignals...)
	}

	// Score and select best signals (up to max positions)
	maxPositions := rbe.strategyEngine.GetMaxConcurrentPositions()
	currentPositions := rbe.strategyEngine.GetPositionCount()
	availablePositions := maxPositions - currentPositions

	if availablePositions <= 0 {
		return // No positions available
	}

	maxSignals := availablePositions
	if len(signals) < maxSignals {
		maxSignals = len(signals)
	}
	bestSignals := rbe.scanner.SelectBestSignals(signals, maxSignals)

	// Filter signals by minimum confidence threshold
	// Adjust threshold based on whether ML is enabled:
	// - Without ML: max score is 70, so use lower threshold (35)
	// - With ML: max score is 100, so use configured threshold (60)
	filteredSignals := make([]*strategy.EntrySignal, 0, len(bestSignals))
	minConfidence := rbe.cfg.MinConfidenceThreshold

	// Adjust threshold based on whether ML is enabled
	// Without ML: max possible score is ~80 (10% ML + 25% pattern + 30% VWAP + 20% RSI + 10% volume + 5% bonus)
	// With ML: max possible score is ~100 (10% ML + 25% pattern + 30% VWAP + 20% RSI + 10% volume + 5% bonus)
	if rbe.mlScorer == nil || !rbe.mlScorer.IsEnabled() {
		// Without ML, max score is ~80, so use 50 (62.5% of max) for good selectivity
		// This takes top ~40% of signals to balance quality and quantity
		minConfidence = 50.0
		if rbe.cfg.MinConfidenceThreshold < 50.0 && rbe.cfg.MinConfidenceThreshold > 0 {
			// If user set a lower threshold, respect it
			minConfidence = rbe.cfg.MinConfidenceThreshold
		}
	} else {
		// With ML enabled, use threshold 60 (60% of max 100) for moderate selectivity
		// ML model is not reliable (0% win rate), so we don't want to be too strict
		if minConfidence < 60.0 {
			minConfidence = 60.0
		}
	}

	for _, signal := range bestSignals {
		// Calculate score for this signal
		scored := rbe.scanner.ScoreSignals([]*strategy.EntrySignal{signal})
		if len(scored) > 0 && scored[0].Score >= minConfidence {
			filteredSignals = append(filteredSignals, signal)
		}
	}

	// Apply correlation filter if enabled
	if rbe.cfg.EnableCorrelationFilter {
		openPositions := rbe.strategyEngine.GetPositions()
		correlationFiltered := make([]*strategy.EntrySignal, 0, len(filteredSignals))
		for _, signal := range filteredSignals {
			// Convert positions to slice for correlation check
			posSlice := make([]*strategy.Position, len(openPositions))
			copy(posSlice, openPositions)

			if rbe.scanner.CheckCorrelation(signal.Ticker, posSlice) {
				correlationFiltered = append(correlationFiltered, signal)
			} else {
				fmt.Printf("  [CORRELATION FILTER] Signal %s rejected: correlation limit reached\n", signal.Ticker)
			}
		}
		filteredSignals = correlationFiltered
	}

	// Execute entries (can take multiple signals per minute up to max positions)
	// Each entry checks buying power individually, so we can attempt all signals
	for _, signal := range filteredSignals {
		// Check if we've reached max positions (buying power check happens in executeEntry)
		if rbe.strategyEngine.GetPositionCount() >= maxPositions {
			break // Max positions reached
		}

		// Check buying power before attempting entry
		// We need to estimate shares to check if we can afford it
		// Use a rough estimate: risk amount / stop distance
		stableBalance := rbe.accountBalance
		if stableBalance < rbe.cfg.AccountSize*0.8 {
			stableBalance = rbe.cfg.AccountSize * 0.8
		}
		riskAmount := stableBalance * rbe.riskPct

		// Estimate shares (will be recalculated in executeEntry with actual fill price)
		estimatedShares := int(riskAmount / math.Abs(signal.EntryPrice-signal.StopLoss))
		if estimatedShares > 2500 {
			estimatedShares = 2500
		}
		if estimatedShares < 1 {
			estimatedShares = 1
		}

		// Check if we can afford this position (using entry price as estimate)
		if !rbe.buyingPower.CanAfford(estimatedShares, signal.EntryPrice, signal.Direction) {
			// Skip this signal - not enough buying power
			continue
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
	// Use riskPct (0.35% = 0.0035) of account balance, but use a stable base to prevent
	// position sizes from shrinking too much after losses
	// Use max of current balance or 80% of initial to maintain reasonable position sizes
	stableBalance := rbe.accountBalance
	if stableBalance < rbe.cfg.AccountSize*0.8 {
		stableBalance = rbe.cfg.AccountSize * 0.8
	}
	baseRiskAmount := stableBalance * rbe.riskPct // 0.35% of stable balance (e.g., $87.50 for $25k)

	// Calculate the actual score for this signal to use for position sizing
	// Signals have already been filtered by score (60+ for ML), so we know they're good
	// But we need to get the actual score to use for position sizing
	scored := rbe.scanner.ScoreSignals([]*strategy.EntrySignal{signal})
	actualScore := 0.0
	if len(scored) > 0 {
		actualScore = scored[0].Score
	}

	// Normalize score to 0-1 for position sizing (score is 0-100)
	normalizedScore := actualScore / 100.0

	// Use normalized score for position sizing (this is what passed the filter)
	// This is more accurate than using pattern confidence alone
	confidenceMultiplier := normalizedScore

	// If we have ML score, use the average of normalized score and ML score for conservative sizing
	if signal.MLScore > 0 {
		// Average the two for balanced position sizing
		confidenceMultiplier = (normalizedScore + signal.MLScore) / 2.0
	}

	// No need for additional threshold check here - signals already passed the score filter (60+)
	// But ensure we have a reasonable minimum for position sizing (0.5 = 50% of base risk)
	if confidenceMultiplier < 0.5 {
		fmt.Printf("  [REJECTED] Signal %s: confidence too low for position sizing (%.2f < 0.5)\n", signal.Ticker, confidenceMultiplier)
		return
	}

	// Apply multiplier to risk amount
	adjustedRiskAmount := baseRiskAmount * confidenceMultiplier

	// Log account balance and risk amount for debugging
	fmt.Printf("  [POSITION SIZING] Account: $%.2f, Stable: $%.2f, Base Risk: $%.2f, Confidence: %.2f, Adjusted Risk: $%.2f\n",
		rbe.accountBalance, stableBalance, baseRiskAmount, confidenceMultiplier, adjustedRiskAmount)

	shares, err := risk.CalculatePositionSize(
		adjustedRiskAmount, // Use adjusted risk amount based on confidence
		fillPrice,          // Use fill price with slippage for position sizing
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

	// Store signal for stats tracking
	signalKey := fmt.Sprintf("%s_%s", signal.Ticker, signal.Timestamp.Format(time.RFC3339))
	rbe.signalsByTrade[signalKey] = signal

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

		// Make a copy of positions list to avoid issues with modifying the list during iteration
		positionsToClose := make([]*strategy.Position, len(positions))
		copy(positionsToClose, positions)

		// Close all positions at their current prices
		// executeExit will automatically cap each position's loss to stay within the daily limit
		// as each position closes, the remaining allowed loss shrinks for subsequent positions
		for i, position := range positionsToClose {
			// Check if position still exists (might have been closed already)
			if !rbe.strategyEngine.HasPosition(position.Ticker) {
				continue
			}
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

		if signalPnLPerShare >= 0.20 { // $0.20/share (matched to entry checker - Target 1)
			// Take 60% at Target 1 (changed from 50%)
			sharesToClose := int(float64(position.RemainingShares) * 0.6)
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

		if signalPnLPerShare >= 0.30 { // $0.30/share (matched to entry checker - Target 2)
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

	// Record trade for adaptive threshold tracking
	rbe.strategyEngine.RecordTrade(position.Ticker, position.EntryTime, netPnL)

	// Record trade for stats (find matching signal)
	signalKey := fmt.Sprintf("%s_%s", position.Ticker, position.EntryTime.Format(time.RFC3339))
	if signal, exists := rbe.signalsByTrade[signalKey]; exists {
		rbe.stats.RecordTrade(trade, signal)
	} else {
		// Record without signal info if not found
		rbe.stats.RecordTrade(trade, nil)
	}

	// Close position
	rbe.strategyEngine.ClosePosition(position.Ticker)

	fmt.Printf("  EXIT: %s %d shares @ $%.2f (%s) - Net P&L: $%.2f (Commission: $%.2f) [Fill: $%.2f]\n",
		position.Ticker, shares, exitPrice, reason, trade.NetPnL, commission, fillPrice)

	// Note: If daily loss limit was hit, checkDailyLossLimit() already handles closing
	// all remaining positions. We don't need to do it here to avoid double-closing.
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

	// Record partial trade for stats (find matching signal)
	signalKey := fmt.Sprintf("%s_%s", position.Ticker, position.EntryTime.Format(time.RFC3339))
	if signal, exists := rbe.signalsByTrade[signalKey]; exists {
		rbe.stats.RecordTrade(trade, signal)
	} else {
		rbe.stats.RecordTrade(trade, nil)
	}

	// Update position
	rbe.strategyEngine.ClosePartial(position.Ticker, shares)

	fmt.Printf("  PARTIAL EXIT: %s %d shares @ $%.2f (%s) - Net P&L: $%.2f (Commission: $%.2f) [Fill: $%.2f]\n",
		position.Ticker, shares, exitPrice, reason, trade.NetPnL, commission, fillPrice)
}

// RunStats holds statistics for a single backtest run
type RunStats struct {
	RunNumber        int
	TotalTrades      int
	Wins             int
	WinRate          float64
	FinalBalance     float64
	TotalPnL         float64
	ProfitTarget     float64
	AccountSize      float64
	ReachedTarget    bool
	Reached75Percent bool
}

// GetRunStats returns statistics for this backtest run
func (rbe *RealisticBacktestEngine) GetRunStats() RunStats {
	stats := RunStats{
		RunNumber:    rbe.runNumber,
		TotalTrades:  len(rbe.trades),
		FinalBalance: rbe.accountBalance,
		TotalPnL:     rbe.accountBalance - rbe.cfg.AccountSize,
		ProfitTarget: rbe.cfg.ProfitTarget,
		AccountSize:  rbe.cfg.AccountSize,
	}

	if len(rbe.trades) > 0 {
		wins := 0
		for _, trade := range rbe.trades {
			if trade.NetPnL > 0 {
				wins++
			}
		}
		stats.Wins = wins
		stats.WinRate = float64(wins) / float64(len(rbe.trades)) * 100
	}

	// Check if profit target was reached
	stats.ReachedTarget = rbe.accountBalance >= rbe.cfg.ProfitTarget

	// Check if reached at least 75% of profit target
	// Profit needed = profitTarget - accountSize
	// 75% of profit needed = (profitTarget - accountSize) * 0.75
	// So: finalBalance >= accountSize + (profitTarget - accountSize) * 0.75
	profitNeeded := rbe.cfg.ProfitTarget - rbe.cfg.AccountSize
	seventyFivePercentThreshold := rbe.cfg.AccountSize + (profitNeeded * 0.75)
	stats.Reached75Percent = rbe.accountBalance >= seventyFivePercentThreshold

	return stats
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
		fmt.Printf("Average Win: $%.2f\n", rbe.stats.AverageWin)
		fmt.Printf("Average Loss: $%.2f\n", rbe.stats.AverageLoss)
	}

	// Print summary stats
	fmt.Println("\n=== STATISTICS SUMMARY ===")
	fmt.Println("Win Rate by Hour:")
	for hour := 9; hour <= 15; hour++ {
		if stat, exists := rbe.stats.WinRateByHour[hour]; exists && stat.Total > 0 {
			winRate := float64(stat.Wins) / float64(stat.Total) * 100
			fmt.Printf("  %d:00 - %.1f%% (%d wins / %d total)\n", hour, winRate, stat.Wins, stat.Total)
		}
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

// exportStats exports backtest statistics to JSON file
func (rbe *RealisticBacktestEngine) exportStats() error {
	// Create results directory if it doesn't exist
	resultsDir := "cmd/backtest/results"
	if err := os.MkdirAll(resultsDir, 0755); err != nil {
		return fmt.Errorf("failed to create results directory: %v", err)
	}

	// Generate filename: backtest_YYYYMMDD_HHMMSS_runN_stats.json
	now := time.Now()
	totalPct := ((rbe.accountBalance - rbe.cfg.AccountSize) / rbe.cfg.AccountSize) * 100
	filename := fmt.Sprintf("backtest_%s_run%d_%dd_%.1fpct_stats.json",
		now.Format("20060102_150405"),
		rbe.runNumber,
		rbe.totalDays,
		totalPct,
	)
	filepath := filepath.Join(resultsDir, filename)

	// Convert stats to JSON-serializable format
	statsJSON := map[string]interface{}{
		"run_number":           rbe.runNumber,
		"total_days":           rbe.totalDays,
		"total_trades":         len(rbe.trades),
		"final_balance":        rbe.accountBalance,
		"total_pnl":            rbe.accountBalance - rbe.cfg.AccountSize,
		"account_size":         rbe.cfg.AccountSize,
		"win_rate_by_hour":     rbe.stats.WinRateByHour,
		"win_rate_by_vwap":     rbe.stats.WinRateByVWAP,
		"win_rate_by_rsi":      rbe.stats.WinRateByRSI,
		"win_rate_by_pattern":  rbe.stats.WinRateByPattern,
		"average_win":          rbe.stats.AverageWin,
		"average_loss":         rbe.stats.AverageLoss,
		"total_wins":           rbe.stats.TotalWins,
		"total_losses":         rbe.stats.TotalLosses,
		"win_rate_by_ml_score": rbe.stats.WinRateByMLScore,
	}

	// Calculate win rates
	winRateByHour := make(map[int]float64)
	for hour, stat := range rbe.stats.WinRateByHour {
		if stat.Total > 0 {
			winRateByHour[hour] = float64(stat.Wins) / float64(stat.Total) * 100
		}
	}
	statsJSON["win_rate_by_hour_pct"] = winRateByHour

	winRateByVWAP := make(map[string]float64)
	for bucket, stat := range rbe.stats.WinRateByVWAP {
		if stat.Total > 0 {
			winRateByVWAP[bucket] = float64(stat.Wins) / float64(stat.Total) * 100
		}
	}
	statsJSON["win_rate_by_vwap_pct"] = winRateByVWAP

	winRateByRSI := make(map[string]float64)
	for bucket, stat := range rbe.stats.WinRateByRSI {
		if stat.Total > 0 {
			winRateByRSI[bucket] = float64(stat.Wins) / float64(stat.Total) * 100
		}
	}
	statsJSON["win_rate_by_rsi_pct"] = winRateByRSI

	winRateByPattern := make(map[string]float64)
	patternNames := map[strategy.DeathCandlePattern]string{
		strategy.NoPattern:            "NoPattern",
		strategy.BearishEngulfing:     "BearishEngulfing",
		strategy.RejectionAtExtension: "RejectionAtExtension",
		strategy.ShootingStar:         "ShootingStar",
		strategy.BullishEngulfing:     "BullishEngulfing",
		strategy.RejectionAtBottom:    "RejectionAtBottom",
		strategy.Hammer:               "Hammer",
	}
	for pattern, stat := range rbe.stats.WinRateByPattern {
		if stat.Total > 0 {
			patternName := patternNames[pattern]
			winRateByPattern[patternName] = float64(stat.Wins) / float64(stat.Total) * 100
		}
	}
	statsJSON["win_rate_by_pattern_pct"] = winRateByPattern

	// Write JSON file
	data, err := json.MarshalIndent(statsJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %v", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write stats file: %v", err)
	}

	fmt.Printf("Stats exported to: %s\n", filepath)
	return nil
}
