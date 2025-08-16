// Package validatorsmock provides mock validators for testing
package validatorsmock

import "github.com/luxfi/node/consensus/validators"

// MockSet is a mock validator set
type MockSet struct {
	validators map[string]validators.Validator
}

// NewMockSet creates a new mock validator set
func NewMockSet() *MockSet {
	return &MockSet{
		validators: make(map[string]validators.Validator),
	}
}

// Add adds a validator to the set
func (m *MockSet) Add(validator validators.Validator) error {
	m.validators[validator.ID()] = validator
	return nil
}

// Remove removes a validator from the set
func (m *MockSet) Remove(validatorID string) error {
	delete(m.validators, validatorID)
	return nil
}

// Get retrieves a validator by ID
func (m *MockSet) Get(validatorID string) (validators.Validator, error) {
	v, ok := m.validators[validatorID]
	if !ok {
		return nil, nil
	}
	return v, nil
}

// Len returns the number of validators
func (m *MockSet) Len() int {
	return len(m.validators)
}