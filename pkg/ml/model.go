package ml

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
)

// Model represents a simple logistic regression model
type Model struct {
	Weights []float64 // Weights for each feature
	Bias    float64   // Bias term
	NumFeatures int   // Number of input features
}

// NewModel creates a new model with random initialization
func NewModel(numFeatures int) *Model {
	weights := make([]float64, numFeatures)
	// Initialize with small random values
	for i := range weights {
		weights[i] = (math.Sin(float64(i)) * 0.1) // Simple initialization
	}
	return &Model{
		Weights:     weights,
		Bias:        0.0,
		NumFeatures: numFeatures,
	}
}

// Predict returns the probability (0-1) of hitting Target 1 before Stop Loss
func (m *Model) Predict(features []float64) float64 {
	if len(features) != m.NumFeatures {
		return 0.5 // Default probability if feature count mismatch
	}
	
	// Linear combination
	z := m.Bias
	for i := 0; i < len(features); i++ {
		z += m.Weights[i] * features[i]
	}
	
	// Sigmoid activation
	return sigmoid(z)
}

// sigmoid function: 1 / (1 + e^(-z))
func sigmoid(z float64) float64 {
	// Clamp z to prevent overflow
	if z > 20 {
		return 1.0
	}
	if z < -20 {
		return 0.0
	}
	return 1.0 / (1.0 + math.Exp(-z))
}

// Train trains the model using gradient descent
func (m *Model) Train(X [][]float64, y []float64, learningRate float64, epochs int) error {
	if len(X) == 0 || len(X) != len(y) {
		return fmt.Errorf("invalid training data: X and y must have same length")
	}
	
	if len(X[0]) != m.NumFeatures {
		return fmt.Errorf("feature count mismatch: expected %d, got %d", m.NumFeatures, len(X[0]))
	}
	
	for epoch := 0; epoch < epochs; epoch++ {
		totalLoss := 0.0
		
		for i := 0; i < len(X); i++ {
			// Forward pass
			prediction := m.Predict(X[i])
			error := prediction - y[i]
			totalLoss += error * error
			
			// Backward pass (gradient descent)
			// Update bias
			m.Bias -= learningRate * error
			
			// Update weights
			for j := 0; j < len(m.Weights); j++ {
				m.Weights[j] -= learningRate * error * X[i][j]
			}
		}
		
		// Optional: print loss every 100 epochs
		if epoch%100 == 0 && epoch > 0 {
			avgLoss := totalLoss / float64(len(X))
			fmt.Printf("Epoch %d: Average Loss = %.6f\n", epoch, avgLoss)
		}
	}
	
	return nil
}

// Save saves the model to a file
func (m *Model) Save(filepath string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal model: %v", err)
	}
	
	return os.WriteFile(filepath, data, 0644)
}

// Load loads the model from a file
func LoadModel(filepath string) (*Model, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read model file: %v", err)
	}
	
	var model Model
	if err := json.Unmarshal(data, &model); err != nil {
		return nil, fmt.Errorf("failed to unmarshal model: %v", err)
	}
	
	return &model, nil
}

// ModelData represents the serializable model data
type ModelData struct {
	Weights     []float64 `json:"weights"`
	Bias        float64   `json:"bias"`
	NumFeatures int       `json:"num_features"`
}

