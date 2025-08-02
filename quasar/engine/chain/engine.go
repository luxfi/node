// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"context"
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/choices"
	"github.com/luxfi/node/quasar/engine/core"
)

var (
	// ErrNotOracle is returned when the block is not an oracle block
	ErrNotOracle = errors.New("block is not an oracle block")
)

// Engine is a linear consensus engine.
type Engine interface {
	core.Engine

	// Initialize this engine.
	Initialize(ctx context.Context, params Parameters) error

	// GetBlock retrieves a block by its ID.
	GetBlock(blkID ids.ID) (Block, error)

	// GetAncestor retrieves an ancestor block at the given height.
	GetAncestor(blkID ids.ID, height uint64) (ids.ID, error)

	// LastAccepted returns the ID of the last accepted block.
	LastAccepted() (ids.ID, uint64)

	// VerifyHeightIndex returns whether height index is enabled.
	VerifyHeightIndex() error
}

// Parameters defines the parameters for the linear consensus engine.
type Parameters struct {
	// BatchSize is the number of blocks to batch together.
	BatchSize int

	// The consensus parameters.
	ConsensusParams interface{}
}

// Block is a block that can be decided.
type Block interface {
	choices.Decidable

	// Parent returns the parent block ID.
	Parent() ids.ID

	// Height returns the height of the block.
	Height() uint64

	// Time returns the time the block was created.
	Time() uint64

	// Verify that the block is valid.
	Verify(context.Context) error

	// Bytes returns the byte representation of the block.
	Bytes() []byte
}

// VM defines the interface that the consensus engine uses to interact with the VM.
type VM interface {
	// GetBlock retrieves a block by its ID.
	GetBlock(ctx context.Context, blkID ids.ID) (Block, error)

	// GetAncestor retrieves an ancestor block at the given height.
	GetAncestor(ctx context.Context, blkID ids.ID, height uint64) (ids.ID, error)

	// LastAccepted returns the ID of the last accepted block.
	LastAccepted(ctx context.Context) (ids.ID, error)

	// ParseBlock parses a block from bytes.
	ParseBlock(ctx context.Context, b []byte) (Block, error)

	// BuildBlock builds a new block.
	BuildBlock(ctx context.Context) (Block, error)

	// SetPreference sets the preferred block.
	SetPreference(ctx context.Context, blkID ids.ID) error

	// VerifyHeightIndex returns whether height index is enabled.
	VerifyHeightIndex(ctx context.Context) error
}

// Consensus represents a linear consensus instance.
type Consensus interface {
	// Initialize the consensus with the given parameters.
	Initialize(ctx context.Context, params Parameters) error

	// Parameters returns the parameters of this consensus instance.
	Parameters() Parameters

	// Add a block to consensus consideration.
	Add(ctx context.Context, block Block) error

	// Issued returns true if the block has been issued into consensus.
	Issued(blk Block) bool

	// Processing returns true if the block is currently processing.
	Processing(blkID ids.ID) bool

	// Decided returns true if the block has been decided.
	Decided(blk Block) bool

	// IsPreferred returns true if the block is on the preferred chain.
	IsPreferred(blk Block) bool

	// Preference returns the current preferred block.
	Preference() ids.ID

	// RecordPoll records the results of a network poll.
	RecordPoll(ctx context.Context, votes []ids.ID) error

	// Finalized returns true if consensus has finalized.
	Finalized() bool

	// HealthCheck returns the health status of consensus.
	HealthCheck(ctx context.Context) (interface{}, error)

	// NumProcessing returns the number of currently processing blocks.
	NumProcessing() int
}