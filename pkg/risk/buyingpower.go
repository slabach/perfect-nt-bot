package risk

// BuyingPowerManager manages available buying power
type BuyingPowerManager struct {
	accountBalance     float64
	capitalInPositions float64
	inRegularHours     bool
}

// NewBuyingPowerManager creates a new buying power manager
func NewBuyingPowerManager(initialBalance float64, inRegularHours bool) *BuyingPowerManager {
	return &BuyingPowerManager{
		accountBalance:     initialBalance,
		capitalInPositions: 0,
		inRegularHours:     inRegularHours,
	}
}

// GetAvailableBuyingPower returns available buying power
// Regular hours: account balance - capital in positions
// Pre-market/after-hours: (account balance / 16) - capital in positions
func (bpm *BuyingPowerManager) GetAvailableBuyingPower() float64 {
	baseBalance := bpm.accountBalance
	
	if !bpm.inRegularHours {
		// Pre-market/after-hours: 1/16th of account
		baseBalance = bpm.accountBalance / 16.0
	}

	available := baseBalance - bpm.capitalInPositions
	
	if available < 0 {
		return 0
	}
	
	return available
}

// ReserveBuyingPower reserves buying power for a new position
func (bpm *BuyingPowerManager) ReserveBuyingPower(shares int, entryPrice float64, direction string) {
	// For longs: full value
	// For shorts: 50% margin requirement
	var capitalRequired float64
	
	if direction == "SHORT" {
		capitalRequired = float64(shares) * entryPrice * 0.5
	} else {
		capitalRequired = float64(shares) * entryPrice
	}
	
	bpm.capitalInPositions += capitalRequired
}

// ReleaseBuyingPower releases buying power when position is closed
func (bpm *BuyingPowerManager) ReleaseBuyingPower(shares int, entryPrice float64, direction string) {
	// Calculate same way as ReserveBuyingPower
	var capitalRequired float64
	
	if direction == "SHORT" {
		capitalRequired = float64(shares) * entryPrice * 0.5
	} else {
		capitalRequired = float64(shares) * entryPrice
	}
	
	bpm.capitalInPositions -= capitalRequired
	
	if bpm.capitalInPositions < 0 {
		bpm.capitalInPositions = 0
	}
}

// UpdateAccountBalance updates the account balance (after a trade P&L)
func (bpm *BuyingPowerManager) UpdateAccountBalance(pnl float64) {
	bpm.accountBalance += pnl
}

// GetAccountBalance returns current account balance
func (bpm *BuyingPowerManager) GetAccountBalance() float64 {
	return bpm.accountBalance
}

// SetInRegularHours sets whether we're in regular trading hours
func (bpm *BuyingPowerManager) SetInRegularHours(inRegularHours bool) {
	bpm.inRegularHours = inRegularHours
}

// CanAfford checks if we can afford a position
func (bpm *BuyingPowerManager) CanAfford(shares int, entryPrice float64, direction string) bool {
	available := bpm.GetAvailableBuyingPower()
	
	var capitalRequired float64
	if direction == "SHORT" {
		capitalRequired = float64(shares) * entryPrice * 0.5
	} else {
		capitalRequired = float64(shares) * entryPrice
	}
	
	return available >= capitalRequired
}
