package strategy

// SimulateSlippage simulates realistic fill prices with slippage
// Conservative slippage model: assume we don't get perfect fills
// For shorts:
//   - Entry (sell short): Worst case = lower price (we sell for less, reducing profit potential)
//   - Exit (buy to cover): Worst case = higher price (we buy for more, reducing profit)
// For longs:
//   - Entry (buy): Worst case = higher price (we buy for more, reducing profit potential)
//   - Exit (sell): Worst case = lower price (we sell for less, reducing profit)
func SimulateSlippage(bar Bar, direction string, isEntry bool) float64 {
	// Conservative slippage: 30% of the bar range, applied in the worst-case direction
	barRange := bar.High - bar.Low
	
	if direction == "SHORT" {
		if isEntry {
			// Selling short - worst case is getting lower price (we sell for less)
			// Apply slippage downward from close
			slippage := barRange * 0.3
			fillPrice := bar.Close - slippage
			// Ensure we don't go below the bar's low
			if fillPrice < bar.Low {
				fillPrice = bar.Low
			}
			return fillPrice
		} else {
			// Buying to cover - worst case is getting higher price (we buy for more)
			// Apply slippage upward from close
			slippage := barRange * 0.3
			fillPrice := bar.Close + slippage
			// Ensure we don't go above the bar's high
			if fillPrice > bar.High {
				fillPrice = bar.High
			}
			return fillPrice
		}
	}
	
	// For longs:
	if isEntry {
		// Buying - worst case is getting higher price (we buy for more)
		slippage := barRange * 0.3
		fillPrice := bar.Close + slippage
		if fillPrice > bar.High {
			fillPrice = bar.High
		}
		return fillPrice
	} else {
		// Selling - worst case is getting lower price (we sell for less)
		slippage := barRange * 0.3
		fillPrice := bar.Close - slippage
		if fillPrice < bar.Low {
			fillPrice = bar.Low
		}
		return fillPrice
	}
}

// GetFillPrice gets realistic fill price for a trade
// Uses close price with conservative slippage simulation
func GetFillPrice(bar Bar, direction string, isEntry bool) float64 {
	// Add small slippage to be more realistic
	return SimulateSlippage(bar, direction, isEntry)
}

