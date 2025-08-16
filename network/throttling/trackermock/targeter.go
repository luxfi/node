// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trackermock

import (
	"testing"

	"github.com/luxfi/ids"
)

// Targeter is a mock targeter for testing
type Targeter struct {
	T            *testing.T
	TargetUsageF func(ids.NodeID) float64
}

// NewTargeter creates a new mock targeter
func NewTargeter(t *testing.T) *Targeter {
	return &Targeter{T: t}
}

func (t *Targeter) TargetUsage(nodeID ids.NodeID) float64 {
	if t.TargetUsageF != nil {
		return t.TargetUsageF(nodeID)
	}
	return 0.5 // Default target usage
}
