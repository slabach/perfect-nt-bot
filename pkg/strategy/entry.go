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
		// Priority 2 Fix: Balanced entry filters for quality and frequency
		vwapExtensionThreshold: 0.42, // Balanced at 0.42x ATR (between 0.40 and 0.45 for quality/frequency balance)
		rsiThreshold:          52.0,  // Balanced at 52.0 (between 51.0 and 53.0 for quality/frequency balance)
		minVolumeMA:           0.57,   // Balanced at 0.57x average (between 0.55 and 0.60 for quality/frequency balance)
		target1Profit:         0.15,  // $0.15/share for first target (larger targets to overcome commissions)
		target2Profit:         0.25,  // $0.25/share for second target (better risk/reward)
		// Phase 1 Fix #3: Tightened stop loss further for better risk/reward
		atrStopMultiplier:    0.9,   // Reduced from 1.0x to 0.9x ATR (tighter stops for better risk/reward)
		maxConcurrentPositions: 6,   // Increased from 5 to 6 for more opportunities
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
	// Allow small negative movements (>= -$0.08) to account for noise and capture reversals
	if !previousBar.Time.IsZero() {
		priceMomentum := currentBar.Close - previousBar.Close
		// For shorts, we want positive momentum (price going up, away from VWAP)
		// Allow small negative movements (>= -$0.08/share) to account for market noise and capture early reversals
		// But filter out clear reversals where price is moving back toward VWAP
		if priceMomentum < -0.08 {
			return nil, fmt.Errorf("price moving back toward VWAP (price change: %.4f, need: >= -0.08 for short entry)", priceMomentum)
		}
	}
	
	// Phase 2 Fix #4: Prefer death candle pattern but allow strong setups without pattern
	// Pattern requirement was too strict - making it optional but preferred
	// Entries without pattern need stronger VWAP extension and RSI to compensate, OR strong volume
	if pattern == NoPattern {
		vwapExtension := GetVWAPExtension(currentPrice, indicators.VWAP, indicators.ATR)
		
		// Check if volume is strong (above 0.85x average) - lower threshold for more opportunities
		strongVolume := indicators.VolumeMA > 0 && float64(currentBar.Volume) > indicators.VolumeMA*0.85
		
		// If volume is strong, allow entry with normal thresholds
		if strongVolume {
			// Strong volume compensates for no pattern - use normal thresholds
			// This will be checked in CheckEntryConditions, so we just allow it here
		} else {
			// Without strong volume, require only slightly higher VWAP extension and RSI
			// Reduced thresholds from 0.55x/56 to 0.50x/54 to capture more opportunities
			requiredExtension := 0.50 // Reduced from 0.55x (still higher than normal 0.40x threshold)
			requiredRSI := 54.0       // Reduced from 56.0 (still higher than normal 51 threshold)
			
			if vwapExtension < requiredExtension || indicators.RSI < requiredRSI {
				return nil, fmt.Errorf("no death candle pattern detected - requires stronger setup (VWAP: %.2f>=%.2f ATR, RSI: %.1f>=%.1f) or strong volume", 
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

// GetMaxConcurrentPositions returns the maximum concurrent positions allowed
func (ec *EntryChecker) GetMaxConcurrentPositions() int {
	return ec.maxConcurrentPositions
}
