package strategy

import (
	"time"
)

// ExitChecker checks if exit conditions are met for a position
type ExitChecker struct {
	target1Profit    float64 // First target profit per share (e.g., 0.15-0.20)
	target2Profit    float64 // Second target profit per share (e.g., 0.25-0.30)
	minProfitPerShare float64 // Minimum profit to count (e.g., 0.10)
	trailingStopOffset float64 // Trailing stop offset (e.g., 0.05)
	timeDecayWindow1Hours float64 // Hours before first time decay window (triggers profit check)
	timeDecayWindow2Hours float64 // Hours before second time decay window (force exit)
	breakevenMinutes  float64 // Minutes before moving to breakeven (e.g., 30)
	earlyExitHour     int     // Hour to exit if not profitable (e.g., 15 for 3:30 PM ET)
	earlyExitMinute   int     // Minute to exit if not profitable (e.g., 30)
}

// NewExitChecker creates a new exit checker
func NewExitChecker() *ExitChecker {
	return &ExitChecker{
		target1Profit:    0.20,  // $0.20/share for first target (increased to overcome commissions)
		target2Profit:    0.30,  // $0.30/share for second target (better risk/reward)
		minProfitPerShare: 0.12, // Increased from 0.10 to require minimum profit after commissions
		trailingStopOffset: 0.12, // Increased from 0.10 to 0.12 to avoid premature exits
		timeDecayWindow1Hours: 1.0, // First window: start checking for profit > $0.12/share
		timeDecayWindow2Hours: 2.0, // Second window: force exit regardless of profit
		breakevenMinutes:  20.0, // Move to breakeven after 20 minutes (increased from 15 to give more time)
		earlyExitHour:     15,   // Exit by 3:30 PM ET if not profitable
		earlyExitMinute:   30,
	}
}

// CheckExitConditions checks if any exit conditions are met for a position
func (ec *ExitChecker) CheckExitConditions(
	position *Position,
	currentBar Bar,
	eodTime time.Time,
) (bool, ExitReason, float64) {
	currentPrice := currentBar.Close
	currentTime := currentBar.Time

	// Calculate current P&L per share
	var pnlPerShare float64
	if position.Direction == "SHORT" {
		pnlPerShare = position.EntryPrice - currentPrice
	} else {
		pnlPerShare = currentPrice - position.EntryPrice
	}

	// Priority 1 Fix: Improved EOD Exit Logic
	// Exit ALL unprofitable positions by 12:30 PM (150 minutes before EOD)
	// This prevents positions from being held all day and losing money at EOD
	unprofitableExitTime := eodTime.Add(-150 * time.Minute) // 12:30 PM
	if currentTime.After(unprofitableExitTime) || currentTime.Equal(unprofitableExitTime) {
		if pnlPerShare <= 0 {
			// Not profitable by 1:30 PM - exit now to avoid EOD losses
			return true, ExitReasonTimeDecay, currentPrice
		}
		// Profitable by 1:30 PM - allow to hold until EOD or target
	}

	// Check EOD (3:50 PM ET) - must close all remaining positions
	if currentTime.After(eodTime) || currentTime.Equal(eodTime) {
		return true, ExitReasonEOD, currentPrice
	}

	// Early exit: If it's after 3:30 PM ET and trade is not profitable, exit
	// (This is redundant now but kept as safety net)
	if ec.shouldEarlyExit(currentTime, pnlPerShare) {
		return true, ExitReasonTimeDecay, currentPrice
	}

	// Check stop loss
	if ec.isStopLossHit(position, currentPrice) {
		return true, ExitReasonStopLoss, currentPrice
	}

	// Check breakeven stop: Move stop to breakeven after 30 minutes if not profitable
	if ec.shouldMoveToBreakeven(position, currentTime, pnlPerShare) {
		if position.TrailingStop == nil {
			// Move stop to breakeven (entry price for short, entry price for long)
			breakevenStop := position.EntryPrice
			position.TrailingStop = &breakevenStop
		}
	}

	// Phase 1 Fix #2: Disable Trailing Stops temporarily
	// Trailing stops are losing money (0% win rate, -$128.50 total P&L)
	// Let Target 1/2 handle profit taking instead
	// 
	// Check trailing stop (DISABLED - commented out for now)
	// if position.TrailingStop != nil {
	// 	if ec.isTrailingStopHit(position, currentPrice) {
	// 		return true, ExitReasonTrailingStop, *position.TrailingStop
	// 	}
	// 	// Update trailing stop if profitable
	// 	ec.updateTrailingStop(position, pnlPerShare)
	// }

	// Check target 1 (partial exit - already handled in position management)
	// This is for remaining shares
	if !position.FilledTarget1 {
		if ec.isTarget1Hit(position, currentPrice) {
			return true, ExitReasonTarget1, currentPrice
		}
	}

	// Check target 2
	if position.FilledTarget1 && !position.FilledTarget2 {
		if ec.isTarget2Hit(position, currentPrice) {
			return true, ExitReasonTarget2, currentPrice
		}
	}

	// Check time decay (two-window system)
	if shouldExit, reason := ec.isTimeDecayHit(position, currentTime, pnlPerShare); shouldExit {
		return true, reason, currentPrice
	}

	return false, "", 0
}

// isStopLossHit checks if stop loss is hit
func (ec *ExitChecker) isStopLossHit(position *Position, currentPrice float64) bool {
	if position.Direction == "SHORT" {
		// For shorts, stop is above entry
		return currentPrice >= position.StopLoss
	}
	// For longs, stop is below entry
	return currentPrice <= position.StopLoss
}

// isTarget1Hit checks if first target is hit
func (ec *ExitChecker) isTarget1Hit(position *Position, currentPrice float64) bool {
	if position.Direction == "SHORT" {
		return currentPrice <= position.Target1
	}
	return currentPrice >= position.Target1
}

// isTarget2Hit checks if second target is hit
func (ec *ExitChecker) isTarget2Hit(position *Position, currentPrice float64) bool {
	if position.Direction == "SHORT" {
		return currentPrice <= position.Target2
	}
	return currentPrice >= position.Target2
}

// updateTrailingStop updates trailing stop if profitable
func (ec *ExitChecker) updateTrailingStop(position *Position, pnlPerShare float64) {
	// Only activate trailing stop after target 1 is hit (to avoid premature exits)
	if !position.FilledTarget1 {
		return
	}
	
	// Only trail if profitable above minimum threshold
	if pnlPerShare < ec.minProfitPerShare {
		return
	}

	if position.Direction == "SHORT" {
		// For shorts, trail from entry price downward
		newTrailingStop := position.EntryPrice - ec.trailingStopOffset
		if position.TrailingStop == nil || newTrailingStop < *position.TrailingStop {
			position.TrailingStop = &newTrailingStop
		}
	} else {
		// For longs, trail from entry price upward
		newTrailingStop := position.EntryPrice + ec.trailingStopOffset
		if position.TrailingStop == nil || newTrailingStop > *position.TrailingStop {
			position.TrailingStop = &newTrailingStop
		}
	}
}

// isTrailingStopHit checks if trailing stop is hit
func (ec *ExitChecker) isTrailingStopHit(position *Position, currentPrice float64) bool {
	if position.TrailingStop == nil {
		return false
	}

	if position.Direction == "SHORT" {
		// For shorts, trailing stop is below entry (stop loss moves down)
		// If price goes above trailing stop, we exit
		return currentPrice >= *position.TrailingStop
	}
	// For longs, trailing stop is above entry
	// If price goes below trailing stop, we exit
	return currentPrice <= *position.TrailingStop
}

// isTimeDecayHit checks if time decay exit condition is met (two-window system)
// Returns (shouldExit, reason)
func (ec *ExitChecker) isTimeDecayHit(position *Position, currentTime time.Time, pnlPerShare float64) (bool, ExitReason) {
	duration := currentTime.Sub(position.EntryTime)
	hoursOpen := duration.Hours()

	// Window 2: Force exit regardless of profit
	if hoursOpen >= ec.timeDecayWindow2Hours {
		return true, ExitReasonTimeDecay
	}

	// Window 1: Start checking if profit > $0.10/share
	if hoursOpen >= ec.timeDecayWindow1Hours {
		// Mark that we've hit window 1
		if !position.TimeDecayWindow1Hit {
			position.TimeDecayWindow1Hit = true
		}

		// If we're in window 1 and profit > $0.10/share, exit
		if pnlPerShare >= ec.minProfitPerShare {
			return true, ExitReasonTimeDecay
		}
	}

	return false, ""
}

// shouldMoveToBreakeven checks if we should move stop to breakeven after 30 minutes
func (ec *ExitChecker) shouldMoveToBreakeven(position *Position, currentTime time.Time, pnlPerShare float64) bool {
	// Only move to breakeven if not already profitable
	if pnlPerShare > 0 {
		return false
	}

	// Check if position has been open for more than breakevenMinutes
	duration := currentTime.Sub(position.EntryTime)
	minutesOpen := duration.Minutes()

	if minutesOpen >= ec.breakevenMinutes {
		return true
	}

	return false
}

// shouldEarlyExit checks if we should exit early (by 3:30 PM ET) if trade is not profitable
func (ec *ExitChecker) shouldEarlyExit(currentTime time.Time, pnlPerShare float64) bool {
	// Only exit early if not profitable
	if pnlPerShare > 0 {
		return false
	}

	// Check if it's after 3:30 PM ET
	hour := currentTime.Hour()
	minute := currentTime.Minute()

	// If hour is after early exit hour, exit
	if hour > ec.earlyExitHour {
		return true
	}

	// If hour matches and minute is >= early exit minute, exit
	if hour == ec.earlyExitHour && minute >= ec.earlyExitMinute {
		return true
	}

	return false
}

// CalculateCommission calculates commission for a trade
func CalculateCommission(shares int) float64 {
	commission := float64(shares) * 0.005 // $0.005 per share
	
	// Minimum commission is $0.75
	if commission < 0.75 {
		return 0.75
	}
	
	return commission
}

// CalculatePnL calculates P&L for a trade
func CalculatePnL(entryPrice, exitPrice float64, shares int, direction string) float64 {
	var pnl float64
	
	if direction == "SHORT" {
		pnl = (entryPrice - exitPrice) * float64(shares)
	} else {
		pnl = (exitPrice - entryPrice) * float64(shares)
	}
	
	return pnl
}

// CalculateNetPnL calculates net P&L (after commissions)
func CalculateNetPnL(entryPrice, exitPrice float64, shares int, direction string) (float64, float64) {
	// Calculate entry and exit commissions
	entryCommission := CalculateCommission(shares)
	exitCommission := CalculateCommission(shares)
	totalCommission := entryCommission + exitCommission

	// Calculate gross P&L
	grossPnL := CalculatePnL(entryPrice, exitPrice, shares, direction)

	// Apply profit threshold rule: winning trades < $0.10/share don't count profit
	// (only commissions count)
	pnlPerShare := grossPnL / float64(shares)
	if grossPnL > 0 && pnlPerShare < 0.10 {
		// Winning trade but less than $0.10/share - profit doesn't count
		netPnL := -totalCommission // Only commissions count (negative)
		return netPnL, totalCommission
	}

	// Normal case: net P&L = gross P&L - commissions
	netPnL := grossPnL - totalCommission
	return netPnL, totalCommission
}
