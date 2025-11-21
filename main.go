package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/perfect-nt-bot/pkg/config"
	"github.com/perfect-nt-bot/pkg/execution"
	"github.com/perfect-nt-bot/pkg/feed"
	"github.com/perfect-nt-bot/pkg/risk"
	"github.com/perfect-nt-bot/pkg/scanner"
	"github.com/perfect-nt-bot/pkg/strategy"
)

func main() {
	fmt.Println("Perfect Trading Bot - Starting...")

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Validate config for live trading
	if err := cfg.Validate(true); err != nil {
		log.Fatalf("Config validation failed: %v", err)
	}

	fmt.Printf("Account Size: $%.2f\n", cfg.AccountSize)
	fmt.Printf("Profit Target: $%.2f\n", cfg.ProfitTarget)
	fmt.Printf("Account Close Limit: $%.2f\n", cfg.AccountCloseLimit)
	fmt.Printf("Max Daily Loss: $%.2f\n", cfg.MaxDailyLossLimit)
	fmt.Printf("Hard Stop Loss: $%.2f\n", cfg.HardStopLossLimit)
	fmt.Println()

	// Get ET timezone
	location, err := config.GetLocation()
	if err != nil {
		log.Fatalf("Failed to load timezone: %v", err)
	}

	// Create components
	polygonFeed := feed.NewPolygonFeed(cfg.PolygonAPIKey)
	signalStack := execution.NewSignalStackClient(cfg.SignalStackWebhookURL)
	scanner := scanner.NewScanner(cfg)

	// Create risk managers
	buyingPower := risk.NewBuyingPowerManager(cfg.AccountSize, true)
	riskLimits := risk.NewRiskLimitsManager(
		cfg.AccountSize,
		cfg.MaxDailyLossLimit,
		cfg.HardStopLossLimit,
		cfg.ProfitTarget,
		cfg.AccountCloseLimit,
	)

	// Create strategy engine
	now := time.Now().In(location)
	marketOpen := time.Date(now.Year(), now.Month(), now.Day(), 9, 30, 0, 0, location)
	strategyEngine := strategy.NewStrategyEngine(location, marketOpen)

	// Create bot
	bot := NewTradingBot(
		cfg,
		polygonFeed,
		signalStack,
		scanner,
		buyingPower,
		riskLimits,
		strategyEngine,
		location,
	)

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start bot in goroutine
	done := make(chan error, 1)
	go func() {
		done <- bot.Run()
	}()

	// Wait for shutdown signal or error
	select {
	case err := <-done:
		if err != nil {
			log.Fatalf("Bot error: %v", err)
		}
	case sig := <-sigChan:
		fmt.Printf("\nReceived signal: %v. Shutting down...\n", sig)
		if err := bot.Shutdown(); err != nil {
			log.Printf("Error during shutdown: %v", err)
		}
	}

	fmt.Println("Bot stopped.")
}

// TradingBot manages the live trading bot
type TradingBot struct {
	cfg            *config.Config
	polygonFeed    *feed.PolygonFeed
	signalStack    *execution.SignalStackClient
	scanner        *scanner.Scanner
	buyingPower    *risk.BuyingPowerManager
	riskLimits     *risk.RiskLimitsManager
	strategyEngine *strategy.StrategyEngine
	location       *time.Location
	shutdown       chan struct{}
}

// NewTradingBot creates a new trading bot
func NewTradingBot(
	cfg *config.Config,
	polygonFeed *feed.PolygonFeed,
	signalStack *execution.SignalStackClient,
	scanner *scanner.Scanner,
	buyingPower *risk.BuyingPowerManager,
	riskLimits *risk.RiskLimitsManager,
	strategyEngine *strategy.StrategyEngine,
	location *time.Location,
) *TradingBot {
	return &TradingBot{
		cfg:            cfg,
		polygonFeed:    polygonFeed,
		signalStack:    signalStack,
		scanner:        scanner,
		buyingPower:    buyingPower,
		riskLimits:     riskLimits,
		strategyEngine: strategyEngine,
		location:       location,
		shutdown:       make(chan struct{}),
	}
}

// Run runs the main bot loop
func (tb *TradingBot) Run() error {
	fmt.Println("Connecting to Polygon.io...")
	// TODO: Connect to Polygon WebSocket feed
	// For now, this is a placeholder

	fmt.Println("Bot running... (Press Ctrl+C to stop)")

	// Main event loop
	// TODO: Implement WebSocket message handling
	// TODO: Process real-time bars
	// TODO: Check entry/exit conditions
	// TODO: Execute trades via SignalStack
	// TODO: Handle 3:50 PM EOD close

	// For now, just wait for shutdown
	<-tb.shutdown
	return nil
}

// Shutdown gracefully shuts down the bot
func (tb *TradingBot) Shutdown() error {
	fmt.Println("Closing all positions...")

	// Close all positions at EOD
	positions := tb.strategyEngine.CloseAllPositions()

	for _, position := range positions {
		fmt.Printf("Closing position: %s\n", position.Ticker)
		// TODO: Execute cover order via SignalStack
		_ = position
	}

	// Close connections
	// TODO: Disconnect from Polygon WebSocket

	close(tb.shutdown)
	return nil
}
