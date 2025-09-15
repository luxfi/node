// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snow

// State represents the state of consensus
type State uint8

const (
	// Bootstrapping means consensus is bootstrapping
	Bootstrapping State = iota
	// NormalOp means consensus is operating normally
	NormalOp
)

// String returns a string representation of the state
func (s State) String() string {
	switch s {
	case Bootstrapping:
		return "Bootstrapping"
	case NormalOp:
		return "NormalOp"
	default:
		return "Unknown"
	}
}