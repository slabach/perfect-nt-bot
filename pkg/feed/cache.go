package feed

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CacheMetadata stores metadata about cached data
type CacheMetadata struct {
	Ticker     string    `json:"ticker"`
	PullDate   time.Time `json:"pull_date"`   // Date when data was pulled
	Days       int       `json:"days"`        // Number of days requested
	DateCount  int       `json:"date_count"` // Number of trading days in cache
}

// CacheManager handles caching of historical data
type CacheManager struct {
	cacheDir string
}

// NewCacheManager creates a new cache manager
func NewCacheManager(cacheDir string) *CacheManager {
	if cacheDir == "" {
		cacheDir = "data/cache"
	}
	return &CacheManager{
		cacheDir: cacheDir,
	}
}

// GetCachePath returns the cache file path for a ticker
func (cm *CacheManager) GetCachePath(ticker string) string {
	return filepath.Join(cm.cacheDir, fmt.Sprintf("%s.json", ticker))
}

// GetMetadataPath returns the metadata file path for a ticker
func (cm *CacheManager) GetMetadataPath(ticker string) string {
	return filepath.Join(cm.cacheDir, fmt.Sprintf("%s_metadata.json", ticker))
}

// LoadCachedData loads cached data for a ticker if it exists and is from today
func (cm *CacheManager) LoadCachedData(ticker string, days int) (map[time.Time][]Bar, *CacheMetadata, error) {
	// Ensure cache directory exists
	if err := os.MkdirAll(cm.cacheDir, 0755); err != nil {
		return nil, nil, fmt.Errorf("failed to create cache directory: %v", err)
	}

	metadataPath := cm.GetMetadataPath(ticker)
	dataPath := cm.GetCachePath(ticker)

	// Check if metadata exists
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, nil, nil // No cache exists, that's okay
	}

	var metadata CacheMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return nil, nil, nil // Invalid metadata, ignore cache
	}

	// Check if cache is from today
	today := time.Now().Truncate(24 * time.Hour)
	cacheDate := metadata.PullDate.Truncate(24 * time.Hour)
	if !cacheDate.Equal(today) {
		return nil, nil, nil // Cache is from a different day, ignore it
	}

	// Check if we have enough days (at least what was requested)
	if metadata.Days < days {
		return nil, nil, nil // Not enough days cached
	}

	// Load cached data
	dataBytes, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, nil, nil // Cache file doesn't exist
	}

	// Deserialize cached data
	// Format: map[string][]Bar where key is date string (YYYY-MM-DD)
	var cachedData map[string][]CachedBar
	if err := json.Unmarshal(dataBytes, &cachedData); err != nil {
		return nil, nil, nil // Invalid cache data
	}

	// Convert back to map[time.Time][]Bar
	barsByDate := make(map[time.Time][]Bar)
	location := time.UTC // Default location, will be adjusted based on bar times
	
	for dateStr, bars := range cachedData {
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue // Skip invalid dates
		}
		
		// Convert cached bars to Bar structs
		convertedBars := make([]Bar, len(bars))
		for i, cb := range bars {
			convertedBars[i] = Bar{
				Time:   cb.Time,
				Open:   cb.Open,
				High:   cb.High,
				Low:    cb.Low,
				Close:  cb.Close,
				Volume: cb.Volume,
			}
			// Use location from first bar
			if i == 0 && !cb.Time.IsZero() {
				location = cb.Time.Location()
			}
		}
		
		// Normalize date to midnight in the bar's timezone
		normalizedDate := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, location)
		barsByDate[normalizedDate] = convertedBars
	}

	return barsByDate, &metadata, nil
}

// CachedBar is a serializable version of Bar
type CachedBar struct {
	Time   time.Time `json:"time"`
	Open   float64   `json:"open"`
	High   float64   `json:"high"`
	Low    float64   `json:"low"`
	Close  float64   `json:"close"`
	Volume int64     `json:"volume"`
}

// SaveCachedData saves data to cache
func (cm *CacheManager) SaveCachedData(ticker string, days int, barsByDate map[time.Time][]Bar) error {
	// Ensure cache directory exists
	if err := os.MkdirAll(cm.cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %v", err)
	}

	// Convert to serializable format (map[string][]CachedBar)
	cachedData := make(map[string][]CachedBar)
	for date, bars := range barsByDate {
		dateStr := date.Format("2006-01-02")
		cachedBars := make([]CachedBar, len(bars))
		for i, bar := range bars {
			cachedBars[i] = CachedBar{
				Time:   bar.Time,
				Open:   bar.Open,
				High:   bar.High,
				Low:    bar.Low,
				Close:  bar.Close,
				Volume: bar.Volume,
			}
		}
		cachedData[dateStr] = cachedBars
	}

	// Save data
	dataPath := cm.GetCachePath(ticker)
	dataBytes, err := json.MarshalIndent(cachedData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache data: %v", err)
	}
	if err := os.WriteFile(dataPath, dataBytes, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %v", err)
	}

	// Save metadata
	metadata := CacheMetadata{
		Ticker:    ticker,
		PullDate:  time.Now(),
		Days:      days,
		DateCount: len(barsByDate),
	}
	metadataPath := cm.GetMetadataPath(ticker)
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %v", err)
	}
	if err := os.WriteFile(metadataPath, metadataBytes, 0644); err != nil {
		return fmt.Errorf("failed to write metadata file: %v", err)
	}

	return nil
}

