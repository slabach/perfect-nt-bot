package scanner

import (
	"sort"
	"strings"
	"time"

	"github.com/perfect-nt-bot/pkg/config"
	"github.com/perfect-nt-bot/pkg/strategy"
)

// MLScorer interface for ML-based signal scoring
type MLScorer interface {
	ScoreSignal(signal *strategy.EntrySignal, indicators *strategy.IndicatorState, recentBars []strategy.Bar) float64
	IsEnabled() bool
}

// Scanner scans for trading opportunities
type Scanner struct {
	tickers   []string
	blacklist map[string]bool
	minPrice  float64
	maxPrice  float64
	minVolume int64
	mlScorer  MLScorer // ML scorer interface (optional)
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
		minPrice:  5.0,    // Minimum $5 per share
		maxPrice:  500.0,  // Maximum $500 per share
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

// SetMLScorer sets the ML scorer for the scanner
func (s *Scanner) SetMLScorer(scorer MLScorer) {
	s.mlScorer = scorer
}

// calculateScore calculates a score for an entry signal (0-100)
func (s *Scanner) calculateScore(signal *strategy.EntrySignal) float64 {
	score := 0.0

	// ML score (0-1, weighted 10% if enabled) - reduced weight since ML model is not reliable
	// ML model showed 0% win rate in backtest, so we reduce its influence significantly
	if s.mlScorer != nil && s.mlScorer.IsEnabled() {
		// ML score is already in signal.MLScore if set
		mlScore := signal.MLScore
		if mlScore == 0 {
			// Fallback: use default if not set
			mlScore = 0.5
		}
		// Reduced from 40% to 10% - ML model is not predictive enough
		score += mlScore * 10.0
	}

	// Pattern confidence (0-1, weighted 25%) - increased from 20% to compensate for reduced ML weight
	score += signal.Confidence * 25.0

	// VWAP extension strength (0-50, weighted 30%) - increased from 20% to compensate for reduced ML weight
	// Use absolute value so both directions score equally
	// Stronger extension = higher score (up to 3x ATR)
	absExtension := signal.VWAPExtension
	if absExtension < 0 {
		absExtension = -absExtension
	}
	if absExtension > 3.0 {
		absExtension = 3.0
	}
	score += (absExtension / 3.0) * 30.0

	// RSI strength (0-50, weighted 20%) - increased from 15% to compensate for reduced ML weight
	var rsiScore float64
	if signal.Direction == "SHORT" {
		// For shorts: Higher RSI = higher score (70+ is very overbought)
		// Normalize from 65-100 to 0-1
		rsiScore = (signal.RSI - 65.0) / 35.0
		if rsiScore > 1.0 {
			rsiScore = 1.0
		}
		if rsiScore < 0 {
			rsiScore = 0
		}
	} else {
		// For longs: Lower RSI = higher score (30- is very oversold)
		// Normalize from 65-30 to 0-1 (inverse)
		// RSI 30 = score 1.0, RSI 65 = score 0.0
		rsiScore = (35.0 - signal.RSI) / 35.0
		if rsiScore > 1.0 {
			rsiScore = 1.0
		}
		if rsiScore < 0 {
			rsiScore = 0
		}
	}
	score += rsiScore * 20.0 // Increased from 15% to 20% to compensate for reduced ML weight

	// Volume strength (0-50, weighted 10%)
	// Use actual volume ratio if available, otherwise use pattern confidence as proxy
	// Note: Volume ratio calculation would need VolumeMA from indicators
	// For now, use a simple heuristic based on volume
	volumeScore := signal.Confidence * 10.0
	// TODO: Improve volume scoring when VolumeMA is available in signal
	score += volumeScore

	// Pattern type bonus (0-10, weighted 5%)
	patternBonus := 0.0
	switch signal.Pattern {
	case strategy.BearishEngulfing:
		patternBonus = 10.0
	case strategy.RejectionAtExtension:
		patternBonus = 8.0
	case strategy.ShootingStar:
		patternBonus = 6.0
	case strategy.BullishEngulfing:
		patternBonus = 10.0
	case strategy.RejectionAtBottom:
		patternBonus = 8.0
	case strategy.Hammer:
		patternBonus = 6.0
	}
	score += patternBonus * 0.5 // 5% weight

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
