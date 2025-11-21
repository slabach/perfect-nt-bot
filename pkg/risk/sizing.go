package risk

import (
	"fmt"
	"math"
)

// CalculatePositionSize calculates the number of shares to trade based on risk
// riskAmount: dollar amount to risk (e.g., $125 for 0.5% of $25k account)
// entryPrice: entry price of the trade
// stopPrice: stop loss price
// maxShares: maximum shares allowed (e.g., 2500)
func CalculatePositionSize(riskAmount, entryPrice, stopPrice float64, maxShares int) (int, error) {
	if entryPrice <= 0 {
		return 0, fmt.Errorf("entry price must be > 0")
	}

	if stopPrice <= 0 {
		return 0, fmt.Errorf("stop price must be > 0")
	}

	if riskAmount <= 0 {
		return 0, fmt.Errorf("risk amount must be > 0")
	}

	// For shorts: risk = (entryPrice - stopPrice) * shares
	// For longs: risk = (stopPrice - entryPrice) * shares
	// We'll use absolute difference to work for both directions
	riskPerShare := math.Abs(entryPrice - stopPrice)

	if riskPerShare == 0 {
		return 0, fmt.Errorf("entry and stop prices cannot be the same")
	}

	shares := int(math.Floor(riskAmount / riskPerShare))

	// Cap at max shares
	if shares > maxShares {
		shares = maxShares
	}

	// Must be at least 1 share
	if shares < 1 {
		shares = 1
	}

	return shares, nil
}

// CalculateStopLoss calculates stop loss price based on ATR
// For shorts: stop = entry + (atr * multiplier)
// For longs: stop = entry - (atr * multiplier)
func CalculateStopLoss(entryPrice, atr, atrMultiplier float64, direction string) float64 {
	if direction == "SHORT" {
		return entryPrice + (atr * atrMultiplier)
	}
	// LONG
	return entryPrice - (atr * atrMultiplier)
}

// ValidateStopLoss validates that stop loss is reasonable
func ValidateStopLoss(entryPrice, stopPrice float64, direction string) bool {
	if direction == "SHORT" {
		// For shorts, stop should be above entry
		return stopPrice > entryPrice
	}
	// For longs, stop should be below entry
	return stopPrice < entryPrice
}
