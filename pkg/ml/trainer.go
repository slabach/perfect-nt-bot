package ml

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/perfect-nt-bot/pkg/feed"
	"github.com/perfect-nt-bot/pkg/strategy"
)

// TrainingExample represents a single training example
type TrainingExample struct {
	Features []float64
	Label    float64 // 1.0 if hit Target 1 before Stop Loss, 0.0 otherwise
}

// TickerBar represents a bar with its ticker
type TickerBar struct {
	Ticker string
	Bar    feed.Bar
}

// TrainOnHistoricalData trains the ML model on historical backtest data
func TrainOnHistoricalData(
	barsByDate map[time.Time]map[string][]feed.Bar,
	location *time.Location,
	modelPath string,
) error {
	fmt.Println("\n=== Training ML Model on Historical Data ===")

	// Sort dates chronologically
	dates := make([]time.Time, 0, len(barsByDate))
	for date := range barsByDate {
		dates = append(dates, date)
	}
	sort.Slice(dates, func(i, j int) bool {
		return dates[i].Before(dates[j])
	})

	fmt.Printf("Processing %d trading days for training...\n", len(dates))

	// Create strategy engine
	now := time.Now().In(location)
	marketOpen := time.Date(now.Year(), now.Month(), now.Day(), 9, 30, 0, 0, location)
	strategyEngine := strategy.NewStrategyEngine(location, marketOpen)

	var trainingExamples []TrainingExample

	// Process each day
	for dayIdx, date := range dates {
		if dayIdx%10 == 0 && dayIdx > 0 {
			fmt.Printf("  Day %d/%d: %s (collected %d examples so far)\n",
				dayIdx, len(dates), date.Format("2006-01-02"), len(trainingExamples))
		}

		marketOpen := time.Date(date.Year(), date.Month(), date.Day(), 9, 30, 0, 0, location)
		eodTime := time.Date(date.Year(), date.Month(), date.Day(), 15, 50, 0, 0, location)

		strategyEngine.ResetDailyState(marketOpen)

		dayBars := barsByDate[date]

		// Create a flat list of all bars with ticker info, sorted by time
		allBars := make([]TickerBar, 0)
		for ticker, bars := range dayBars {
			for _, bar := range bars {
				allBars = append(allBars, TickerBar{Ticker: ticker, Bar: bar})
			}
		}

		// Sort all bars chronologically
		sort.Slice(allBars, func(i, j int) bool {
			return allBars[i].Bar.Time.Before(allBars[j].Bar.Time)
		})

		// Process bars chronologically
		for _, tickerBar := range allBars {
			if tickerBar.Bar.Time.After(eodTime) {
				break
			}

			// Convert to strategy bar
			strategyBar := strategy.Bar{
				Time:   tickerBar.Bar.Time,
				Open:   tickerBar.Bar.Open,
				High:   tickerBar.Bar.High,
				Low:    tickerBar.Bar.Low,
				Close:  tickerBar.Bar.Close,
				Volume: tickerBar.Bar.Volume,
			}

			// Update indicators
			strategyEngine.UpdateTicker(tickerBar.Ticker, strategyBar)

			// Get ticker state
			tickerState, exists := strategyEngine.GetTickerState(tickerBar.Ticker)
			if !exists || tickerState.VWAP == 0 || tickerState.ATR == 0 || tickerState.RSI == 0 {
				continue // Indicators not ready
			}

			// Check for entry signals (but don't actually take trades)
			openPositions := strategyEngine.GetPositionCount()
			signals := strategyEngine.CheckBothDirections(tickerBar.Ticker, strategyBar, eodTime, openPositions)

			// For each signal, simulate the trade outcome
			for _, signal := range signals {
				// Simulate trade: check if price hits Target 1 before Stop Loss
				label := simulateTradeOutcome(tickerBar.Ticker, signal, allBars, tickerBar.Bar.Time, eodTime)

				// Extract features
				recentBars := strategyEngine.GetRecentBars(tickerBar.Ticker, 10)
				features := ExtractFeatures(signal, tickerState, recentBars, signal.Timestamp)
				featureVector := features.ToVector()

				trainingExamples = append(trainingExamples, TrainingExample{
					Features: featureVector,
					Label:    label,
				})
			}
		}
	}

	if len(trainingExamples) == 0 {
		return fmt.Errorf("no training examples generated")
	}

	fmt.Printf("\nGenerated %d training examples\n", len(trainingExamples))

	// Count wins vs losses
	wins := 0
	for _, ex := range trainingExamples {
		if ex.Label > 0.5 {
			wins++
		}
	}
	fmt.Printf("  Wins: %d (%.1f%%), Losses: %d (%.1f%%)\n",
		wins, float64(wins)/float64(len(trainingExamples))*100,
		len(trainingExamples)-wins, float64(len(trainingExamples)-wins)/float64(len(trainingExamples))*100)

	// Step 2: Fix class imbalance - balance wins and losses
	// Separate wins and losses
	var winExamples, lossExamples []TrainingExample
	for _, ex := range trainingExamples {
		if ex.Label > 0.5 {
			winExamples = append(winExamples, ex)
		} else {
			lossExamples = append(lossExamples, ex)
		}
	}

	fmt.Printf("  Before balancing: Wins: %d, Losses: %d\n", len(winExamples), len(lossExamples))

	// Duplicate wins to match losses (or vice versa if losses are fewer)
	balancedExamples := make([]TrainingExample, 0)
	if len(winExamples) < len(lossExamples) {
		// Duplicate wins to match losses
		duplicatedWins := make([]TrainingExample, len(winExamples))
		copy(duplicatedWins, winExamples)
		for len(duplicatedWins) < len(lossExamples) {
			// Duplicate by appending the original wins
			duplicatedWins = append(duplicatedWins, winExamples...)
		}
		// Truncate if we overshot
		if len(duplicatedWins) > len(lossExamples) {
			duplicatedWins = duplicatedWins[:len(lossExamples)]
		}
		balancedExamples = append(duplicatedWins, lossExamples...)
		fmt.Printf("  After balancing: Duplicated wins to match losses. Total: %d (Wins: %d, Losses: %d)\n",
			len(balancedExamples), len(duplicatedWins), len(lossExamples))
	} else if len(lossExamples) < len(winExamples) {
		// Duplicate losses to match wins
		duplicatedLosses := make([]TrainingExample, len(lossExamples))
		copy(duplicatedLosses, lossExamples)
		for len(duplicatedLosses) < len(winExamples) {
			// Duplicate by appending the original losses
			duplicatedLosses = append(duplicatedLosses, lossExamples...)
		}
		// Truncate if we overshot
		if len(duplicatedLosses) > len(winExamples) {
			duplicatedLosses = duplicatedLosses[:len(winExamples)]
		}
		balancedExamples = append(winExamples, duplicatedLosses...)
		fmt.Printf("  After balancing: Duplicated losses to match wins. Total: %d (Wins: %d, Losses: %d)\n",
			len(balancedExamples), len(winExamples), len(duplicatedLosses))
	} else {
		// Already balanced
		balancedExamples = trainingExamples
		fmt.Printf("  Already balanced: %d examples\n", len(balancedExamples))
	}

	// Use balanced examples for training
	trainingExamples = balancedExamples

	// Prepare training data
	X := make([][]float64, len(trainingExamples))
	y := make([]float64, len(trainingExamples))

	for i, ex := range trainingExamples {
		X[i] = ex.Features
		y[i] = ex.Label
	}

	// Create and train model
	numFeatures := len(X[0])
	model := NewModel(numFeatures)

	fmt.Printf("\nTraining model with %d features...\n", numFeatures)
	fmt.Printf("  Learning rate: 0.1, Epochs: 500\n")

	if err := model.Train(X, y, 0.1, 500); err != nil {
		return fmt.Errorf("failed to train model: %v", err)
	}

	// Ensure model path is a file, not a directory
	// If path ends with a directory separator or is a directory, append filename
	finalModelPath := modelPath
	if stat, err := os.Stat(modelPath); err == nil && stat.IsDir() {
		// Path is a directory, append default filename
		finalModelPath = filepath.Join(modelPath, "model.json")
	} else if !strings.HasSuffix(modelPath, ".json") && !strings.HasSuffix(modelPath, ".txt") {
		// Path doesn't have an extension, assume it should be .json
		finalModelPath = modelPath + ".json"
	}
	
	// Create directory if it doesn't exist
	dir := filepath.Dir(finalModelPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create model directory: %v", err)
	}
	
	// Save model
	if err := model.Save(finalModelPath); err != nil {
		return fmt.Errorf("failed to save model: %v", err)
	}

	fmt.Printf("\n✓ Model trained and saved to: %s\n", finalModelPath)

	return nil
}

// simulateTradeOutcome simulates a trade and returns 1.0 if Target 1 hit before Stop Loss, 0.0 otherwise
func simulateTradeOutcome(
	ticker string,
	signal *strategy.EntrySignal,
	allBars []TickerBar,
	entryTime time.Time,
	eodTime time.Time,
) float64 {
	// Find bars after entry time for this ticker
	for _, tickerBar := range allBars {
		if tickerBar.Ticker != ticker {
			continue
		}
		if tickerBar.Bar.Time.Before(entryTime) || tickerBar.Bar.Time.Equal(entryTime) {
			continue
		}
		if tickerBar.Bar.Time.After(eodTime) {
			break
		}

		// Check high/low prices to see if stop or target was hit intra-bar
		// For SHORT: stop is above entry (check High), target is below entry (check Low)
		// For LONG: stop is below entry (check Low), target is above entry (check High)
		
		var stopHit, target1Hit bool
		
		if signal.Direction == "SHORT" {
			// Stop loss: price went above stop level (check high)
			stopHit = tickerBar.Bar.High >= signal.StopLoss
			// Target 1: price went below target level (check low)
			target1Hit = tickerBar.Bar.Low <= signal.Target1
		} else {
			// Stop loss: price went below stop level (check low)
			stopHit = tickerBar.Bar.Low <= signal.StopLoss
			// Target 1: price went above target level (check high)
			target1Hit = tickerBar.Bar.High >= signal.Target1
		}

		// If both hit in same bar, check which happened first
		// For simplicity, we'll check stop first (more conservative)
		if stopHit {
			return 0.0 // Loss
		}
		
		if target1Hit {
			return 1.0 // Win
		}
	}

	// If neither hit by EOD, consider it a loss (didn't reach target)
	return 0.0
}

