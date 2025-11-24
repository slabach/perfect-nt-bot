package ml

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/perfect-nt-bot/pkg/strategy"
)

// Scorer wraps the ML model and provides scoring functionality
type Scorer struct {
	model *Model
	enabled bool
}

// NewScorer creates a new ML scorer
func NewScorer(modelPath string) (*Scorer, error) {
	scorer := &Scorer{
		enabled: false,
	}
	
	if modelPath == "" {
		// No model path provided, scorer is disabled
		return scorer, nil
	}
	
	// Handle directory paths - if path is a directory, append model.json
	finalModelPath := modelPath
	if stat, err := os.Stat(modelPath); err == nil && stat.IsDir() {
		// Path is a directory, append default filename
		finalModelPath = filepath.Join(modelPath, "model.json")
	} else if !strings.HasSuffix(modelPath, ".json") && !strings.HasSuffix(modelPath, ".txt") {
		// Path doesn't have an extension, assume it should be .json
		finalModelPath = modelPath + ".json"
	}
	
	// Check if model file exists
	if _, err := os.Stat(finalModelPath); os.IsNotExist(err) {
		// Model file doesn't exist, scorer is disabled
		return scorer, nil
	}
	
	// Load model
	model, err := LoadModel(finalModelPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load ML model: %v", err)
	}
	
	scorer.model = model
	scorer.enabled = true
	
	return scorer, nil
}

// ScoreSignal scores an entry signal using the ML model
// Returns a score from 0-1 (probability of hitting Target 1 before Stop Loss)
func (s *Scorer) ScoreSignal(
	signal *strategy.EntrySignal,
	indicators *strategy.IndicatorState,
	recentBars []strategy.Bar,
) float64 {
	if !s.enabled || s.model == nil {
		// Return default score if ML is not enabled
		return 0.5
	}
	
	// Extract features
	features := ExtractFeatures(signal, indicators, recentBars, signal.Timestamp)
	
	// Convert to vector
	featureVector := features.ToVector()
	
	// Predict
	probability := s.model.Predict(featureVector)
	
	return probability
}

// IsEnabled returns whether the ML scorer is enabled
func (s *Scorer) IsEnabled() bool {
	return s.enabled
}

