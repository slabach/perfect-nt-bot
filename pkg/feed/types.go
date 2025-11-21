package feed

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

// Quote represents a bid/ask quote
type Quote struct {
	Bid     float64
	Ask     float64
	BidSize int64
	AskSize int64
	Time    time.Time
}

// Trade represents a single trade execution
type Trade struct {
	Price     float64
	Size      int64
	Timestamp time.Time
}

// MarketData represents real-time market data for a ticker
type MarketData struct {
	Ticker string
	Bars   []Bar
	Quotes []Quote
	Trades []Trade
	LastUpdate time.Time
}

// Feed represents a data feed interface
type Feed interface {
	// Connect connects to the feed
	Connect() error
	
	// Disconnect disconnects from the feed
	Disconnect() error
	
	// Subscribe subscribes to a ticker for real-time data
	Subscribe(ticker string) error
	
	// Unsubscribe unsubscribes from a ticker
	Unsubscribe(ticker string) error
	
	// GetHistoricalBars fetches historical bars for backtesting
	GetHistoricalBars(ticker string, startDate, endDate time.Time, timeframe string) ([]Bar, error)
	
	// GetCurrentBar returns the current minute bar for a ticker
	GetCurrentBar(ticker string) (*Bar, error)
}
