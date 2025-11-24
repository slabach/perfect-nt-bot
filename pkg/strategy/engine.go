package strategy

import (
	"fmt"
	"time"
)

// StrategyEngine manages the complete strategy logic
type StrategyEngine struct {
	// Per-ticker calculators (will need separate instances per ticker)
	entryChecker  *EntryChecker
	exitChecker   *ExitChecker
	positionMgr   *PositionManager
	
	// Per-ticker state
	tickerStates  map[string]*IndicatorState
	tickerBars    map[string][]Bar // History for pattern detection
	tickerVWAPs   map[string]*VWAPCalculator
	tickerATRs    map[string]*ATRCalculator
	tickerRSIs    map[string]*RSICalculator
	
	// Market hours
	location      *time.Location
	eodTime       time.Time
	
	// Configuration
	marketOpen    time.Time
}

// NewStrategyEngine creates a new strategy engine
func NewStrategyEngine(location *time.Location, marketOpen time.Time) *StrategyEngine {
	return &StrategyEngine{
		entryChecker: NewEntryChecker(),
		exitChecker:  NewExitChecker(),
		positionMgr:  NewPositionManager(),
		tickerStates: make(map[string]*IndicatorState),
		tickerBars:   make(map[string][]Bar),
		tickerVWAPs:  make(map[string]*VWAPCalculator),
		tickerATRs:   make(map[string]*ATRCalculator),
		tickerRSIs:   make(map[string]*RSICalculator),
		location:     location,
		marketOpen:   marketOpen,
	}
}

// ResetDailyState resets daily state (call at market open)
func (se *StrategyEngine) ResetDailyState(marketOpen time.Time) {
	se.marketOpen = marketOpen
	
	// Reset performance tracker for adaptive thresholds
	if se.entryChecker != nil {
		se.entryChecker.ResetDaily()
	}
	
	// Reset per-ticker indicators
	for ticker := range se.tickerStates {
		se.tickerStates[ticker] = &IndicatorState{}
		
		// Reset calculators for this ticker
		if vwap, exists := se.tickerVWAPs[ticker]; exists {
			vwap.Reset(marketOpen)
		}
		if atr, exists := se.tickerATRs[ticker]; exists {
			atr.Reset()
		}
		if rsi, exists := se.tickerRSIs[ticker]; exists {
			rsi.Reset()
		}
	}
}

// RecordTrade records a completed trade for performance tracking
func (se *StrategyEngine) RecordTrade(ticker string, entryTime time.Time, netPnL float64) {
	if se.entryChecker != nil {
		se.entryChecker.RecordTrade(ticker, entryTime, netPnL)
	}
}

// SetAdaptiveThresholdsEnabled enables or disables adaptive thresholds
func (se *StrategyEngine) SetAdaptiveThresholdsEnabled(enabled bool) {
	if se.entryChecker != nil {
		se.entryChecker.SetAdaptiveEnabled(enabled)
	}
}

// UpdateTicker updates indicators for a ticker with a new bar
func (se *StrategyEngine) UpdateTicker(ticker string, bar Bar) {
	// Skip bars before market open
	if bar.Time.Before(se.marketOpen) {
		return
	}

	// Initialize calculators if needed
	if _, exists := se.tickerVWAPs[ticker]; !exists {
		se.tickerVWAPs[ticker] = NewVWAPCalculator()
		se.tickerATRs[ticker] = NewATRCalculator(14)
		se.tickerRSIs[ticker] = NewRSICalculator(14)
		se.tickerStates[ticker] = &IndicatorState{}
		se.tickerBars[ticker] = make([]Bar, 0)
	}

	vwap := se.tickerVWAPs[ticker]
	atr := se.tickerATRs[ticker]
	rsi := se.tickerRSIs[ticker]

	// Update VWAP
	vwap.Update(bar, se.marketOpen)

	// Update ATR
	atr.Update(bar)

	// Update RSI
	rsi.Update(bar)

	// Calculate volume MA (20-period)
	se.tickerBars[ticker] = append(se.tickerBars[ticker], bar)
	if len(se.tickerBars[ticker]) > 20 {
		se.tickerBars[ticker] = se.tickerBars[ticker][len(se.tickerBars[ticker])-20:]
	}

	// Calculate volume MA
	var volumeSum int64
	for _, b := range se.tickerBars[ticker] {
		volumeSum += b.Volume
	}
	volumeMA := float64(volumeSum) / float64(len(se.tickerBars[ticker]))

	// Update ticker state
	se.tickerStates[ticker] = &IndicatorState{
		VWAP:      vwap.GetVWAP(),
		ATR:       atr.GetATR(),
		RSI:       rsi.GetRSI(),
		VolumeMA:  volumeMA,
		LastUpdate: bar.Time,
	}
}

// GetTickerState returns the indicator state for a ticker
func (se *StrategyEngine) GetTickerState(ticker string) (*IndicatorState, bool) {
	state, exists := se.tickerStates[ticker]
	return state, exists
}

// GetRecentBars returns recent bars for a ticker (for ML feature extraction)
func (se *StrategyEngine) GetRecentBars(ticker string, count int) []Bar {
	if bars, exists := se.tickerBars[ticker]; exists {
		if len(bars) > count {
			return bars[len(bars)-count:]
		}
		return bars
	}
	return make([]Bar, 0)
}

// CheckEntry checks if entry conditions are met for a ticker (checks both short and long)
func (se *StrategyEngine) CheckEntry(ticker string, bar Bar, eodTime time.Time, openPositions int) (*EntrySignal, error) {
	signals := se.CheckBothDirections(ticker, bar, eodTime, openPositions)
	
	// Return the first signal found (short has priority, but scoring will handle ranking)
	// If both exist, scoring will rank them properly in the backtest
	if len(signals) > 0 {
		return signals[0], nil
	}
	
	return nil, fmt.Errorf("no entry signals found")
}

// CheckBothDirections checks both short and long entry opportunities for a ticker
func (se *StrategyEngine) CheckBothDirections(ticker string, bar Bar, eodTime time.Time, openPositions int) []*EntrySignal {
	signals := make([]*EntrySignal, 0)
	
	// Get ticker state
	state, exists := se.tickerStates[ticker]
	if !exists {
		return signals
	}

	// Get previous bar for pattern detection
	var previousBar Bar
	if bars, exists := se.tickerBars[ticker]; exists && len(bars) > 0 {
		previousBar = bars[len(bars)-1]
	}

	// Check short entry conditions
	var shortSignal *EntrySignal
	var err error
	
	if !previousBar.Time.IsZero() {
		// Use previous bar for better pattern detection
		shortSignal, err = se.entryChecker.CheckEntryConditionsWithPrevious(
			ticker,
			bar,
			previousBar,
			state,
			openPositions,
			eodTime,
		)
	} else {
		// No previous bar, use basic check
		currentPrice := bar.Close
		shortSignal, err = se.entryChecker.CheckEntryConditions(
			ticker,
			bar,
			state,
			currentPrice,
			openPositions,
			eodTime,
		)
	}
	
	if err == nil && shortSignal != nil {
		signals = append(signals, shortSignal)
	}

	// Check long entry conditions
	var longSignal *EntrySignal
	
	if !previousBar.Time.IsZero() {
		// Use previous bar for better pattern detection
		longSignal, err = se.entryChecker.CheckLongEntryConditionsWithPrevious(
			ticker,
			bar,
			previousBar,
			state,
			openPositions,
			eodTime,
		)
	} else {
		// No previous bar, use basic check
		currentPrice := bar.Close
		longSignal, err = se.entryChecker.CheckLongEntryConditions(
			ticker,
			bar,
			state,
			currentPrice,
			openPositions,
			eodTime,
		)
	}
	
	if err == nil && longSignal != nil {
		signals = append(signals, longSignal)
	}

	return signals
}

// CheckExits checks all open positions for exit conditions
func (se *StrategyEngine) CheckExits(bar Bar, eodTime time.Time) []ExitSignal {
	exits := make([]ExitSignal, 0)
	positions := se.positionMgr.GetAllPositions()

	for _, position := range positions {
		shouldExit, reason, exitPrice := se.exitChecker.CheckExitConditions(position, bar, eodTime)
		
		if shouldExit {
			exits = append(exits, ExitSignal{
				Ticker:    position.Ticker,
				Position:  position,
				Reason:    reason,
				ExitPrice: exitPrice,
			})
		}
	}

	return exits
}

// ExitSignal represents an exit signal for a position
type ExitSignal struct {
	Ticker    string
	Position  *Position
	Reason    ExitReason
	ExitPrice float64
}

// OpenPosition opens a position from an entry signal
func (se *StrategyEngine) OpenPosition(signal *EntrySignal, shares int) *Position {
	return se.positionMgr.OpenPosition(signal, shares)
}

// ClosePosition closes a position
func (se *StrategyEngine) ClosePosition(ticker string) *Position {
	return se.positionMgr.ClosePosition(ticker)
}

// GetPositions returns all open positions
func (se *StrategyEngine) GetPositions() []*Position {
	return se.positionMgr.GetAllPositions()
}

// GetPositionCount returns the number of open positions
func (se *StrategyEngine) GetPositionCount() int {
	return se.positionMgr.GetOpenPositionCount()
}

// HasPosition checks if we have a position in a ticker
func (se *StrategyEngine) HasPosition(ticker string) bool {
	return se.positionMgr.HasPosition(ticker)
}

// ClosePartial closes a partial position
func (se *StrategyEngine) ClosePartial(ticker string, shares int) *Position {
	return se.positionMgr.ClosePartial(ticker, shares)
}

// MarkTarget1Filled marks target 1 as filled
func (se *StrategyEngine) MarkTarget1Filled(ticker string) {
	se.positionMgr.MarkTarget1Filled(ticker)
}

// MarkTarget2Filled marks target 2 as filled
func (se *StrategyEngine) MarkTarget2Filled(ticker string) {
	se.positionMgr.MarkTarget2Filled(ticker)
}

// CloseAllPositions closes all positions (EOD)
func (se *StrategyEngine) CloseAllPositions() []*Position {
	return se.positionMgr.CloseAllPositions()
}

// GetMaxConcurrentPositions returns the maximum concurrent positions allowed
func (se *StrategyEngine) GetMaxConcurrentPositions() int {
	return se.entryChecker.GetMaxConcurrentPositions()
}
