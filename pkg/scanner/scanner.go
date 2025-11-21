package scanner

import (
	"sort"
	"strings"
	"time"

	"github.com/perfect-nt-bot/pkg/config"
	"github.com/perfect-nt-bot/pkg/strategy"
)

// Scanner scans for trading opportunities
type Scanner struct {
	tickers      []string
	blacklist    map[string]bool
	minPrice     float64
	maxPrice     float64
	minVolume    int64
}

// NewScanner creates a new scanner
func NewScanner(cfg *config.Config) *Scanner {
	// Build blacklist map for quick lookup
	blacklistMap := make(map[string]bool)
	for _, ticker := range cfg.Blacklist {
		blacklistMap[strings.ToUpper(ticker)] = true
	}

	// Default ticker list (can be overridden by config)
	tickers := cfg.BacktestTickers
	if len(tickers) == 0 {
		// Default watchlist - high volume, liquid stocks
		tickers = []string{
			"AAPL", "MSFT", "GOOGL", "AMZN", "NVDA",
			"TSLA", "META", "AMD", "INTC", "SPY",
			"QQQ", "IWM", "AMD", "NFLX", "DIS",
		}
	}

	return &Scanner{
		tickers:   tickers,
		blacklist: blacklistMap,
		minPrice:  5.0,   // Minimum $5 per share
		maxPrice:  500.0, // Maximum $500 per share
		minVolume: 100000, // Minimum daily volume (can be adjusted)
	}
}

// GetTickers returns the list of tickers to scan
func (s *Scanner) GetTickers() []string {
	return s.tickers
}

// IsBlacklisted checks if a ticker is blacklisted
func (s *Scanner) IsBlacklisted(ticker string) bool {
	return s.blacklist[strings.ToUpper(ticker)]
}

// FilterTicker checks if a ticker meets basic filter criteria
func (s *Scanner) FilterTicker(ticker string, price float64, volume int64) bool {
	// Check blacklist
	if s.IsBlacklisted(ticker) {
		return false
	}

	// Check price range
	if price < s.minPrice || price > s.maxPrice {
		return false
	}

	// Check volume (if provided)
	if volume > 0 && volume < s.minVolume {
		return false
	}

	return true
}

// SignalScore represents a scored entry signal
type SignalScore struct {
	Signal *strategy.EntrySignal
	Score  float64
}

// ScoreSignals scores and ranks entry signals
func (s *Scanner) ScoreSignals(signals []*strategy.EntrySignal) []*SignalScore {
	scores := make([]*SignalScore, 0, len(signals))

	for _, signal := range signals {
		score := s.calculateScore(signal)
		scores = append(scores, &SignalScore{
			Signal: signal,
			Score:  score,
		})
	}

	// Sort by score (highest first)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	return scores
}

// calculateScore calculates a score for an entry signal (0-100)
func (s *Scanner) calculateScore(signal *strategy.EntrySignal) float64 {
	score := 0.0

	// Pattern confidence (0-1, weighted 30%)
	score += signal.Confidence * 30.0

	// VWAP extension strength (0-50, weighted 25%)
	// Stronger extension = higher score (up to 3x ATR)
	extensionScore := signal.VWAPExtension
	if extensionScore > 3.0 {
		extensionScore = 3.0
	}
	score += (extensionScore / 3.0) * 25.0

	// RSI strength (0-50, weighted 20%)
	// Higher RSI = higher score (70+ is very overbought)
	rsiScore := (signal.RSI - 65.0) / 35.0 // Normalize from 65-100 to 0-1
	if rsiScore > 1.0 {
		rsiScore = 1.0
	}
	if rsiScore < 0 {
		rsiScore = 0
	}
	score += rsiScore * 20.0

	// Volume strength (0-50, weighted 15%)
	// Higher volume = higher score (we'll need volume MA for this)
	// For now, we'll use pattern confidence as proxy
	volumeScore := signal.Confidence * 15.0
	score += volumeScore

	// Pattern type bonus (0-10, weighted 10%)
	patternBonus := 0.0
	switch signal.Pattern {
	case strategy.BearishEngulfing:
		patternBonus = 10.0
	case strategy.RejectionAtExtension:
		patternBonus = 8.0
	case strategy.ShootingStar:
		patternBonus = 6.0
	}
	score += patternBonus

	// Cap at 100
	if score > 100.0 {
		score = 100.0
	}

	return score
}

// SelectBestSignals selects the best N signals from a list
func (s *Scanner) SelectBestSignals(signals []*strategy.EntrySignal, maxCount int) []*strategy.EntrySignal {
	if len(signals) == 0 {
		return nil
	}

	scored := s.ScoreSignals(signals)
	
	count := maxCount
	if len(scored) < count {
		count = len(scored)
	}

	result := make([]*strategy.EntrySignal, 0, count)
	for i := 0; i < count; i++ {
		result = append(result, scored[i].Signal)
	}

	return result
}

// IsMarketOpen checks if the market is currently open (9:30 AM - 4:00 PM ET)
func IsMarketOpen(currentTime time.Time, location *time.Location) bool {
	// Convert to ET
	etTime := currentTime.In(location)
	
	// Market hours: 9:30 AM - 4:00 PM ET
	marketOpen := time.Date(etTime.Year(), etTime.Month(), etTime.Day(), 9, 30, 0, 0, location)
	marketClose := time.Date(etTime.Year(), etTime.Month(), etTime.Day(), 16, 0, 0, 0, location)
	
	return etTime.After(marketOpen) && etTime.Before(marketClose)
}

// IsPreMarket checks if it's pre-market hours (4:00 AM - 9:30 AM ET)
func IsPreMarket(currentTime time.Time, location *time.Location) bool {
	etTime := currentTime.In(location)
	
	preMarketOpen := time.Date(etTime.Year(), etTime.Month(), etTime.Day(), 4, 0, 0, 0, location)
	marketOpen := time.Date(etTime.Year(), etTime.Month(), etTime.Day(), 9, 30, 0, 0, location)
	
	return etTime.After(preMarketOpen) && etTime.Before(marketOpen)
}

// IsAfterHours checks if it's after-hours (4:00 PM - 8:00 PM ET)
func IsAfterHours(currentTime time.Time, location *time.Location) bool {
	etTime := currentTime.In(location)
	
	marketClose := time.Date(etTime.Year(), etTime.Month(), etTime.Day(), 16, 0, 0, 0, location)
	afterHoursClose := time.Date(etTime.Year(), etTime.Month(), etTime.Day(), 20, 0, 0, 0, location)
	
	return etTime.After(marketClose) && etTime.Before(afterHoursClose)
}

// GetEODTime returns the EOD time (3:50 PM ET) for a given date
func GetEODTime(date time.Time, location *time.Location) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), 15, 50, 0, 0, location)
}
