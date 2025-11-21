package feed

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PolygonFeed implements the Feed interface using Polygon.io
type PolygonFeed struct {
	apiKey   string
	baseURL  string
	client   *http.Client
}

// NewPolygonFeed creates a new Polygon.io feed
func NewPolygonFeed(apiKey string) *PolygonFeed {
	return &PolygonFeed{
		apiKey:  apiKey,
		baseURL: "https://api.polygon.io",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// GetHistoricalBars fetches historical minute bars from Polygon.io
func (pf *PolygonFeed) GetHistoricalBars(ticker string, startDate, endDate time.Time, timeframe string) ([]Bar, error) {
	// Polygon.io API endpoint for aggregates
	endpoint := fmt.Sprintf("%s/v2/aggs/ticker/%s/range/1/%s/%s/%s", 
		pf.baseURL,
		ticker,
		timeframe, // "minute" for minute bars
		formatDate(startDate),
		formatDate(endDate),
	)

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Add API key as query parameter
	q := req.URL.Query()
	q.Add("apiKey", pf.apiKey)
	q.Add("adjusted", "true") // Adjusted for splits
	q.Add("sort", "asc")      // Sort ascending
	req.URL.RawQuery = q.Encode()

	resp, err := pf.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Results []struct {
			T      int64   `json:"t"` // Timestamp (milliseconds)
			O      float64 `json:"o"` // Open
			H      float64 `json:"h"` // High
			L      float64 `json:"l"` // Low
			C      float64 `json:"c"` // Close
			V      float64 `json:"v"` // Volume (can be float64 from API)
		} `json:"results"`
		Status    string `json:"status"`
		RequestID string `json:"request_id"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	// Accept OK or DELAYED status for historical data
	if result.Status != "OK" && result.Status != "DELAYED" {
		return nil, fmt.Errorf("API returned non-OK status: %s", result.Status)
	}

	bars := make([]Bar, 0, len(result.Results))
	for _, r := range result.Results {
		bars = append(bars, Bar{
			Time:   time.Unix(0, r.T*int64(time.Millisecond)),
			Open:   r.O,
			High:   r.H,
			Low:    r.L,
			Close:  r.C,
			Volume: int64(r.V), // Convert float64 to int64
		})
	}

	return bars, nil
}

// formatDate formats a date for Polygon.io API (YYYY-MM-DD)
func formatDate(t time.Time) string {
	return t.Format("2006-01-02")
}

// Connect is a no-op for REST API (needed for interface compliance)
func (pf *PolygonFeed) Connect() error {
	return nil
}

// Disconnect is a no-op for REST API (needed for interface compliance)
func (pf *PolygonFeed) Disconnect() error {
	return nil
}

// Subscribe is a no-op for REST API (needed for interface compliance)
// WebSocket implementation would go here
func (pf *PolygonFeed) Subscribe(ticker string) error {
	return nil
}

// Unsubscribe is a no-op for REST API (needed for interface compliance)
func (pf *PolygonFeed) Unsubscribe(ticker string) error {
	return nil
}

// GetCurrentBar returns the current minute bar (not implemented for REST API)
// This would require WebSocket or polling
func (pf *PolygonFeed) GetCurrentBar(ticker string) (*Bar, error) {
	return nil, fmt.Errorf("not implemented - requires WebSocket or polling")
}

// GetDaysOfBars fetches multiple days of minute bars efficiently
func (pf *PolygonFeed) GetDaysOfBars(ticker string, days int) (map[time.Time][]Bar, error) {
	// Calculate date range
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -days)

	// Fetch all bars at once
	allBars, err := pf.GetHistoricalBars(ticker, startDate, endDate, "minute")
	if err != nil {
		return nil, err
	}

	// Group by date
	barsByDate := make(map[time.Time][]Bar)
	
	for _, bar := range allBars {
		// Extract date (normalize to midnight)
		date := time.Date(bar.Time.Year(), bar.Time.Month(), bar.Time.Day(), 0, 0, 0, 0, bar.Time.Location())
		barsByDate[date] = append(barsByDate[date], bar)
	}

	return barsByDate, nil
}
