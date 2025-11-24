package scanner

import (
	"strings"

	"github.com/perfect-nt-bot/pkg/strategy"
)

// SectorMap maps tickers to their sectors (basic implementation)
var SectorMap = map[string]string{
	// Technology
	"AAPL": "Technology", "MSFT": "Technology", "GOOGL": "Technology", "GOOG": "Technology",
	"AMZN": "Technology", "NVDA": "Technology", "META": "Technology", "AMD": "Technology",
	"INTC": "Technology", "NFLX": "Technology", "TSLA": "Technology",

	// Finance
	"JPM": "Finance", "BAC": "Finance", "WFC": "Finance", "GS": "Finance", "MS": "Finance",

	// Healthcare
	"JNJ": "Healthcare", "PFE": "Healthcare", "UNH": "Healthcare", "ABBV": "Healthcare",

	// Consumer
	"WMT": "Consumer", "HD": "Consumer", "MCD": "Consumer", "NKE": "Consumer", "DIS": "Consumer",

	// Energy
	"XOM": "Energy", "CVX": "Energy", "COP": "Energy",

	// ETFs
	"SPY": "ETF", "QQQ": "ETF", "IWM": "ETF", "DIA": "ETF",

	// Default sector for unknown tickers
}

// GetSector returns the sector for a ticker
func GetSector(ticker string) string {
	tickerUpper := strings.ToUpper(ticker)
	if sector, exists := SectorMap[tickerUpper]; exists {
		return sector
	}
	return "Other" // Default sector
}

// CheckCorrelation checks if a new position would violate correlation rules
// Returns true if the position should be allowed, false if it should be rejected
func (s *Scanner) CheckCorrelation(newTicker string, openPositions []*strategy.Position) bool {
	if len(openPositions) == 0 {
		return true // No existing positions, always allow
	}

	newSector := GetSector(newTicker)

	// Count positions in same sector
	sectorCount := 0
	for _, pos := range openPositions {
		if GetSector(pos.Ticker) == newSector {
			sectorCount++
		}
	}

	// Don't allow more than 2 positions in same sector
	if sectorCount >= 2 {
		return false
	}

	// Check for direct ticker match (don't allow duplicate positions)
	for _, pos := range openPositions {
		if strings.EqualFold(pos.Ticker, newTicker) {
			return false
		}
	}

	return true
}
