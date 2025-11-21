package strategy

import (
	"time"
)

// Bar represents a single bar/candlestick
type Bar struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume int64
}

// IndicatorState holds calculated indicators for a ticker
type IndicatorState struct {
	VWAP       float64
	ATR        float64
	RSI        float64
	VolumeMA   float64 // 20-period volume moving average
	LastUpdate time.Time
}

// DeathCandlePattern represents detected pattern types
type DeathCandlePattern int

const (
	NoPattern DeathCandlePattern = iota
	BearishEngulfing
	RejectionAtExtension
	ShootingStar
)

// EntrySignal represents a trading opportunity
type EntrySignal struct {
	Ticker              string
	EntryPrice          float64
	Direction           string // "SHORT" or "LONG"
	StopLoss            float64
	Target1             float64 // First profit target
	Target2             float64 // Second profit target
	Confidence          float64 // 0-1 score
	VWAPExtension       float64 // How far above/below VWAP (in ATR multiples)
	Pattern             DeathCandlePattern
	RSI                 float64
	Volume              int64
	Timestamp           time.Time
	Reason              string // Human-readable reason for entry
}

// Position represents an open trading position
type Position struct {
	Ticker           string
	EntryPrice       float64
	Shares           int
	Direction        string // "SHORT" or "LONG"
	EntryTime        time.Time
	StopLoss         float64
	Target1          float64
	Target2          float64
	RemainingShares  int    // After partial fills
	FilledTarget1    bool
	FilledTarget2    bool
	TrailingStop     *float64 // Active trailing stop price
	StrategyState    *IndicatorState
	Pattern          DeathCandlePattern
}

// ExitReason represents why a position was closed
type ExitReason string

const (
	ExitReasonStopLoss     ExitReason = "Stop Loss"
	ExitReasonTarget1      ExitReason = "Target 1"
	ExitReasonTarget2      ExitReason = "Target 2"
	ExitReasonTrailingStop ExitReason = "Trailing Stop"
	ExitReasonTimeDecay    ExitReason = "Time Decay"
	ExitReasonEOD          ExitReason = "End of Day"
	ExitReasonManual       ExitReason = "Manual"
)

// TradeResult represents a completed trade
type TradeResult struct {
	Ticker      string
	EntryTime   time.Time
	ExitTime    time.Time
	EntryPrice  float64
	ExitPrice   float64
	Shares      int
	Direction   string
	Reason      ExitReason
	PnL         float64
	Commission  float64
	NetPnL      float64
}
