// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package wave

// Factory creates wave threshold mechanisms
// (Previously QuorumFactory in Avalanche)
type Factory struct {
	alphaPreference int
	alphaConfidence int
}

// NewFactory creates a new wave threshold factory
func NewFactory(alphaPreference, alphaConfidence int) *Factory {
	return &Factory{
		alphaPreference: alphaPreference,
		alphaConfidence: alphaConfidence,
	}
}

// NewStatic creates a static wave threshold
func (f *Factory) NewStatic() Threshold {
	// Use preference alpha for static threshold
	return NewStaticWaveThreshold(f.alphaPreference)
}

// NewDynamic creates a dynamic wave threshold with decoupled alphas
func (f *Factory) NewDynamic() Threshold {
	return NewDynamicWaveThreshold(f.alphaPreference, f.alphaConfidence)
}

// New creates a threshold of the specified type
func (f *Factory) New(thresholdType ThresholdType) Threshold {
	switch thresholdType {
	case Static:
		return f.NewStatic()
	case Dynamic:
		return f.NewDynamic()
	default:
		return f.NewDynamic()
	}
}

// Config represents wave threshold configuration
type Config struct {
	Type            ThresholdType
	AlphaPreference int
	AlphaConfidence int
}

// NewFromConfig creates a threshold from configuration
func NewFromConfig(cfg Config) Threshold {
	factory := NewFactory(cfg.AlphaPreference, cfg.AlphaConfidence)
	return factory.New(cfg.Type)
}