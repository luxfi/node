// Copyright (C) 2024, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrap

import (
	"context"
	"fmt"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/quasar/engine/chain/block"
	"github.com/luxfi/node/utils/set"
)

// Config contains the configuration for bootstrapping
type Config struct {
	// MaxOutstandingRequests is the maximum number of outstanding fetch requests
	MaxOutstandingRequests int

	// MaxProcessingTime is the maximum time to process a single item
	MaxProcessingTime time.Duration

	// RequestTimeout is the timeout for individual requests
	RequestTimeout time.Duration

	// Log is the logger
	Log log.Logger
}

// Bootstrapper handles the bootstrapping process for linear chains
type Bootstrapper struct {
	config     Config
	currentJob *job
	pending    map[ids.ID]struct{}
	finished   bool
	onFinished func(lastReqID uint32) error
}

type job struct {
	numAccepted uint64
	numDropped  uint64
	missingIDs  set.Set[ids.ID]
}

// New creates a new bootstrapper
func New(config Config, onFinished func(lastReqID uint32) error) *Bootstrapper {
	return &Bootstrapper{
		config:     config,
		pending:    make(map[ids.ID]struct{}),
		onFinished: onFinished,
	}
}

// Start begins the bootstrapping process
func (b *Bootstrapper) Start(ctx context.Context, startingHeight uint64) error {
	b.currentJob = &job{
		missingIDs: set.Set[ids.ID]{},
	}

	b.config.Log.Info("starting bootstrapper",
		"startingHeight", startingHeight,
	)

	// Initialize bootstrapping
	return nil
}

// Add adds blocks to be fetched
func (b *Bootstrapper) Add(blockIDs ...ids.ID) error {
	if b.finished {
		return fmt.Errorf("bootstrapper already finished")
	}

	for _, blockID := range blockIDs {
		if _, exists := b.pending[blockID]; !exists {
			b.pending[blockID] = struct{}{}
			b.currentJob.missingIDs.Add(blockID)
		}
	}

	return nil
}

// Put handles received blocks
func (b *Bootstrapper) Put(ctx context.Context, nodeID ids.NodeID, requestID uint32, blkIntf interface{}) error {
	// Process received block
	b.currentJob.numAccepted++

	// Remove from pending
	if blk, ok := blkIntf.(block.Block); ok {
		delete(b.pending, blk.ID())
		b.currentJob.missingIDs.Remove(blk.ID())
	}

	// Check if we're done
	if len(b.pending) == 0 {
		return b.finish(requestID)
	}

	return nil
}

// GetFailed handles failed fetch requests
func (b *Bootstrapper) GetFailed(nodeID ids.NodeID, requestID uint32) error {
	b.currentJob.numDropped++
	return nil
}

// Timeout handles request timeouts
func (b *Bootstrapper) Timeout() error {
	// Handle timeout logic
	return nil
}

// finish completes the bootstrapping process
func (b *Bootstrapper) finish(lastReqID uint32) error {
	b.finished = true

	b.config.Log.Info("bootstrapping finished",
		"numAccepted", b.currentJob.numAccepted,
		"numDropped", b.currentJob.numDropped,
	)

	if b.onFinished != nil {
		return b.onFinished(lastReqID)
	}

	return nil
}

// IsBootstrapped returns whether bootstrapping is complete
func (b *Bootstrapper) IsBootstrapped() bool {
	return b.finished
}

// Clear resets the bootstrapper state
func (b *Bootstrapper) Clear() {
	b.currentJob = nil
	b.pending = make(map[ids.ID]struct{})
	b.finished = false
}
