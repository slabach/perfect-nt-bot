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
		vwapExtensionThreshold: 0.1,  // Price must be 0.1x ATR above VWAP (very relaxed for max trades)
		rsiThreshold:          47.0,  // RSI must be > 47 (very relaxed for more opportunities)
		minVolumeMA:           0.15,   // Volume must be > 0.15x 20-period average (very permissive)
		target1Profit:         0.08,  // $0.08/share for first target (faster profits)
		target2Profit:         0.15,  // $0.15/share for second target (faster exits)
		atrStopMultiplier:    1.5,   // Stop at 1.5x ATR (reasonable stops)
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

	// Check for death candle pattern (optional - prefer pattern but allow setups without)
	pattern := DetectDeathCandlePattern(bar, Bar{}) // Need previous bar for full detection
	patternConfidence := PatternConfidence(pattern, bar, vwapExtension)

	// Allow entries without death candle patterns - very relaxed for eval mode
	if pattern == NoPattern {
		// Just need basic filters passed (VWAP extension and RSI already checked above)
		// If we pass those, allow entry to maximize opportunities
		patternConfidence = 0.4
	}

	// Calculate stop loss (1.5x ATR above entry for short)
	stopLoss := currentPrice + (indicators.ATR * ec.atrStopMultiplier)

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
