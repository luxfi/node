// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package beam

import (
	"context"
	"fmt"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/snow/choices"
	"github.com/luxfi/node/consensus/engine/chain"
)

// BeamBlock implements the Block interface for beam consensus
// (Previously a concrete implementation of snowman.Block)
type BeamBlock struct {
	// Embedded linear block for compatibility
	linear.Block

	id          ids.ID
	parentID    ids.ID
	height      uint64
	timestamp   time.Time
	bytes       []byte
	status      choices.Status
	beamScore   uint64 // Consensus score
	photonCount int    // Number of photon queries
}

// NewBeamBlock creates a new beam block
func NewBeamBlock(
	id ids.ID,
	parentID ids.ID,
	height uint64,
	timestamp time.Time,
	bytes []byte,
) *BeamBlock {
	return &BeamBlock{
		id:        id,
		parentID:  parentID,
		height:    height,
		timestamp: timestamp,
		bytes:     bytes,
		status:    choices.Processing,
	}
}

// ID returns the block ID
func (b *BeamBlock) ID() ids.ID {
	return b.id
}

// Parent returns the parent block ID
func (b *BeamBlock) Parent() ids.ID {
	return b.parentID
}

// Height returns the block height
func (b *BeamBlock) Height() uint64 {
	return b.height
}

// Timestamp returns the block timestamp
func (b *BeamBlock) Timestamp() time.Time {
	return b.timestamp
}

// Bytes returns the block bytes
func (b *BeamBlock) Bytes() []byte {
	return b.bytes
}

// Status returns the block status
func (b *BeamBlock) Status() choices.Status {
	return b.status
}

// BeamHeight returns the block's position in the beam
func (b *BeamBlock) BeamHeight() uint64 {
	return b.height
}

// BeamScore returns the block's consensus score
func (b *BeamBlock) BeamScore() uint64 {
	return b.beamScore
}

// Photons returns the number of photon queries for this block
func (b *BeamBlock) Photons() int {
	return b.photonCount
}

// Accept marks the block as accepted (finalized in the beam)
func (b *BeamBlock) Accept(ctx context.Context) error {
	if b.status == choices.Accepted {
		return fmt.Errorf("block %s already accepted", b.id)
	}
	b.status = choices.Accepted
	return nil
}

// Reject marks the block as rejected (excluded from the beam)
func (b *BeamBlock) Reject(ctx context.Context) error {
	if b.status == choices.Rejected {
		return fmt.Errorf("block %s already rejected", b.id)
	}
	b.status = choices.Rejected
	return nil
}

// Verify ensures the block is valid according to beam rules
func (b *BeamBlock) Verify(ctx context.Context) error {
	// Basic verification
	if b.height == 0 && b.parentID != ids.Empty {
		return fmt.Errorf("genesis block must have empty parent")
	}
	if b.height > 0 && b.parentID == ids.Empty {
		return fmt.Errorf("non-genesis block must have parent")
	}
	return nil
}

// IncrementPhotons increments the photon query count
func (b *BeamBlock) IncrementPhotons() {
	b.photonCount++
}

// UpdateBeamScore updates the consensus score
func (b *BeamBlock) UpdateBeamScore(score uint64) {
	b.beamScore = score
}

// BeamBlockWrapper wraps an existing linear.Block for beam consensus
type BeamBlockWrapper struct {
	linear.Block
	beamHeight   uint64
	beamScore    uint64
	photonCount  int
}

// WrapBlock wraps an existing linear block for beam consensus
func WrapBlock(block linear.Block, height uint64) *BeamBlockWrapper {
	return &BeamBlockWrapper{
		Block:      block,
		beamHeight: height,
	}
}

// BeamHeight returns the block's position in the beam
func (w *BeamBlockWrapper) BeamHeight() uint64 {
	return w.beamHeight
}

// BeamScore returns the block's consensus score
func (w *BeamBlockWrapper) BeamScore() uint64 {
	return w.beamScore
}

// Photons returns the number of photon queries for this block
func (w *BeamBlockWrapper) Photons() int {
	return w.photonCount
}