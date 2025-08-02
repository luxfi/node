// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

// State represents the current state of the consensus engine
type State uint32

const (
	// Bootstrapping state
	Bootstrapping State = iota
	// NormalOp represents normal operation state
	NormalOp
	// StateSyncing state
	StateSyncing
)

// EngineState represents the state of the consensus engine
type EngineState struct {
	State State
}

// Set sets the engine state
func (s *EngineState) Set(state EngineState) {
	*s = state
}

// Get gets the engine state
func (s *EngineState) Get() *EngineState {
	return s
}
