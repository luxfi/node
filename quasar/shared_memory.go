// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"github.com/luxfi/ids"
)

// Requests represents shared memory requests
type Requests struct {
	RemoveRequests [][]byte   `serialize:"true"`
	PutRequests    []*Element `serialize:"true"`

	peerChainID ids.ID
}

// Element represents a shared memory element
type Element struct {
	Key    []byte   `serialize:"true"`
	Value  []byte   `serialize:"true"`
	Traits [][]byte `serialize:"true"`
}

// SharedMemory is the interface for shared memory operations
type SharedMemory interface {
	// Get fetches the values corresponding to [keys] that have been sent from
	// [peerChainID]
	//
	// Invariant: Get guarantees that the resulting values array is the same
	//            length as keys.
	Get(peerChainID ids.ID, keys [][]byte) (values [][]byte, err error)
	
	// Indexed returns a paginated result of values that possess any of the
	// given traits and were sent from [peerChainID].
	Indexed(
		peerChainID ids.ID,
		traits [][]byte,
		startTrait,
		startKey []byte,
		limit int,
	) (
		values [][]byte,
		lastTrait,
		lastKey []byte,
		err error,
	)
	
	// Apply performs the requested set of operations by atomically applying
	// [requests] to their respective chainID keys in the map along with the
	// atomic value batch.
	Apply(requests map[ids.ID]*Requests, batch Batch) error
}

// Batch represents an atomic batch operation
type Batch interface {
	// Write adds a put operation to the batch
	Write([]byte, []byte) error
	
	// Delete adds a delete operation to the batch
	Delete([]byte) error
}