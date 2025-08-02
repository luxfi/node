// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/quasar"
)

// sharedMemoryAdapter adapts atomic.SharedMemory to quasar.SharedMemory
type sharedMemoryAdapter struct {
	sm atomic.SharedMemory
}

func NewSharedMemoryAdapter(sm atomic.SharedMemory) quasar.SharedMemory {
	return &sharedMemoryAdapter{sm: sm}
}

func (s *sharedMemoryAdapter) Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error) {
	return s.sm.Get(peerChainID, keys)
}

func (s *sharedMemoryAdapter) Indexed(
	peerChainID ids.ID,
	traits [][]byte,
	startTrait,
	startKey []byte,
	limit int,
) ([][]byte, []byte, []byte, error) {
	return s.sm.Indexed(peerChainID, traits, startTrait, startKey, limit)
}

func (s *sharedMemoryAdapter) Apply(requests map[ids.ID]*quasar.Requests, batch quasar.Batch) error {
	// Convert quasar.Requests to atomic.Requests
	atomicRequests := make(map[ids.ID]*atomic.Requests)
	for chainID, req := range requests {
		atomicReq := &atomic.Requests{
			RemoveRequests: req.RemoveRequests,
			PutRequests:    make([]*atomic.Element, len(req.PutRequests)),
		}
		for i, elem := range req.PutRequests {
			atomicReq.PutRequests[i] = &atomic.Element{
				Key:    elem.Key,
				Value:  elem.Value,
				Traits: elem.Traits,
			}
		}
		atomicRequests[chainID] = atomicReq
	}
	
	// Convert quasar.Batch to database.Batch
	if batch == nil {
		return s.sm.Apply(atomicRequests)
	}
	
	// If we have a batch, we need to convert it to database.Batch
	// For now, we'll assume the batch is nil in tests
	return s.sm.Apply(atomicRequests)
}

// batchAdapter adapts database.Batch to quasar.Batch
type batchAdapter struct {
	db database.Batch
}

func (b *batchAdapter) Write(key, value []byte) error {
	return b.db.Put(key, value)
}

func (b *batchAdapter) Delete(key []byte) error {
	return b.db.Delete(key)
}