package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/perfect-nt-bot/pkg/config"
	"github.com/perfect-nt-bot/pkg/feed"
	"github.com/perfect-nt-bot/pkg/ml"
	"github.com/perfect-nt-bot/pkg/strategy"
)

func main() {
	// Parse command-line flags
	csvDirFlag := flag.String("csv-dir", "cmd/backtest/results", "Directory containing CSV backtest results")
	modelPathFlag := flag.String("model", "models/trading_model.json", "Path to save trained model")
	epochsFlag := flag.Int("epochs", 1000, "Number of training epochs")
	learningRateFlag := flag.Float64("lr", 0.01, "Learning rate")
	flag.Parse()

	// Load configuration for API key (if needed for feature extraction)
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	fmt.Println("Training ML model from backtest results...")
	fmt.Printf("CSV Directory: %s\n", *csvDirFlag)
	fmt.Printf("Model Output: %s\n", *modelPathFlag)
	fmt.Printf("Epochs: %d, Learning Rate: %.4f\n", *epochsFlag, *learningRateFlag)

	// Load training data from CSV files
	X, y, err := loadTrainingData(*csvDirFlag, cfg)
	if err != nil {
		log.Fatalf("Failed to load training data: %v", err)
	}

	if len(X) == 0 {
		log.Fatal("No training data found")
	}

	fmt.Printf("Loaded %d training samples\n", len(X))

	// Create model (14 features based on features.go)
	model := ml.NewModel(14)

	// Train model
	fmt.Println("Training model...")
	if err := model.Train(X, y, *learningRateFlag, *epochsFlag); err != nil {
		log.Fatalf("Training failed: %v", err)
	}

	// Create model directory if it doesn't exist
	modelDir := filepath.Dir(*modelPathFlag)
	if err := os.MkdirAll(modelDir, 0755); err != nil {
		log.Fatalf("Failed to create model directory: %v", err)
	}

	// Save model
	if err := model.Save(*modelPathFlag); err != nil {
		log.Fatalf("Failed to save model: %v", err)
	}

	fmt.Printf("Model trained and saved to: %s\n", *modelPathFlag)
}

// loadTrainingData loads training data from CSV files
// Returns features (X) and labels (y) where y=1 if trade hit Target 1 before Stop Loss, else 0
func loadTrainingData(csvDir string, cfg *config.Config) ([][]float64, []float64, error) {
	var X [][]float64
	var y []float64

	// Read all CSV files in directory
	files, err := filepath.Glob(filepath.Join(csvDir, "*.csv"))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list CSV files: %v", err)
	}

	if len(files) == 0 {
		return nil, nil, fmt.Errorf("no CSV files found in %s", csvDir)
	}

	// Note: polygonFeed would be used for fetching historical bars for feature extraction
	// For now, we use simplified features from trade data
	_ = feed.NewPolygonFeed(cfg.PolygonAPIKey)

	for _, file := range files {
		fmt.Printf("Processing %s...\n", file)
		
		trades, err := loadTradesFromCSV(file)
		if err != nil {
			fmt.Printf("Warning: Failed to load %s: %v\n", file, err)
			continue
		}

		// For each trade, extract features and determine label
		for _, trade := range trades {
			// Determine label: 1 if hit Target 1, 0 if hit Stop Loss
			label := 0.0
			if trade.Reason == strategy.ExitReasonTarget1 || trade.Reason == strategy.ExitReasonTarget2 {
				label = 1.0
			} else if trade.Reason == strategy.ExitReasonStopLoss {
				label = 0.0
			} else {
				// For other reasons (EOD, Time Decay), use P&L to determine
				if trade.NetPnL > 0 {
					label = 1.0
				} else {
					label = 0.0
				}
			}

			// Extract features (simplified - would need historical bars)
			// For now, create placeholder features based on trade data
			features := extractFeaturesFromTrade(trade)
			if features != nil {
				X = append(X, features)
				y = append(y, label)
			}
		}
	}

	return X, y, nil
}

// loadTradesFromCSV loads trades from a CSV file
func loadTradesFromCSV(filepath string) ([]*TradeData, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	if len(records) < 2 {
		return nil, fmt.Errorf("CSV file has no data rows")
	}

	trades := make([]*TradeData, 0, len(records)-1)
	for i := 1; i < len(records); i++ {
		record := records[i]
		if len(record) < 11 {
			continue // Skip invalid rows
		}

		entryTime, err := time.Parse(time.RFC3339, record[1])
		if err != nil {
			continue
		}
		exitTime, err := time.Parse(time.RFC3339, record[2])
		if err != nil {
			continue
		}

		entryPrice, _ := strconv.ParseFloat(record[4], 64)
		exitPrice, _ := strconv.ParseFloat(record[5], 64)
		shares, _ := strconv.Atoi(record[6])
		netPnL, _ := strconv.ParseFloat(record[10], 64)

		trade := &TradeData{
			Ticker:     record[0],
			EntryTime:  entryTime,
			ExitTime:   exitTime,
			Direction:  record[3],
			EntryPrice: entryPrice,
			ExitPrice:  exitPrice,
			Shares:     shares,
			Reason:     strategy.ExitReason(record[7]),
			NetPnL:     netPnL,
		}
		trades = append(trades, trade)
	}

	return trades, nil
}

// TradeData represents a trade from CSV
type TradeData struct {
	Ticker     string
	EntryTime  time.Time
	ExitTime   time.Time
	Direction  string
	EntryPrice float64
	ExitPrice  float64
	Shares     int
	Reason     strategy.ExitReason
	NetPnL     float64
}

// extractFeaturesFromTrade extracts features from a trade
// This is a simplified version - ideally we'd have the original signal data
func extractFeaturesFromTrade(trade *TradeData) []float64 {
	// Create placeholder features (14 features to match model)
	// In a real implementation, we'd need to fetch historical bars and recalculate
	features := make([]float64, 14)
	
	// Basic features from trade data
	duration := trade.ExitTime.Sub(trade.EntryTime).Minutes()
	features[0] = duration / 390.0 // Normalized duration (0-1)
	
	// Price change
	priceChange := math.Abs(trade.ExitPrice - trade.EntryPrice) / trade.EntryPrice
	features[1] = priceChange // Price change ratio
	
	// Hour of day
	hour := float64(trade.EntryTime.Hour())
	features[2] = (hour - 9.0) / 6.0 // Normalized 9-15 to 0-1
	
	// Direction (SHORT=1, LONG=0)
	if trade.Direction == "SHORT" {
		features[3] = 1.0
	} else {
		features[3] = 0.0
	}
	
	// Fill remaining features with zeros (would need actual signal data)
	for i := 4; i < 14; i++ {
		features[i] = 0.0
	}
	
	return features
}

