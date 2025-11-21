package strategy

import (
	"math"
)

// ATRCalculator calculates Average True Range
type ATRCalculator struct {
	period      int
	trueRanges  []float64
	atr         float64
	previousClose float64
}

// NewATRCalculator creates a new ATR calculator with specified period
func NewATRCalculator(period int) *ATRCalculator {
	return &ATRCalculator{
		period:     period,
		trueRanges: make([]float64, 0, period+1),
	}
}

// Update adds a new bar and updates ATR
func (a *ATRCalculator) Update(bar Bar) {
	tr := a.calculateTrueRange(bar)
	
	if a.previousClose == 0 {
		// First bar, just store close
		a.previousClose = bar.Close
		a.trueRanges = append(a.trueRanges, tr)
		return
	}

	// Add new true range
	a.trueRanges = append(a.trueRanges, tr)
	
	// Keep only last period+1 values
	if len(a.trueRanges) > a.period+1 {
		a.trueRanges = a.trueRanges[len(a.trueRanges)-(a.period+1):]
	}

	// Calculate ATR using Wilder's smoothing method
	if len(a.trueRanges) <= a.period {
		// Still accumulating, use simple average
		sum := 0.0
		for _, tr := range a.trueRanges {
			sum += tr
		}
		a.atr = sum / float64(len(a.trueRanges))
	} else {
		// Use Wilder's smoothing: ATR = (Previous ATR * (Period - 1) + Current TR) / Period
		a.atr = ((a.atr * float64(a.period-1)) + tr) / float64(a.period)
	}

	a.previousClose = bar.Close
}

// calculateTrueRange calculates True Range for a bar
func (a *ATRCalculator) calculateTrueRange(bar Bar) float64 {
	var tr float64

	if a.previousClose == 0 {
		// First bar, TR is just high - low
		tr = bar.High - bar.Low
	} else {
		// TR = max(High - Low, |High - PreviousClose|, |Low - PreviousClose|)
		tr1 := bar.High - bar.Low
		tr2 := math.Abs(bar.High - a.previousClose)
		tr3 := math.Abs(bar.Low - a.previousClose)
		
		tr = math.Max(tr1, math.Max(tr2, tr3))
	}

	return tr
}

// GetATR returns the current ATR value
func (a *ATRCalculator) GetATR() float64 {
	return a.atr
}

// IsReady returns true if ATR has enough data to be reliable
func (a *ATRCalculator) IsReady() bool {
	return len(a.trueRanges) >= a.period
}

// Reset resets the ATR calculator
func (a *ATRCalculator) Reset() {
	a.trueRanges = a.trueRanges[:0]
	a.atr = 0
	a.previousClose = 0
}
