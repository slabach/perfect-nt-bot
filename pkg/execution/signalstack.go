package execution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// SignalStackClient handles order execution via SignalStack webhooks
type SignalStackClient struct {
	webhookURL string
	client     *http.Client
}

// NewSignalStackClient creates a new SignalStack client
func NewSignalStackClient(webhookURL string) *SignalStackClient {
	return &SignalStackClient{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// OrderType represents the type of order
type OrderType string

const (
	OrderTypeMarket OrderType = "MARKET"
	OrderTypeLimit  OrderType = "LIMIT"
)

// Side represents buy or sell
type Side string

const (
	SideBuy   Side = "BUY"
	SideSell  Side = "SELL"
	SideShort Side = "SHORT" // Short sell
	SideCover Side = "COVER" // Cover short
)

// Order represents a trading order
type Order struct {
	Ticker    string    `json:"ticker"`
	Side      Side      `json:"side"`
	Type      OrderType `json:"type"`
	Shares    int       `json:"shares"`
	Price     *float64  `json:"price,omitempty"` // Optional for limit orders
	OrderID   string    `json:"order_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// OrderResponse represents the response from SignalStack
type OrderResponse struct {
	Success bool   `json:"success"`
	OrderID string `json:"order_id,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// PlaceOrder places an order via SignalStack webhook
func (ss *SignalStackClient) PlaceOrder(order *Order) (*OrderResponse, error) {
	if order.Timestamp.IsZero() {
		order.Timestamp = time.Now()
	}

	// Generate order ID if not provided
	if order.OrderID == "" {
		order.OrderID = fmt.Sprintf("%s_%d_%d", order.Ticker, order.Shares, time.Now().UnixNano())
	}

	// Marshal order to JSON
	jsonData, err := json.Marshal(order)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal order: %v", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", ss.webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := ss.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send order: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}

	// Parse response
	var orderResp OrderResponse
	if err := json.Unmarshal(body, &orderResp); err != nil {
		// If response is not JSON, check HTTP status
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("order failed: status %d, body: %s", resp.StatusCode, string(body))
		}
		// Try to create a basic response
		orderResp.Success = resp.StatusCode == http.StatusOK
		orderResp.Message = string(body)
	}

	if !orderResp.Success {
		return &orderResp, fmt.Errorf("order failed: %s", orderResp.Error)
	}

	return &orderResp, nil
}

// PlaceMarketOrder places a market order
func (ss *SignalStackClient) PlaceMarketOrder(ticker string, side Side, shares int) (*OrderResponse, error) {
	order := &Order{
		Ticker:    ticker,
		Side:      side,
		Type:      OrderTypeMarket,
		Shares:    shares,
		Timestamp: time.Now(),
	}

	return ss.PlaceOrder(order)
}

// PlaceLimitOrder places a limit order
func (ss *SignalStackClient) PlaceLimitOrder(ticker string, side Side, shares int, price float64) (*OrderResponse, error) {
	order := &Order{
		Ticker:    ticker,
		Side:      side,
		Type:      OrderTypeLimit,
		Shares:    shares,
		Price:     &price,
		Timestamp: time.Now(),
	}

	return ss.PlaceOrder(order)
}

// PlaceShortOrder places a short sell order
func (ss *SignalStackClient) PlaceShortOrder(ticker string, shares int) (*OrderResponse, error) {
	return ss.PlaceMarketOrder(ticker, SideShort, shares)
}

// PlaceCoverOrder places a cover order (to close a short position)
func (ss *SignalStackClient) PlaceCoverOrder(ticker string, shares int) (*OrderResponse, error) {
	return ss.PlaceMarketOrder(ticker, SideCover, shares)
}
