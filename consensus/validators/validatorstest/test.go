// Package validatorstest provides test utilities for validators
package validatorstest

import (
	"testing"
)

// TestValidator is a test validator implementation
type TestValidator struct {
	id     string
	weight uint64
	pubKey []byte
}

// NewTestValidator creates a new test validator
func NewTestValidator(id string, weight uint64) *TestValidator {
	return &TestValidator{
		id:     id,
		weight: weight,
		pubKey: []byte(id),
	}
}

// ID returns the validator's unique identifier
func (v *TestValidator) ID() string {
	return v.id
}

// Weight returns the validator's stake weight
func (v *TestValidator) Weight() uint64 {
	return v.weight
}

// PublicKey returns the validator's public key
func (v *TestValidator) PublicKey() []byte {
	return v.pubKey
}

// Helper provides test helper functions
func Helper(t *testing.T) {
	t.Helper()
}
