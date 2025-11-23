package risk

import (
	"fmt"
	"time"
)

// RiskLimitsManager manages risk limits and daily P&L
type RiskLimitsManager struct {
	maxDailyLoss       float64
	hardStopLoss       float64
	profitTarget       float64
	accountCloseLimit  float64
	initialAccountSize float64
	
	dailyPnL       float64
	peakDailyPnL   float64  // Track peak daily P&L after hitting 2x goal
	lastTradeDate  time.Time
	accountBalance float64
	
	dailyLossHit         bool
	accountClosed        bool
	profitTargetHit      bool
	protectGainsActive   bool  // True when we've hit 2x daily goal
	protectGainsTriggered bool // True when we've given back >50% of excess and stopped trading
}

// NewRiskLimitsManager creates a new risk limits manager
func NewRiskLimitsManager(initialAccountSize, maxDailyLoss, hardStopLoss, profitTarget, accountCloseLimit float64) *RiskLimitsManager {
	return &RiskLimitsManager{
		maxDailyLoss:       maxDailyLoss,
		hardStopLoss:       hardStopLoss,
		profitTarget:       profitTarget,
		accountCloseLimit:  accountCloseLimit,
		initialAccountSize: initialAccountSize,
		accountBalance:     initialAccountSize,
	}
}

// UpdateDailyPnL updates daily P&L and resets if new day
func (rlm *RiskLimitsManager) UpdateDailyPnL(pnl float64, tradeTime time.Time) {
	// Check if this is a new trading day
	tradeDate := tradeTime.Truncate(24 * time.Hour)
	
	if !rlm.lastTradeDate.IsZero() && !tradeDate.Equal(rlm.lastTradeDate) {
		// New day, reset daily P&L
		rlm.dailyPnL = 0
		rlm.peakDailyPnL = 0
		rlm.dailyLossHit = false
		rlm.protectGainsActive = false
		rlm.protectGainsTriggered = false
	}
	
	rlm.lastTradeDate = tradeDate
	rlm.dailyPnL += pnl
	rlm.accountBalance += pnl
	
	// Calculate daily goal (0.4% of account size) and 2x goal (0.8%)
	dailyGoal := rlm.initialAccountSize * 0.004
	twoXDailyGoal := dailyGoal * 2.0
	
	// Check if we've hit 2x daily goal - start protecting gains
	if rlm.dailyPnL >= twoXDailyGoal && !rlm.protectGainsActive {
		rlm.protectGainsActive = true
		rlm.peakDailyPnL = rlm.dailyPnL
		fmt.Printf("  [PROTECT GAINS] Daily P&L ($%.2f) hit 2x daily goal ($%.2f) - protecting gains\n",
			rlm.dailyPnL, twoXDailyGoal)
	}
	
	// Update peak if we're in protection mode and P&L increased
	if rlm.protectGainsActive && rlm.dailyPnL > rlm.peakDailyPnL {
		rlm.peakDailyPnL = rlm.dailyPnL
	}
	
	// Check if we've given back more than 50% of excess above 2x goal
	if rlm.protectGainsActive && rlm.peakDailyPnL > twoXDailyGoal {
		excess := rlm.peakDailyPnL - twoXDailyGoal
		allowedGiveback := excess * 0.50 // Can give back 50% of excess
		threshold := twoXDailyGoal + (excess - allowedGiveback)
		
		if rlm.dailyPnL < threshold {
			rlm.protectGainsTriggered = true
			fmt.Printf("  [PROTECT GAINS] Daily P&L ($%.2f) dropped below threshold ($%.2f) - stopping trading\n",
				rlm.dailyPnL, threshold)
		}
	}
	
	// Check limits
	rlm.checkLimits()
}

// checkLimits checks all risk limits and updates flags
func (rlm *RiskLimitsManager) checkLimits() {
	// Check daily loss limit
	if rlm.dailyPnL <= -rlm.maxDailyLoss {
		rlm.dailyLossHit = true
	}
	
	// Check account close limit
	if rlm.accountBalance <= rlm.accountCloseLimit {
		rlm.accountClosed = true
	}
	
	// Check profit target
	if rlm.accountBalance >= rlm.profitTarget {
		rlm.profitTargetHit = true
	}
}

// ResetDailyPnL resets daily P&L (call at market open)
func (rlm *RiskLimitsManager) ResetDailyPnL() {
	rlm.dailyPnL = 0
	rlm.peakDailyPnL = 0
	rlm.dailyLossHit = false
	rlm.protectGainsActive = false
	rlm.protectGainsTriggered = false
}

// CanTrade checks if trading is allowed
func (rlm *RiskLimitsManager) CanTrade() bool {
	if rlm.accountClosed {
		return false
	}
	
	if rlm.profitTargetHit {
		return false
	}
	
	if rlm.dailyLossHit {
		return false
	}
	
	// Stop trading if we've given back >50% of excess above 2x goal
	if rlm.protectGainsTriggered {
		return false
	}
	
	return true
}

// GetDailyPnL returns current daily P&L
func (rlm *RiskLimitsManager) GetDailyPnL() float64 {
	return rlm.dailyPnL
}

// GetAccountBalance returns current account balance
func (rlm *RiskLimitsManager) GetAccountBalance() float64 {
	return rlm.accountBalance
}

// IsDailyLossHit returns true if daily loss limit has been hit
func (rlm *RiskLimitsManager) IsDailyLossHit() bool {
	return rlm.dailyLossHit
}

// IsAccountClosed returns true if account should be closed
func (rlm *RiskLimitsManager) IsAccountClosed() bool {
	return rlm.accountClosed
}

// IsProfitTargetHit returns true if profit target has been reached
func (rlm *RiskLimitsManager) IsProfitTargetHit() bool {
	return rlm.profitTargetHit
}

// GetHardStopLoss returns the hard stop loss amount per trade
func (rlm *RiskLimitsManager) GetHardStopLoss() float64 {
	return rlm.hardStopLoss
}

// GetPeakDailyPnL returns the peak daily P&L (for debugging)
func (rlm *RiskLimitsManager) GetPeakDailyPnL() float64 {
	return rlm.peakDailyPnL
}

// IsProtectGainsActive returns true if we're in protection mode
func (rlm *RiskLimitsManager) IsProtectGainsActive() bool {
	return rlm.protectGainsActive
}

// IsProtectGainsTriggered returns true if we've stopped trading due to giving back gains
func (rlm *RiskLimitsManager) IsProtectGainsTriggered() bool {
	return rlm.protectGainsTriggered
}
