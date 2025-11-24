package ml

import (
	"math"
	"time"

	"github.com/perfect-nt-bot/pkg/strategy"
)

// Features represents extracted features for ML model
type Features struct {
	// Core indicators
	VWAPExtension float64 // Absolute value of VWAP extension in ATR multiples
	RSI           float64 // RSI value (normalized 0-1)
	VolumeRatio   float64 // Current volume / VolumeMA (normalized 0-2x to 0-1)
	ATRPriceRatio float64 // ATR / Price (volatility relative to price)

	// Pattern features
	PatternType       int     // Encoded pattern type (0-6)
	PatternConfidence float64 // Pattern confidence (0-1)

	// Momentum features
	PriceMomentum  float64 // Price change from previous bar (normalized)
	Momentum5Bars  float64 // Average momentum over last 5 bars
	Momentum10Bars float64 // Average momentum over last 10 bars

	// Time features
	HourOfDay       float64 // Hour of day (9-15, normalized 0-1)
	MinutesFromOpen float64 // Minutes since market open (normalized 0-1)

	// Risk features
	StopDistance    float64 // Stop loss distance in ATR multiples
	Target1Distance float64 // Target 1 distance in ATR multiples
	RiskRewardRatio float64 // Target1Distance / StopDistance
}

// ExtractFeatures extracts features from a signal and historical bars
func ExtractFeatures(
	signal *strategy.EntrySignal,
	indicators *strategy.IndicatorState,
	recentBars []strategy.Bar,
	currentTime time.Time,
) Features {
	features := Features{}

	// Core indicators
	features.VWAPExtension = math.Abs(signal.VWAPExtension)
	if features.VWAPExtension > 3.0 {
		features.VWAPExtension = 3.0 // Cap at 3.0
	}
	features.VWAPExtension = features.VWAPExtension / 3.0 // Normalize to 0-1

	// RSI normalization (0-100 to 0-1)
	features.RSI = signal.RSI / 100.0

	// Volume ratio
	if indicators.VolumeMA > 0 {
		volumeRatio := float64(signal.Volume) / indicators.VolumeMA
		if volumeRatio > 2.0 {
			volumeRatio = 2.0 // Cap at 2x
		}
		features.VolumeRatio = volumeRatio / 2.0 // Normalize to 0-1
	}

	// ATR/Price ratio
	if signal.EntryPrice > 0 && indicators.ATR > 0 {
		features.ATRPriceRatio = indicators.ATR / signal.EntryPrice
		// Normalize: typically 0.01-0.10, cap at 0.15
		if features.ATRPriceRatio > 0.15 {
			features.ATRPriceRatio = 0.15
		}
		features.ATRPriceRatio = features.ATRPriceRatio / 0.15 // Normalize to 0-1
	}

	// Pattern encoding
	features.PatternType = int(signal.Pattern)
	features.PatternConfidence = signal.Confidence

	// Momentum features
	if len(recentBars) >= 2 {
		// Price momentum (current - previous)
		priceChange := recentBars[len(recentBars)-1].Close - recentBars[len(recentBars)-2].Close
		// Normalize by ATR
		if indicators.ATR > 0 {
			features.PriceMomentum = priceChange / indicators.ATR
			// Cap at ±2 ATR and normalize to 0-1
			if features.PriceMomentum > 2.0 {
				features.PriceMomentum = 2.0
			} else if features.PriceMomentum < -2.0 {
				features.PriceMomentum = -2.0
			}
			features.PriceMomentum = (features.PriceMomentum + 2.0) / 4.0 // Normalize to 0-1
		}

		// 5-bar momentum
		if len(recentBars) >= 5 {
			momentum5 := 0.0
			for i := len(recentBars) - 5; i < len(recentBars); i++ {
				if i > 0 {
					momentum5 += recentBars[i].Close - recentBars[i-1].Close
				}
			}
			if indicators.ATR > 0 {
				momentum5 = momentum5 / (indicators.ATR * 5.0)
				if momentum5 > 2.0 {
					momentum5 = 2.0
				} else if momentum5 < -2.0 {
					momentum5 = -2.0
				}
				features.Momentum5Bars = (momentum5 + 2.0) / 4.0 // Normalize to 0-1
			}
		}

		// 10-bar momentum
		if len(recentBars) >= 10 {
			momentum10 := 0.0
			for i := len(recentBars) - 10; i < len(recentBars); i++ {
				if i > 0 {
					momentum10 += recentBars[i].Close - recentBars[i-1].Close
				}
			}
			if indicators.ATR > 0 {
				momentum10 = momentum10 / (indicators.ATR * 10.0)
				if momentum10 > 2.0 {
					momentum10 = 2.0
				} else if momentum10 < -2.0 {
					momentum10 = -2.0
				}
				features.Momentum10Bars = (momentum10 + 2.0) / 4.0 // Normalize to 0-1
			}
		}
	}

	// Time features
	hour := currentTime.Hour()
	features.HourOfDay = float64(hour-9) / 6.0 // 9-15 normalized to 0-1
	if features.HourOfDay < 0 {
		features.HourOfDay = 0
	} else if features.HourOfDay > 1.0 {
		features.HourOfDay = 1.0
	}

	// Minutes from market open (9:30 AM)
	marketOpen := time.Date(currentTime.Year(), currentTime.Month(), currentTime.Day(), 9, 30, 0, 0, currentTime.Location())
	minutesFromOpen := currentTime.Sub(marketOpen).Minutes()
	features.MinutesFromOpen = minutesFromOpen / 390.0 // 6.5 hours = 390 minutes, normalize to 0-1
	if features.MinutesFromOpen < 0 {
		features.MinutesFromOpen = 0
	} else if features.MinutesFromOpen > 1.0 {
		features.MinutesFromOpen = 1.0
	}

	// Risk features
	if indicators.ATR > 0 {
		stopDistance := math.Abs(signal.EntryPrice - signal.StopLoss)
		features.StopDistance = stopDistance / indicators.ATR
		if features.StopDistance > 2.0 {
			features.StopDistance = 2.0
		}
		features.StopDistance = features.StopDistance / 2.0 // Normalize to 0-1

		target1Distance := math.Abs(signal.EntryPrice - signal.Target1)
		features.Target1Distance = target1Distance / indicators.ATR
		if features.Target1Distance > 2.0 {
			features.Target1Distance = 2.0
		}
		features.Target1Distance = features.Target1Distance / 2.0 // Normalize to 0-1

		if features.StopDistance > 0 {
			features.RiskRewardRatio = features.Target1Distance / features.StopDistance
			if features.RiskRewardRatio > 3.0 {
				features.RiskRewardRatio = 3.0
			}
			features.RiskRewardRatio = features.RiskRewardRatio / 3.0 // Normalize to 0-1
		}
	}

	return features
}

// ToVector converts features to a vector for ML model input
func (f Features) ToVector() []float64 {
	return []float64{
		f.VWAPExtension,
		f.RSI,
		f.VolumeRatio,
		f.ATRPriceRatio,
		float64(f.PatternType) / 6.0, // Normalize pattern type
		f.PatternConfidence,
		f.PriceMomentum,
		f.Momentum5Bars,
		f.Momentum10Bars,
		f.HourOfDay,
		f.MinutesFromOpen,
		f.StopDistance,
		f.Target1Distance,
		f.RiskRewardRatio,
	}
}
