// Package validators provides validator management functionality
package validators

// Set manages a set of validators
type Set interface {
	// Add adds a validator to the set
	Add(validator Validator) error

	// Remove removes a validator from the set
	Remove(validatorID string) error

	// Get retrieves a validator by ID
	Get(validatorID string) (Validator, error)

	// Len returns the number of validators
	Len() int
}

// Validator represents a network validator
type Validator interface {
	// ID returns the validator's unique identifier
	ID() string

	// Weight returns the validator's stake weight
	Weight() uint64

	// PublicKey returns the validator's public key
	PublicKey() []byte
}
