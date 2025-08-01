// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"crypto/rand"
	"github.com/luxfi/ids"
)

// generateTestID creates a random test ID
func generateTestID() ids.ID {
	var id ids.ID
	rand.Read(id[:])
	return id
}

// generateTestNodeID creates a random test node ID
func generateTestNodeID() ids.NodeID {
	var id ids.NodeID
	var bytes [20]byte
	rand.Read(bytes[:])
	copy(id[:], bytes[:])
	return id
}