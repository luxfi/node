// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package focus

// Factory creates focus tracking mechanisms
// (Previously ConfidenceFactory in Avalanche)
type Factory struct {
	beta int
}

// NewFactory creates a new focus tracker factory
func NewFactory(beta int) *Factory {
	return &Factory{
		beta: beta,
	}
}

// NewBinary creates a binary focus tracker
func (f *Factory) NewBinary() Confidence {
	return NewBinaryFocusTracker(f.beta)
}

// NewUnary creates a unary focus tracker
func (f *Factory) NewUnary() Confidence {
	return NewUnaryFocusTracker(f.beta)
}

// New creates a focus tracker of the specified type
func (f *Factory) New(focusType FocusType) Confidence {
	switch focusType {
	case BinaryFocus:
		return f.NewBinary()
	case UnaryFocus:
		return f.NewUnary()
	default:
		return f.NewBinary()
	}
}

// Config represents focus tracking configuration
type Config struct {
	Type FocusType
	Beta int
}

// NewFromConfig creates a focus tracker from configuration
func NewFromConfig(cfg Config) Confidence {
	factory := NewFactory(cfg.Beta)
	return factory.New(cfg.Type)
}

// Parameters holds all photonic consensus parameters
// (Maps to Avalanche consensus Parameters)
type Parameters struct {
	K               int // Photon sample size
	AlphaPreference int // Wave threshold for preference
	AlphaConfidence int // Wave threshold for confidence
	Beta            int // Focus rounds for finality
}