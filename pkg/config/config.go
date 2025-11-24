package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all configuration values
type Config struct {
	// API Keys
	PolygonAPIKey         string
	SignalStackWebhookURL string

	// Account Configuration
	AccountSize     float64
	MaxDailyLoss    float64
	MaxDailyLossPct float64
	HardStopLoss    float64
	HardStopLossPct float64

	// Trading Configuration
	BacktestTickers []string
	Blacklist       []string

	// ML and Filtering Configuration
	MLModelPath              string  // Path to ML model file
	MinConfidenceThreshold   float64 // Minimum confidence score (0-100, default: 60.0)
	EnableCorrelationFilter  bool    // Enable correlation filtering
	EnableAdaptiveThresholds bool    // Enable adaptive threshold adjustment

	// Risk Limits (calculated)
	MaxDailyLossLimit float64 // Capped at 1% of account
	HardStopLossLimit float64 // Capped at 0.5% of account
	ProfitTarget      float64 // AccountSize + (AccountSize * 0.06)
	MaxProfitPerTrade float64 // 30% of profit target (eval requirement)
	AccountCloseLimit float64 // AccountSize - 3*(AccountSize*0.01)
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	// Try to load .env file (ignore error if it doesn't exist)
	_ = godotenv.Load()

	cfg := &Config{}

	// Load API keys
	cfg.PolygonAPIKey = getEnv("POLYGON_API_KEY", "")
	cfg.SignalStackWebhookURL = getEnv("SIGNALSTACK_WEBHOOK_URL", "")

	// Load account size (required)
	accountSizeStr := getEnv("ACCOUNT_SIZE", "25000")
	accountSize, err := strconv.ParseFloat(accountSizeStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid ACCOUNT_SIZE: %v", err)
	}
	if accountSize <= 0 {
		return nil, fmt.Errorf("ACCOUNT_SIZE must be > 0")
	}
	cfg.AccountSize = accountSize

	// Load max daily loss (optional, defaults to 1% of account)
	maxDailyLossPctStr := getEnv("MAX_DAILY_LOSS_PCT", "")
	if maxDailyLossPctStr != "" {
		pct, err := strconv.ParseFloat(maxDailyLossPctStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_DAILY_LOSS_PCT: %v", err)
		}
		cfg.MaxDailyLossPct = pct
	} else {
		maxDailyLossStr := getEnv("MAX_DAILY_LOSS", "")
		if maxDailyLossStr != "" {
			loss, err := strconv.ParseFloat(maxDailyLossStr, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid MAX_DAILY_LOSS: %v", err)
			}
			cfg.MaxDailyLoss = loss
			cfg.MaxDailyLossPct = loss / accountSize
		} else {
			cfg.MaxDailyLossPct = 0.01 // Default 1%
		}
	}

	// Cap max daily loss at 1% of account
	if cfg.MaxDailyLossPct > 0.01 {
		cfg.MaxDailyLossPct = 0.01
	}
	cfg.MaxDailyLossLimit = accountSize * cfg.MaxDailyLossPct

	// Load hard stop loss (optional, defaults to 0.5% of account)
	hardStopLossPctStr := getEnv("HARD_STOP_LOSS_PCT", "")
	if hardStopLossPctStr != "" {
		pct, err := strconv.ParseFloat(hardStopLossPctStr, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid HARD_STOP_LOSS_PCT: %v", err)
		}
		cfg.HardStopLossPct = pct
	} else {
		hardStopLossStr := getEnv("HARD_STOP_LOSS", "")
		if hardStopLossStr != "" {
			loss, err := strconv.ParseFloat(hardStopLossStr, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid HARD_STOP_LOSS: %v", err)
			}
			cfg.HardStopLoss = loss
			cfg.HardStopLossPct = loss / accountSize
		} else {
			cfg.HardStopLossPct = 0.005 // Default 0.5%
		}
	}

	// Cap hard stop loss at 0.5% of account
	if cfg.HardStopLossPct > 0.005 {
		cfg.HardStopLossPct = 0.005
	}
	cfg.HardStopLossLimit = accountSize * cfg.HardStopLossPct

	// Calculate profit target: AccountSize + (AccountSize * 0.06)
	cfg.ProfitTarget = accountSize + (accountSize * 0.06)

	// Calculate max profit per trade for eval (30% of profit target)
	// Example: $25k account needs $1,500 profit, so max per trade = $450
	profitNeeded := accountSize * 0.06
	cfg.MaxProfitPerTrade = profitNeeded * 0.30

	// Calculate account close limit: AccountSize - 3*(AccountSize*0.01)
	cfg.AccountCloseLimit = accountSize - (3 * (accountSize * 0.01))

	// Load ticker lists
	backtestTickersStr := getEnv("BACKTEST_TICKERS", "")
	if backtestTickersStr != "" {
		cfg.BacktestTickers = parseCommaList(backtestTickersStr)
	}

	blacklistStr := getEnv("BLACKLIST", "")
	if blacklistStr != "" {
		cfg.Blacklist = parseCommaList(blacklistStr)
	}

	// Load ML and filtering configuration
	// Default to empty (disabled) - ML model is not reliable enough yet
	// Set ML_MODEL_PATH environment variable to enable ML
	cfg.MLModelPath = getEnv("ML_MODEL_PATH", "")

	minConfidenceStr := getEnv("MIN_CONFIDENCE_THRESHOLD", "60.0")
	minConfidence, err := strconv.ParseFloat(minConfidenceStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid MIN_CONFIDENCE_THRESHOLD: %v", err)
	}
	cfg.MinConfidenceThreshold = minConfidence

	correlationFilterStr := getEnv("ENABLE_CORRELATION_FILTER", "true")
	cfg.EnableCorrelationFilter = correlationFilterStr == "true" || correlationFilterStr == "1"

	adaptiveThresholdsStr := getEnv("ENABLE_ADAPTIVE_THRESHOLDS", "true")
	cfg.EnableAdaptiveThresholds = adaptiveThresholdsStr == "true" || adaptiveThresholdsStr == "1"

	return cfg, nil
}

// Validate checks that required configuration is present
func (c *Config) Validate(liveTrading bool) error {
	if c.PolygonAPIKey == "" {
		return fmt.Errorf("POLYGON_API_KEY is required")
	}

	if liveTrading && c.SignalStackWebhookURL == "" {
		return fmt.Errorf("SIGNALSTACK_WEBHOOK_URL is required for live trading")
	}

	if c.AccountSize <= 0 {
		return fmt.Errorf("ACCOUNT_SIZE must be > 0")
	}

	return nil
}

// getEnv gets an environment variable with a default value
func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

// parseCommaList parses a comma-separated list and trims whitespace
func parseCommaList(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// IsInBlacklist checks if a ticker is in the blacklist
func (c *Config) IsInBlacklist(ticker string) bool {
	for _, blacklisted := range c.Blacklist {
		if strings.EqualFold(blacklisted, ticker) {
			return true
		}
	}
	return false
}

// GetLocation returns the ET timezone location for market hours
func GetLocation() (*time.Location, error) {
	return time.LoadLocation("America/New_York")
}
