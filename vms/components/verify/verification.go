// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package verify

type Verifiable interface {
	Verify() error
}

// ContextInitializable defines the interface for context initialization
type ContextInitializable interface {
	// Initialize initializes the state with the given context
	Initialize(ctx interface{}) error
}

type State interface {
	ContextInitializable
	Verifiable
	IsState
	// InitCtx initializes the state with consensus context
	InitCtx(ctx interface{})
}

type IsState interface {
	isState()
}

type IsNotState interface {
	isState() error
}

// All returns nil if all the verifiables were verified with no errors
func All(verifiables ...Verifiable) error {
	for _, verifiable := range verifiables {
		if err := verifiable.Verify(); err != nil {
			return err
		}
	}
	return nil
}
