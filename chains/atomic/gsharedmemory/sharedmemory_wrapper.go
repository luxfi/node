// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gsharedmemory

import (
	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/quasar"
	db "github.com/luxfi/database"
)

// SharedMemoryWrapper wraps quasar.SharedMemory to implement atomic.SharedMemory
type SharedMemoryWrapper struct {
	sm quasar.SharedMemory
}

// NewSharedMemoryWrapper creates a new wrapper
func NewSharedMemoryWrapper(sm quasar.SharedMemory) atomic.SharedMemory {
	return &SharedMemoryWrapper{sm: sm}
}

// Get implements atomic.SharedMemory
func (w *SharedMemoryWrapper) Get(peerChainID ids.ID, keys [][]byte) (values [][]byte, err error) {
	return w.sm.Get(peerChainID, keys)
}

// Indexed implements atomic.SharedMemory
func (w *SharedMemoryWrapper) Indexed(
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
) {
	return w.sm.Indexed(peerChainID, traits, startTrait, startKey, limit)
}

// Apply implements atomic.SharedMemory
func (w *SharedMemoryWrapper) Apply(requests map[ids.ID]*atomic.Requests, batches ...db.Batch) error {
	// Convert atomic.Requests to quasar.Requests
	consensusRequests := make(map[ids.ID]*quasar.Requests, len(requests))
	for chainID, req := range requests {
		consensusReq := &quasar.Requests{
			RemoveRequests: req.RemoveRequests,
			PutRequests:    make([]quasar.Element, len(req.PutRequests)),
		}
		for i, elem := range req.PutRequests {
			consensusReq.PutRequests[i] = quasar.Element{
				Key:    elem.Key,
				Value:  elem.Value,
				Traits: elem.Traits,
			}
		}
		consensusRequests[chainID] = consensusReq
	}
	
	// Convert batches to interface{}
	interfaceBatches := make([]interface{}, len(batches))
	for i, batch := range batches {
		interfaceBatches[i] = batch
	}
	
	return w.sm.Apply(consensusRequests, interfaceBatches...)
}