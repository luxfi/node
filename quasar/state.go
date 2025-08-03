// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

// State represents the state of the consensus
type State uint8

const (
	// Bootstrapping state
	Bootstrapping State = iota
	// NormalOp state (normal operation)
	NormalOp
	// StateSyncing state
	StateSyncing
)

// String returns the string representation of the state
func (s State) String() string {
	switch s {
	case Bootstrapping:
		return "Bootstrapping"
	case NormalOp:
		return "NormalOp"
	case StateSyncing:
		return "StateSyncing"
	default:
		return "Unknown"
	}
}