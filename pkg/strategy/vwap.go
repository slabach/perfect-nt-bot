package strategy

import (
	"time"
)

// VWAPCalculator calculates Volume Weighted Average Price
type VWAPCalculator struct {
	volumeSum    float64
	priceVolumeSum float64
	resetTime    time.Time
	dailyBars    []Bar
}

// NewVWAPCalculator creates a new VWAP calculator
func NewVWAPCalculator() *VWAPCalculator {
	return &VWAPCalculator{
		dailyBars: make([]Bar, 0),
	}
}

// Reset resets VWAP calculation (call at market open 9:30 AM ET)
func (v *VWAPCalculator) Reset(marketOpen time.Time) {
	v.volumeSum = 0
	v.priceVolumeSum = 0
	v.resetTime = marketOpen
	v.dailyBars = v.dailyBars[:0]
}

// Update adds a new bar and updates VWAP
func (v *VWAPCalculator) Update(bar Bar, marketOpen time.Time) {
	// Reset if this is a new trading day
	if !v.resetTime.IsZero() && bar.Time.Before(v.resetTime) {
		v.Reset(marketOpen)
	}

	// Skip bars before market open
	if bar.Time.Before(marketOpen) {
		return
	}

	// Calculate typical price (high + low + close) / 3
	typicalPrice := (bar.High + bar.Low + bar.Close) / 3.0

	// Accumulate volume and price * volume
	v.volumeSum += float64(bar.Volume)
	v.priceVolumeSum += typicalPrice * float64(bar.Volume)

	v.dailyBars = append(v.dailyBars, bar)
}

// GetVWAP returns the current VWAP value
func (v *VWAPCalculator) GetVWAP() float64 {
	if v.volumeSum == 0 {
		return 0
	}
	return v.priceVolumeSum / v.volumeSum
}

// GetVWAPExtension calculates how many ATRs the price is above/below VWAP
func GetVWAPExtension(price, vwap, atr float64) float64 {
	if atr == 0 {
		return 0
	}
	return (price - vwap) / atr
}

// IsPriceExtendedAboveVWAP checks if price is extended above VWAP by threshold ATRs
func IsPriceExtendedAboveVWAP(price, vwap, atr, thresholdATR float64) bool {
	extension := GetVWAPExtension(price, vwap, atr)
	return extension >= thresholdATR
}

// IsPriceExtendedBelowVWAP checks if price is extended below VWAP by threshold ATRs
func IsPriceExtendedBelowVWAP(price, vwap, atr, thresholdATR float64) bool {
	extension := GetVWAPExtension(price, vwap, atr)
	return extension <= -thresholdATR
}

// GetVWAPLevel returns the VWAP level at a given ATR extension
func GetVWAPLevel(vwap, atr, atrMultiplier float64) float64 {
	return vwap + (atr * atrMultiplier)
}
