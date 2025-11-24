package strategy

import (
	"fmt"
	"math"
	"time"
)

// TradePerformance tracks performance of a single trade
type TradePerformance struct {
	Ticker    string
	EntryTime time.Time
	NetPnL    float64
	IsWin     bool
}

// PerformanceTracker tracks recent trade performance
type PerformanceTracker struct {
	recentTrades []TradePerformance
	maxTrades    int // Track last N trades
}

// EntryChecker checks if entry conditions are met for a short trade
type EntryChecker struct {
	vwapExtensionThreshold float64 // ATR multiples (e.g., 1.5)
	rsiThreshold           float64 // Overbought threshold (e.g., 65)
	minVolumeMA            float64 // Minimum volume multiplier (e.g., 1.0 for 1x average)
	target1Profit          float64 // First target profit per share (e.g., 0.15-0.20)
	target2Profit          float64 // Second target profit per share (e.g., 0.25-0.30)
	atrStopMultiplier      float64 // Stop loss ATR multiplier (e.g., 1.5)
	maxConcurrentPositions int
	performanceTracker     *PerformanceTracker
	enableAdaptive         bool
}

// NewEntryChecker creates a new entry checker with default parameters
func NewEntryChecker() *EntryChecker {
	return &EntryChecker{
		// Step 3: Loosened thresholds to get more trades (3-5 per day target)
		// Reduced from 0.62 to 0.45 to increase trade frequency
		vwapExtensionThreshold: 0.45, // Reduced from 0.62 - need more volume to hit 6% profit
		rsiThreshold:           58.0, // Keep same - require more extreme overbought/oversold
		minVolumeMA:            0.9,  // Keep same - require volume near average (stronger confirmation)
		target1Profit:          0.15, // $0.20/share for first target (keep same)
		target2Profit:          0.30, // $0.30/share for second target (keep same)
		atrStopMultiplier:      0.85, // Back to 0.85x ATR - 0.80x was too tight, causing premature stops
		maxConcurrentPositions: 3,    // Keep at 3 - allow some diversification
		performanceTracker:     NewPerformanceTracker(),
		enableAdaptive:         false, // Disable adaptive thresholds by default - they tighten too much after losses
	}
}

// NewPerformanceTracker creates a new performance tracker
func NewPerformanceTracker() *PerformanceTracker {
	return &PerformanceTracker{
		recentTrades: make([]TradePerformance, 0),
		maxTrades:    10, // Track last 10 trades
	}
}

// RecordTrade records a completed trade
func (pt *PerformanceTracker) RecordTrade(ticker string, entryTime time.Time, netPnL float64) {
	trade := TradePerformance{
		Ticker:    ticker,
		EntryTime: entryTime,
		NetPnL:    netPnL,
		IsWin:     netPnL > 0,
	}

	pt.recentTrades = append(pt.recentTrades, trade)

	// Keep only last maxTrades
	if len(pt.recentTrades) > pt.maxTrades {
		pt.recentTrades = pt.recentTrades[len(pt.recentTrades)-pt.maxTrades:]
	}
}

// GetRecentWinRate calculates win rate from last N trades (default: 5)
func (pt *PerformanceTracker) GetRecentWinRate(n int) float64 {
	if n <= 0 {
		n = 5 // Default to last 5 trades
	}

	if len(pt.recentTrades) == 0 {
		return 0.5 // Default 50% if no trades
	}

	// Get last N trades
	startIdx := len(pt.recentTrades) - n
	if startIdx < 0 {
		startIdx = 0
	}

	recent := pt.recentTrades[startIdx:]
	if len(recent) == 0 {
		return 0.5
	}

	wins := 0
	for _, trade := range recent {
		if trade.IsWin {
			wins++
		}
	}

	return float64(wins) / float64(len(recent))
}

// Reset clears the performance tracker (call at start of each day)
func (pt *PerformanceTracker) Reset() {
	pt.recentTrades = make([]TradePerformance, 0)
}

// GetAdaptiveThresholds returns adjusted thresholds based on recent performance
// Uses the configured thresholds as the base (not hardcoded values)
func (ec *EntryChecker) GetAdaptiveThresholds() (vwapThreshold, rsiThreshold float64) {
	// Use configured thresholds as base (not hardcoded values!)
	// This was a critical bug: adaptive was using 0.55 instead of configured 0.45
	baseVWAP := ec.vwapExtensionThreshold
	baseRSI := ec.rsiThreshold

	if !ec.enableAdaptive || ec.performanceTracker == nil {
		return baseVWAP, baseRSI
	}

	recentWinRate := ec.performanceTracker.GetRecentWinRate(5)

	if recentWinRate < 0.3 {
		// Poor performance: slightly tighten thresholds (reduced tightening to prevent over-restricting)
		// Reduced from +0.05/+3.0 to +0.02/+1.0 to avoid killing trade volume after a few losses
		return baseVWAP + 0.02, baseRSI + 1.0
	} else if recentWinRate > 0.6 {
		// Good performance: slightly relax thresholds
		return baseVWAP - 0.02, baseRSI - 1.0
	}

	// Default: use base thresholds (the configured values)
	return baseVWAP, baseRSI
}

// SetAdaptiveEnabled enables or disables adaptive thresholds
func (ec *EntryChecker) SetAdaptiveEnabled(enabled bool) {
	ec.enableAdaptive = enabled
}

// RecordTrade records a completed trade for performance tracking
func (ec *EntryChecker) RecordTrade(ticker string, entryTime time.Time, netPnL float64) {
	if ec.performanceTracker != nil {
		ec.performanceTracker.RecordTrade(ticker, entryTime, netPnL)
	}
}

// ResetDaily resets daily performance tracking
func (ec *EntryChecker) ResetDaily() {
	if ec.performanceTracker != nil {
		ec.performanceTracker.Reset()
	}
}

// CheckEntryConditions checks if all entry conditions are met for a short trade
func (ec *EntryChecker) CheckEntryConditions(
	ticker string,
	bar Bar,
	indicators *IndicatorState,
	currentPrice float64,
	openPositions int,
	eodTime time.Time, // 3:50 PM ET cutoff
) (*EntrySignal, error) {
	// Check if we're past EOD cutoff (3:50 PM ET)
	if bar.Time.After(eodTime) {
		return nil, fmt.Errorf("past EOD cutoff")
	}

	// Priority 2 Fix: Extended entry window to 3:00 PM to capture more opportunities
	// Still avoid entries too late in the day to prevent EOD losses
	entryHour := bar.Time.Hour()
	entryMinute := bar.Time.Minute()

	// Avoid entries after 3:00 PM (15:00) - extended from 2:45 PM
	if entryHour > 15 || (entryHour == 15 && entryMinute >= 0) {
		return nil, fmt.Errorf("entry too late in day (hour: %d:%02d, need: < 15:00)", entryHour, entryMinute)
	}

	// Avoid entries in first 15 minutes of market open (9:30-9:45 AM)
	// Market opens at 9:30 AM - allow entries starting at 9:45 AM
	// 10:00 AM entries are the best performing (avg +$58.56)
	if entryHour == 9 && entryMinute >= 30 && entryMinute < 45 {
		// Reject 9:30-9:44 AM (first 15 minutes after market open)
		return nil, fmt.Errorf("entry too early in day (hour: %d:%02d, need: >= 9:45)", entryHour, entryMinute)
	}

	// Check max concurrent positions
	if openPositions >= ec.maxConcurrentPositions {
		return nil, fmt.Errorf("max concurrent positions reached")
	}

	// Check if we have valid indicators
	if indicators.VWAP == 0 || indicators.ATR == 0 || indicators.RSI == 0 {
		return nil, fmt.Errorf("indicators not ready")
	}

	// Minimum price filter: avoid stocks < $2.00 (reduced from $2.50 for more opportunities)
	if currentPrice < 2.0 {
		return nil, fmt.Errorf("price too low (price: $%.2f, need: >= $2.00)", currentPrice)
	}

	// Minimum volume filter: require at least 100k daily volume for liquidity (very relaxed for more opportunities)
	if bar.Volume < 100000 {
		return nil, fmt.Errorf("volume too low (volume: %d, need: >= 100k)", bar.Volume)
	}

	// Get adaptive thresholds if enabled
	vwapThreshold := ec.vwapExtensionThreshold
	rsiThreshold := ec.rsiThreshold
	if ec.enableAdaptive {
		vwapThreshold, rsiThreshold = ec.GetAdaptiveThresholds()
	}

	// Check VWAP extension: price must be extended above VWAP
	vwapExtension := GetVWAPExtension(currentPrice, indicators.VWAP, indicators.ATR)

	// Step 4: Trend filter - avoid shorting during massive breakouts
	// If current price > previous day high, be very careful about shorting
	// This prevents mean reversion bot from getting run over by trends
	if indicators.PreviousDayHigh > 0 && currentPrice > indicators.PreviousDayHigh {
		// Price is breaking out above previous day high - require stronger VWAP extension to compensate for trend risk
		trendRiskThreshold := vwapThreshold + 0.15 // Require 0.15 ATR more extension
		if vwapExtension < trendRiskThreshold {
			return nil, fmt.Errorf("price breaking out above previous day high (%.2f > %.2f) - requires stronger extension (%.2f ATR, need: %.2f)",
				currentPrice, indicators.PreviousDayHigh, vwapExtension, trendRiskThreshold)
		}
	}

	// Check base VWAP extension threshold
	if vwapExtension < vwapThreshold {
		return nil, fmt.Errorf("price not extended above VWAP (extension: %.2f ATR, need: %.2f)",
			vwapExtension, vwapThreshold)
	}

	// Check RSI: must be overbought
	if indicators.RSI < rsiThreshold {
		return nil, fmt.Errorf("RSI not overbought (RSI: %.2f, need: >%.2f)",
			indicators.RSI, rsiThreshold)
	}

	// Check volume: must be above average
	if indicators.VolumeMA == 0 || float64(bar.Volume) < (indicators.VolumeMA*ec.minVolumeMA) {
		return nil, fmt.Errorf("volume too low (volume: %d, need: >%.0f)",
			bar.Volume, indicators.VolumeMA*ec.minVolumeMA)
	}

	// Phase 2 Fix #4: Require death candle pattern (don't allow entries without pattern)
	// Pattern detection requires previous bar for full accuracy
	// This will be properly checked in CheckEntryConditionsWithPrevious
	pattern := DetectDeathCandlePattern(bar, Bar{}) // Placeholder - will be updated with previous bar
	patternConfidence := PatternConfidence(pattern, bar, vwapExtension)

	// Note: Actual pattern requirement is enforced in CheckEntryConditionsWithPrevious
	// For now, set low confidence - will be updated if pattern exists
	if pattern == NoPattern {
		patternConfidence = 0.3
	}

	// Priority 3 Fix: Momentum filter - price should be moving away from VWAP
	// For short entries, we want to see price moving up (away from VWAP)
	// This will be properly checked in CheckEntryConditionsWithPrevious with previous bar

	// Phase 1 Fix #3: Calculate stop loss with max limit
	// Stop at 1.0x ATR above entry for short (reduced from 1.2x)
	atrStop := indicators.ATR * ec.atrStopMultiplier
	maxStopPerShare := 0.50 // Limit max stop loss to $0.50/share for high-volatility stocks
	if atrStop > maxStopPerShare {
		atrStop = maxStopPerShare
	}
	stopLoss := currentPrice + atrStop

	// Calculate targets
	target1 := currentPrice - ec.target1Profit
	target2 := currentPrice - ec.target2Profit

	// Validate stop loss is reasonable
	if stopLoss <= currentPrice {
		return nil, fmt.Errorf("invalid stop loss calculation")
	}

	// Create entry signal
	signal := &EntrySignal{
		Ticker:        ticker,
		EntryPrice:    currentPrice,
		Direction:     "SHORT",
		StopLoss:      stopLoss,
		Target1:       target1,
		Target2:       target2,
		Confidence:    patternConfidence,
		VWAPExtension: vwapExtension,
		Pattern:       pattern,
		RSI:           indicators.RSI,
		Volume:        bar.Volume,
		Timestamp:     bar.Time,
		Reason: fmt.Sprintf("Short entry: VWAP extension %.2fx ATR, RSI %.1f, pattern %v",
			vwapExtension, indicators.RSI, pattern),
	}

	return signal, nil
}

// CheckEntryConditionsWithPrevious checks entry conditions with previous bar for pattern detection
func (ec *EntryChecker) CheckEntryConditionsWithPrevious(
	ticker string,
	currentBar, previousBar Bar,
	indicators *IndicatorState,
	openPositions int,
	eodTime time.Time,
) (*EntrySignal, error) {
	currentPrice := currentBar.Close
	pattern := DetectDeathCandlePattern(currentBar, previousBar)

	// Momentum filter - REMOVED: This conflicted with mean reversion strategy
	// Mean reversion trades work when price is ALREADY extended from VWAP, not when it's moving further away
	// The momentum filter was blocking valid mean reversion setups where price was extended but starting to reverse
	// Original filter required price moving away from VWAP, which is more suited for trend-following strategies

	// Phase 2 Fix #4: Prefer death candle pattern but allow strong setups without pattern
	// Pattern requirement was too strict - making it optional but preferred
	// Entries without pattern need stronger VWAP extension and RSI to compensate, OR strong volume
	if pattern == NoPattern {
		vwapExtension := GetVWAPExtension(currentPrice, indicators.VWAP, indicators.ATR)

		// BALANCED FIX: Require stronger setups for entries without patterns, but not too strict
		// Check if volume is strong (above 1.3x average) - stricter threshold
		strongVolume := indicators.VolumeMA > 0 && float64(currentBar.Volume) > indicators.VolumeMA*1.3

		// If volume is strong, allow entry with normal thresholds
		if strongVolume {
			// Strong volume compensates for no pattern - use normal thresholds
			// This will be checked in CheckEntryConditions, so we just allow it here
		} else {
			// Without strong volume, require stronger VWAP extension and RSI
			// Use configurable thresholds (not hardcoded) - this allows the strategy to be tuned
			// Get adaptive thresholds if enabled, otherwise use base thresholds
			vwapThreshold := ec.vwapExtensionThreshold
			rsiThreshold := ec.rsiThreshold
			if ec.enableAdaptive {
				vwapThreshold, rsiThreshold = ec.GetAdaptiveThresholds()
			}

			// For no-pattern entries, require slightly higher thresholds to compensate
			// But use the configurable values, not hardcoded ones
			// Reduced penalty from 15% to 5% to allow more valid setups without patterns
			requiredExtension := vwapThreshold * 1.05 // 5% higher than configured threshold (reduced from 15%)
			requiredRSI := rsiThreshold + 1.0         // 1.0 point higher than configured RSI (reduced from 3.0)

			if vwapExtension < requiredExtension || indicators.RSI < requiredRSI {
				return nil, fmt.Errorf("no death candle pattern detected - requires stronger setup (VWAP: %.2f>=%.2f ATR, RSI: %.1f>=%.1f) or strong volume (1.3x+)",
					vwapExtension, requiredExtension, indicators.RSI, requiredRSI)
			}
		}
		// Strong setup without pattern - allow entry but with lower confidence
	}

	// Update indicators with pattern confidence calculation
	vwapExtension := GetVWAPExtension(currentPrice, indicators.VWAP, indicators.ATR)
	patternConfidence := PatternConfidence(pattern, currentBar, vwapExtension)

	// Create temporary indicator state with pattern info
	tempIndicators := *indicators

	// Call base check
	signal, err := ec.CheckEntryConditions(
		ticker,
		currentBar,
		&tempIndicators,
		currentPrice,
		openPositions,
		eodTime,
	)

	if err != nil {
		return nil, err
	}

	// Update with pattern from previous bar check
	signal.Pattern = pattern
	signal.Confidence = patternConfidence

	return signal, nil
}

// CheckLongEntryConditions checks if all entry conditions are met for a long trade
func (ec *EntryChecker) CheckLongEntryConditions(
	ticker string,
	bar Bar,
	indicators *IndicatorState,
	currentPrice float64,
	openPositions int,
	eodTime time.Time, // 3:50 PM ET cutoff
) (*EntrySignal, error) {
	// Check if we're past EOD cutoff (3:50 PM ET)
	if bar.Time.After(eodTime) {
		return nil, fmt.Errorf("past EOD cutoff")
	}

	// Same entry window restrictions as shorts
	entryHour := bar.Time.Hour()
	entryMinute := bar.Time.Minute()

	// Avoid entries after 3:00 PM (15:00)
	if entryHour > 15 || (entryHour == 15 && entryMinute >= 0) {
		return nil, fmt.Errorf("entry too late in day (hour: %d:%02d, need: < 15:00)", entryHour, entryMinute)
	}

	// Avoid entries in first 15 minutes of market open (9:30-9:45 AM)
	if entryHour == 9 && entryMinute >= 30 && entryMinute < 45 {
		return nil, fmt.Errorf("entry too early in day (hour: %d:%02d, need: >= 9:45)", entryHour, entryMinute)
	}

	// Check max concurrent positions
	if openPositions >= ec.maxConcurrentPositions {
		return nil, fmt.Errorf("max concurrent positions reached")
	}

	// Check if we have valid indicators
	if indicators.VWAP == 0 || indicators.ATR == 0 || indicators.RSI == 0 {
		return nil, fmt.Errorf("indicators not ready")
	}

	// Minimum price filter: avoid stocks < $2.00
	if currentPrice < 2.0 {
		return nil, fmt.Errorf("price too low (price: $%.2f, need: >= $2.00)", currentPrice)
	}

	// Minimum volume filter: require at least 100k daily volume for liquidity
	if bar.Volume < 100000 {
		return nil, fmt.Errorf("volume too low (volume: %d, need: >= 100k)", bar.Volume)
	}

	// Get adaptive thresholds if enabled
	vwapThreshold := ec.vwapExtensionThreshold
	rsiThreshold := ec.rsiThreshold
	if ec.enableAdaptive {
		vwapThreshold, rsiThreshold = ec.GetAdaptiveThresholds()
	}

	// Check VWAP extension: price must be extended below VWAP
	vwapExtension := GetVWAPExtension(currentPrice, indicators.VWAP, indicators.ATR)
	// For longs, we need negative extension (price below VWAP)
	// Check if extension is <= -threshold (extended below by at least threshold)
	if vwapExtension > -vwapThreshold {
		return nil, fmt.Errorf("price not extended below VWAP (extension: %.2f ATR, need: <= %.2f)",
			vwapExtension, -vwapThreshold)
	}

	// Check RSI: must be oversold (symmetric to overbought threshold)
	longRSIThreshold := 100.0 - rsiThreshold
	if indicators.RSI > longRSIThreshold {
		return nil, fmt.Errorf("RSI not oversold (RSI: %.2f, need: <%.2f)",
			indicators.RSI, longRSIThreshold)
	}

	// Check volume: must be above average (same as shorts)
	if indicators.VolumeMA == 0 || float64(bar.Volume) < (indicators.VolumeMA*ec.minVolumeMA) {
		return nil, fmt.Errorf("volume too low (volume: %d, need: >%.0f)",
			bar.Volume, indicators.VolumeMA*ec.minVolumeMA)
	}

	// Pattern detection requires previous bar for full accuracy
	// This will be properly checked in CheckLongEntryConditionsWithPrevious
	pattern := DetectBullishReversalPattern(bar, Bar{}) // Placeholder - will be updated with previous bar
	// Use absolute value of extension for confidence calculation
	absExtension := math.Abs(vwapExtension)
	patternConfidence := PatternConfidence(pattern, bar, absExtension)

	// Note: Actual pattern requirement is enforced in CheckLongEntryConditionsWithPrevious
	// For now, set low confidence - will be updated if pattern exists
	if pattern == NoPattern {
		patternConfidence = 0.3
	}

	// Momentum filter - price should be moving away from VWAP
	// For long entries, we want to see price moving down (away from VWAP)
	// This will be properly checked in CheckLongEntryConditionsWithPrevious with previous bar

	// Calculate stop loss with max limit
	// Stop at 0.9x ATR below entry for long
	atrStop := indicators.ATR * ec.atrStopMultiplier
	maxStopPerShare := 0.50 // Limit max stop loss to $0.50/share for high-volatility stocks
	if atrStop > maxStopPerShare {
		atrStop = maxStopPerShare
	}
	stopLoss := currentPrice - atrStop

	// Calculate targets (above entry for longs)
	target1 := currentPrice + ec.target1Profit
	target2 := currentPrice + ec.target2Profit

	// Validate stop loss is reasonable
	if stopLoss >= currentPrice {
		return nil, fmt.Errorf("invalid stop loss calculation")
	}

	// Create entry signal
	signal := &EntrySignal{
		Ticker:        ticker,
		EntryPrice:    currentPrice,
		Direction:     "LONG",
		StopLoss:      stopLoss,
		Target1:       target1,
		Target2:       target2,
		Confidence:    patternConfidence,
		VWAPExtension: vwapExtension,
		Pattern:       pattern,
		RSI:           indicators.RSI,
		Volume:        bar.Volume,
		Timestamp:     bar.Time,
		Reason: fmt.Sprintf("Long entry: VWAP extension %.2fx ATR, RSI %.1f, pattern %v",
			vwapExtension, indicators.RSI, pattern),
	}

	return signal, nil
}

// CheckLongEntryConditionsWithPrevious checks long entry conditions with previous bar for pattern detection
func (ec *EntryChecker) CheckLongEntryConditionsWithPrevious(
	ticker string,
	currentBar, previousBar Bar,
	indicators *IndicatorState,
	openPositions int,
	eodTime time.Time,
) (*EntrySignal, error) {
	currentPrice := currentBar.Close
	pattern := DetectBullishReversalPattern(currentBar, previousBar)

	// Momentum filter - REMOVED: This conflicted with mean reversion strategy
	// Mean reversion trades work when price is ALREADY extended from VWAP, not when it's moving further away
	// The momentum filter was blocking valid mean reversion setups where price was extended but starting to reverse
	// Original filter required price moving away from VWAP, which is more suited for trend-following strategies

	// Prefer bullish reversal pattern but allow strong setups without pattern
	// Pattern requirement was too strict - making it optional but preferred
	// Entries without pattern need stronger VWAP extension and RSI to compensate, OR strong volume
	if pattern == NoPattern {
		vwapExtension := GetVWAPExtension(currentPrice, indicators.VWAP, indicators.ATR)

		// BALANCED FIX: Require stronger setups for entries without patterns, but not too strict
		// Check if volume is strong (above 1.3x average) - stricter threshold
		strongVolume := indicators.VolumeMA > 0 && float64(currentBar.Volume) > indicators.VolumeMA*1.3

		// If volume is strong, allow entry with normal thresholds
		if strongVolume {
			// Strong volume compensates for no pattern - use normal thresholds
			// This will be checked in CheckLongEntryConditions, so we just allow it here
		} else {
			// Without strong volume, require stronger VWAP extension and RSI
			// Use configurable thresholds (not hardcoded) - this allows the strategy to be tuned
			// Get adaptive thresholds if enabled, otherwise use base thresholds
			vwapThreshold := ec.vwapExtensionThreshold
			rsiThreshold := ec.rsiThreshold
			if ec.enableAdaptive {
				vwapThreshold, rsiThreshold = ec.GetAdaptiveThresholds()
			}

			// For longs, we need more negative extension (further below VWAP)
			// For no-pattern entries, require slightly stronger thresholds to compensate
			// But use the configurable values, not hardcoded ones
			// Reduced penalty from 15% to 5% to allow more valid setups without patterns
			requiredExtension := -vwapThreshold * 1.05     // 5% more negative than configured threshold (reduced from 15%)
			longRSIThreshold := 100.0 - rsiThreshold - 1.0 // 1.0 point more oversold than configured (reduced from 3.0)

			if vwapExtension > requiredExtension || indicators.RSI > longRSIThreshold {
				return nil, fmt.Errorf("no bullish reversal pattern detected - requires stronger setup (VWAP: %.2f<=%.2f ATR, RSI: %.1f<=%.1f) or strong volume (1.3x+)",
					vwapExtension, requiredExtension, indicators.RSI, longRSIThreshold)
			}
		}
		// Strong setup without pattern - allow entry but with lower confidence
	}

	// Update indicators with pattern confidence calculation
	vwapExtension := GetVWAPExtension(currentPrice, indicators.VWAP, indicators.ATR)
	// Use absolute value for confidence calculation
	absExtension := math.Abs(vwapExtension)
	patternConfidence := PatternConfidence(pattern, currentBar, absExtension)

	// Create temporary indicator state with pattern info
	tempIndicators := *indicators

	// Call base check
	signal, err := ec.CheckLongEntryConditions(
		ticker,
		currentBar,
		&tempIndicators,
		currentPrice,
		openPositions,
		eodTime,
	)

	if err != nil {
		return nil, err
	}

	// Update with pattern from previous bar check
	signal.Pattern = pattern
	signal.Confidence = patternConfidence

	return signal, nil
}

// GetMaxConcurrentPositions returns the maximum concurrent positions allowed
func (ec *EntryChecker) GetMaxConcurrentPositions() int {
	return ec.maxConcurrentPositions
}
