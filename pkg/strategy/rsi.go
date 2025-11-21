package strategy

// RSICalculator calculates Relative Strength Index
type RSICalculator struct {
	period      int
	gains       []float64
	losses      []float64
	avgGain     float64
	avgLoss     float64
	previousClose float64
}

// NewRSICalculator creates a new RSI calculator with specified period
func NewRSICalculator(period int) *RSICalculator {
	return &RSICalculator{
		period: period,
		gains:   make([]float64, 0, period+1),
		losses:  make([]float64, 0, period+1),
	}
}

// Update adds a new bar and updates RSI
func (r *RSICalculator) Update(bar Bar) {
	if r.previousClose == 0 {
		// First bar, just store close
		r.previousClose = bar.Close
		return
	}

	// Calculate change
	change := bar.Close - r.previousClose
	
	var gain, loss float64
	if change > 0 {
		gain = change
		loss = 0
	} else {
		gain = 0
		loss = -change // Store as positive value
	}

	// Add to arrays
	r.gains = append(r.gains, gain)
	r.losses = append(r.losses, loss)

	// Keep only last period+1 values
	if len(r.gains) > r.period+1 {
		r.gains = r.gains[len(r.gains)-(r.period+1):]
	}
	if len(r.losses) > r.period+1 {
		r.losses = r.losses[len(r.losses)-(r.period+1):]
	}

	// Calculate average gain and loss using Wilder's smoothing
	if len(r.gains) <= r.period {
		// Still accumulating, use simple average
		sumGain := 0.0
		sumLoss := 0.0
		for _, g := range r.gains {
			sumGain += g
		}
		for _, l := range r.losses {
			sumLoss += l
		}
		r.avgGain = sumGain / float64(len(r.gains))
		r.avgLoss = sumLoss / float64(len(r.losses))
	} else {
		// Use Wilder's smoothing
		r.avgGain = ((r.avgGain * float64(r.period-1)) + gain) / float64(r.period)
		r.avgLoss = ((r.avgLoss * float64(r.period-1)) + loss) / float64(r.period)
	}

	r.previousClose = bar.Close
}

// GetRSI returns the current RSI value (0-100)
func (r *RSICalculator) GetRSI() float64 {
	if r.avgLoss == 0 {
		if r.avgGain == 0 {
			return 50.0 // Neutral if no change
		}
		return 100.0 // All gains, no losses
	}

	rs := r.avgGain / r.avgLoss
	rsi := 100.0 - (100.0 / (1.0 + rs))
	return rsi
}

// IsReady returns true if RSI has enough data to be reliable
func (r *RSICalculator) IsReady() bool {
	return len(r.gains) >= r.period && len(r.losses) >= r.period
}

// IsOverbought checks if RSI indicates overbought conditions (typically > 70, we use > 65)
func (r *RSICalculator) IsOverbought(threshold float64) bool {
	return r.GetRSI() > threshold
}

// IsOversold checks if RSI indicates oversold conditions (typically < 30)
func (r *RSICalculator) IsOversold(threshold float64) bool {
	return r.GetRSI() < threshold
}

// Reset resets the RSI calculator
func (r *RSICalculator) Reset() {
	r.gains = r.gains[:0]
	r.losses = r.losses[:0]
	r.avgGain = 0
	r.avgLoss = 0
	r.previousClose = 0
}
