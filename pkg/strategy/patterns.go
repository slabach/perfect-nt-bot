package strategy

// DetectDeathCandlePattern detects bearish reversal patterns
func DetectDeathCandlePattern(current, previous Bar) DeathCandlePattern {
	// Bearish Engulfing: current red candle completely engulfs previous green candle
	if isBearishEngulfing(current, previous) {
		return BearishEngulfing
	}

	// Rejection at extension: long upper wick, closes in lower half
	if isRejectionAtExtension(current) {
		return RejectionAtExtension
	}

	// Shooting Star: small body at bottom, long upper wick
	if isShootingStar(current) {
		return ShootingStar
	}

	return NoPattern
}

// isBearishEngulfing checks for bearish engulfing pattern
func isBearishEngulfing(current, previous Bar) bool {
	// Previous must be green (bullish)
	if previous.Close <= previous.Open {
		return false
	}

	// Current must be red (bearish)
	if current.Close >= current.Open {
		return false
	}

	// Current must completely engulf previous
	engulfsHigh := current.Open > previous.Close && current.Close < previous.Open
	engulfsLow := current.Close < previous.Open && current.Open > previous.Close

	return engulfsHigh && engulfsLow
}

// isRejectionAtExtension checks for rejection pattern (long upper wick)
func isRejectionAtExtension(bar Bar) bool {
	bodySize := abs(bar.Close - bar.Open)
	totalRange := bar.High - bar.Low

	if totalRange == 0 {
		return false
	}

	upperWick := bar.High - max(bar.Open, bar.Close)
	lowerWick := min(bar.Open, bar.Close) - bar.Low

	// Long upper wick relative to body (at least 2x body size)
	// Close should be in lower half of range
	hasLongUpperWick := upperWick >= (bodySize * 2.0)
	closesLowerHalf := bar.Close <= (bar.High + bar.Low) / 2.0

	return hasLongUpperWick && closesLowerHalf && upperWick > lowerWick
}

// isShootingStar checks for shooting star pattern
func isShootingStar(bar Bar) bool {
	bodySize := abs(bar.Close - bar.Open)
	totalRange := bar.High - bar.Low

	if totalRange == 0 {
		return false
	}

	upperWick := bar.High - max(bar.Open, bar.Close)
	lowerWick := min(bar.Open, bar.Close) - bar.Low

	// Small body at bottom (body < 30% of range)
	// Long upper wick (upper wick > 50% of range)
	// Minimal lower wick
	smallBody := bodySize < (totalRange * 0.3)
	longUpperWick := upperWick > (totalRange * 0.5)
	minimalLowerWick := lowerWick < (totalRange * 0.2)

	return smallBody && longUpperWick && minimalLowerWick
}

// PatternConfidence returns a confidence score (0-1) for a pattern
func PatternConfidence(pattern DeathCandlePattern, bar Bar, vwapExtension float64) float64 {
	baseConfidence := 0.0

	switch pattern {
	case BearishEngulfing:
		baseConfidence = 0.7
	case RejectionAtExtension:
		baseConfidence = 0.6
	case ShootingStar:
		baseConfidence = 0.5
	case BullishEngulfing:
		baseConfidence = 0.7
	case RejectionAtBottom:
		baseConfidence = 0.6
	case Hammer:
		baseConfidence = 0.5
	default:
		return 0.0
	}

	// Boost confidence if price is extended far from VWAP
	// Use absolute value for longs (negative extension means below VWAP)
	absExtension := abs(vwapExtension)
	if absExtension > 2.0 {
		baseConfidence += 0.2
	} else if absExtension > 1.5 {
		baseConfidence += 0.1
	}

	// Cap at 1.0
	if baseConfidence > 1.0 {
		baseConfidence = 1.0
	}

	return baseConfidence
}

// DetectBullishReversalPattern detects bullish reversal patterns
func DetectBullishReversalPattern(current, previous Bar) DeathCandlePattern {
	// Bullish Engulfing: current green candle completely engulfs previous red candle
	if isBullishEngulfing(current, previous) {
		return BullishEngulfing
	}

	// Rejection at bottom: long lower wick, closes in upper half
	if isRejectionAtBottom(current) {
		return RejectionAtBottom
	}

	// Hammer: small body at top, long lower wick
	if isHammer(current) {
		return Hammer
	}

	return NoPattern
}

// isBullishEngulfing checks for bullish engulfing pattern
func isBullishEngulfing(current, previous Bar) bool {
	// Previous must be red (bearish)
	if previous.Close >= previous.Open {
		return false
	}

	// Current must be green (bullish)
	if current.Close <= current.Open {
		return false
	}

	// Current must completely engulf previous
	// Current opens below previous close AND current closes above previous open
	engulfsBody := current.Open < previous.Close && current.Close > previous.Open
	// Current's range completely engulfs previous's range
	engulfsRange := current.High > previous.High && current.Low < previous.Low

	return engulfsBody && engulfsRange
}

// isRejectionAtBottom checks for rejection pattern (long lower wick)
func isRejectionAtBottom(bar Bar) bool {
	bodySize := abs(bar.Close - bar.Open)
	totalRange := bar.High - bar.Low

	if totalRange == 0 {
		return false
	}

	upperWick := bar.High - max(bar.Open, bar.Close)
	lowerWick := min(bar.Open, bar.Close) - bar.Low

	// Long lower wick relative to body (at least 2x body size)
	// Close should be in upper half of range
	hasLongLowerWick := lowerWick >= (bodySize * 2.0)
	closesUpperHalf := bar.Close >= (bar.High + bar.Low) / 2.0

	return hasLongLowerWick && closesUpperHalf && lowerWick > upperWick
}

// isHammer checks for hammer pattern
func isHammer(bar Bar) bool {
	bodySize := abs(bar.Close - bar.Open)
	totalRange := bar.High - bar.Low

	if totalRange == 0 {
		return false
	}

	upperWick := bar.High - max(bar.Open, bar.Close)
	lowerWick := min(bar.Open, bar.Close) - bar.Low

	// Small body at top (body < 30% of range)
	// Long lower wick (lower wick > 50% of range)
	// Minimal upper wick
	smallBody := bodySize < (totalRange * 0.3)
	longLowerWick := lowerWick > (totalRange * 0.5)
	minimalUpperWick := upperWick < (totalRange * 0.2)

	return smallBody && longLowerWick && minimalUpperWick
}

// Helper functions
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
