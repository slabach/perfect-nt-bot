package risk

import (
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
	lastTradeDate  time.Time
	accountBalance float64
	
	dailyLossHit      bool
	accountClosed     bool
	profitTargetHit   bool
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
		rlm.dailyLossHit = false
	}
	
	rlm.lastTradeDate = tradeDate
	rlm.dailyPnL += pnl
	rlm.accountBalance += pnl
	
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
	rlm.dailyLossHit = false
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
