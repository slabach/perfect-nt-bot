package strategy

// PositionManager manages open positions
type PositionManager struct {
	positions map[string]*Position // ticker -> position
}

// NewPositionManager creates a new position manager
func NewPositionManager() *PositionManager {
	return &PositionManager{
		positions: make(map[string]*Position),
	}
}

// OpenPosition opens a new position
func (pm *PositionManager) OpenPosition(signal *EntrySignal, shares int) *Position {
	position := &Position{
		Ticker:          signal.Ticker,
		EntryPrice:      signal.EntryPrice,
		Shares:          shares,
		RemainingShares: shares,
		Direction:       signal.Direction,
		EntryTime:       signal.Timestamp,
		StopLoss:        signal.StopLoss,
		Target1:         signal.Target1,
		Target2:         signal.Target2,
		FilledTarget1:   false,
		FilledTarget2:   false,
		TimeDecayWindow1Hit: false, // Initialize time decay window tracking
		StrategyState: &IndicatorState{
			VWAP:     signal.VWAPExtension, // Store extension, not VWAP itself
			ATR:      0,                     // Will need to be updated
			RSI:      signal.RSI,
			VolumeMA: float64(signal.Volume),
			LastUpdate: signal.Timestamp,
		},
		Pattern: signal.Pattern,
	}

	pm.positions[signal.Ticker] = position
	return position
}

// ClosePosition closes a position
func (pm *PositionManager) ClosePosition(ticker string) *Position {
	position, exists := pm.positions[ticker]
	if !exists {
		return nil
	}

	delete(pm.positions, ticker)
	return position
}

// GetPosition returns a position for a ticker
func (pm *PositionManager) GetPosition(ticker string) (*Position, bool) {
	position, exists := pm.positions[ticker]
	return position, exists
}

// GetAllPositions returns all open positions
func (pm *PositionManager) GetAllPositions() []*Position {
	positions := make([]*Position, 0, len(pm.positions))
	for _, pos := range pm.positions {
		positions = append(positions, pos)
	}
	return positions
}

// GetOpenPositionCount returns the number of open positions
func (pm *PositionManager) GetOpenPositionCount() int {
	return len(pm.positions)
}

// HasPosition checks if we have a position in a ticker
func (pm *PositionManager) HasPosition(ticker string) bool {
	_, exists := pm.positions[ticker]
	return exists
}

// ClosePartial closes a partial position (e.g., 50% at target 1)
func (pm *PositionManager) ClosePartial(ticker string, sharesToClose int) *Position {
	position, exists := pm.positions[ticker]
	if !exists {
		return nil
	}

	if sharesToClose >= position.RemainingShares {
		// Close entire position
		return pm.ClosePosition(ticker)
	}

	position.RemainingShares -= sharesToClose
	return position
}

// MarkTarget1Filled marks target 1 as filled
func (pm *PositionManager) MarkTarget1Filled(ticker string) {
	position, exists := pm.positions[ticker]
	if exists {
		position.FilledTarget1 = true
	}
}

// MarkTarget2Filled marks target 2 as filled
func (pm *PositionManager) MarkTarget2Filled(ticker string) {
	position, exists := pm.positions[ticker]
	if exists {
		position.FilledTarget2 = true
	}
}

// CloseAllPositions closes all open positions (EOD)
func (pm *PositionManager) CloseAllPositions() []*Position {
	positions := make([]*Position, 0, len(pm.positions))
	for _, pos := range pm.positions {
		positions = append(positions, pos)
	}
	pm.positions = make(map[string]*Position)
	return positions
}

// UpdatePositionIndicators updates indicators for a position (for trailing stops, etc.)
func (pm *PositionManager) UpdatePositionIndicators(ticker string, indicators *IndicatorState) {
	position, exists := pm.positions[ticker]
	if exists && position.StrategyState != nil {
		position.StrategyState.ATR = indicators.ATR
		position.StrategyState.RSI = indicators.RSI
		position.StrategyState.LastUpdate = indicators.LastUpdate
	}
}
