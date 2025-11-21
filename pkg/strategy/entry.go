package strategy

import (
	"fmt"
	"time"
)

// EntryChecker checks if entry conditions are met for a short trade
type EntryChecker struct {
	vwapExtensionThreshold float64 // ATR multiples (e.g., 1.5)
	rsiThreshold          float64 // Overbought threshold (e.g., 65)
	minVolumeMA           float64 // Minimum volume multiplier (e.g., 1.0 for 1x average)
	target1Profit         float64 // First target profit per share (e.g., 0.15-0.20)
	target2Profit         float64 // Second target profit per share (e.g., 0.25-0.30)
	atrStopMultiplier    float64 // Stop loss ATR multiplier (e.g., 1.5)
	maxConcurrentPositions int
}

// NewEntryChecker creates a new entry checker with default parameters
func NewEntryChecker() *EntryChecker {
	return &EntryChecker{
		// Priority 2 Fix: Relaxed entry filters to increase trade frequency
		vwapExtensionThreshold: 0.55, // Relaxed from 0.6x to 0.55x ATR to capture more opportunities
		rsiThreshold:          57.0,  // Relaxed from 58 to 57 to capture more opportunities
		minVolumeMA:           0.7,   // Relaxed from 0.75x to 0.7x average to capture more opportunities
		target1Profit:         0.15,  // $0.15/share for first target (larger targets to overcome commissions)
		target2Profit:         0.25,  // $0.25/share for second target (better risk/reward)
		// Phase 1 Fix #3: Tightened stop loss
		atrStopMultiplier:    1.0,   // Reduced from 1.2x to 1.0x ATR (tighter stops for better risk/reward)
		maxConcurrentPositions: 3,   // More positions for more opportunities
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

	// Priority 2 Fix: Extended entry window to 2:00 PM to capture more opportunities
	// Still avoid entries too late in the day to prevent EOD losses
	entryHour := bar.Time.Hour()
	entryMinute := bar.Time.Minute()
	
	// Avoid entries after 2:00 PM (14:00) - extended from 1:30 PM
	if entryHour > 14 || (entryHour == 14 && entryMinute >= 0) {
		return nil, fmt.Errorf("entry too late in day (hour: %d:%02d, need: < 14:00)", entryHour, entryMinute)
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

	// Minimum price filter: avoid stocks < $3 (relaxed from $5 for more opportunities)
	if currentPrice < 3.0 {
		return nil, fmt.Errorf("price too low (price: $%.2f, need: >= $3.00)", currentPrice)
	}

	// Minimum volume filter: require at least 100k daily volume for liquidity (very relaxed for more opportunities)
	if bar.Volume < 100000 {
		return nil, fmt.Errorf("volume too low (volume: %d, need: >= 100k)", bar.Volume)
	}

	// Check VWAP extension: price must be extended above VWAP
	vwapExtension := GetVWAPExtension(currentPrice, indicators.VWAP, indicators.ATR)
	if vwapExtension < ec.vwapExtensionThreshold {
		return nil, fmt.Errorf("price not extended above VWAP (extension: %.2f ATR, need: %.2f)", 
			vwapExtension, ec.vwapExtensionThreshold)
	}

	// Check RSI: must be overbought
	if indicators.RSI < ec.rsiThreshold {
		return nil, fmt.Errorf("RSI not overbought (RSI: %.2f, need: >%.2f)", 
			indicators.RSI, ec.rsiThreshold)
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
		Reason:        fmt.Sprintf("Short entry: VWAP extension %.2fx ATR, RSI %.1f, pattern %v", 
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
	
	// Priority 3 Fix: Momentum filter - require price moving away from VWAP
	// For short entries, we want to see price moving up (current > previous close)
	// This indicates momentum away from VWAP, making it more likely to hit Target 1
	// Allow small negative movements (>= -$0.01) to account for noise, but filter out clear reversals
	if !previousBar.Time.IsZero() {
		priceMomentum := currentBar.Close - previousBar.Close
		// For shorts, we want positive momentum (price going up, away from VWAP)
		// Allow small negative movements (>= -$0.01/share) to account for market noise
		// But filter out clear reversals where price is moving back toward VWAP
		if priceMomentum < -0.01 {
			return nil, fmt.Errorf("price moving back toward VWAP (price change: %.4f, need: >= -0.01 for short entry)", priceMomentum)
		}
	}
	
	// Phase 2 Fix #4: Prefer death candle pattern but allow strong setups without pattern
	// Pattern requirement was too strict - making it optional but preferred
	// Entries without pattern need stronger VWAP extension and RSI to compensate
	if pattern == NoPattern {
		// Allow entries without pattern IF other criteria are very strong
		// Require higher VWAP extension (0.75x vs 0.55x normal) and higher RSI (60 vs 57 normal)
		requiredExtension := 0.75 // Higher than normal 0.55x threshold
		requiredRSI := 60.0       // Higher than normal 57 threshold
		
		vwapExtension := GetVWAPExtension(currentPrice, indicators.VWAP, indicators.ATR)
		if vwapExtension < requiredExtension || indicators.RSI < requiredRSI {
			return nil, fmt.Errorf("no death candle pattern detected - requires stronger setup (VWAP: %.2f>=%.2f ATR, RSI: %.1f>=%.1f)", 
				vwapExtension, requiredExtension, indicators.RSI, requiredRSI)
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
