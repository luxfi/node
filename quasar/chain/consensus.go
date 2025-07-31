// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chain

import (
	"context"
	"errors"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/quasar/choices"
)

var (
	ErrUnknownBlock = errors.New("unknown block")
	ErrBlockNotVerified = errors.New("block not verified")
	ErrNotOracle = errors.New("block is not an oracle block")
)

// Consensus represents a linear consensus instance.
type Consensus interface {
	// Initialize the consensus with the given parameters.
	Initialize(ctx context.Context, params Parameters, lastAcceptedID string, lastAcceptedHeight uint64, lastAcceptedTime uint64) error

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

	// Accept accepts the block (overrides choices.Decidable)
	Accept() error
}

// OracleBlock is an oracle block interface
type OracleBlock interface {
	Block

	// Options returns the oracle block options
	Options(context.Context) ([2]Block, error)
}

// Parameters defines the consensus parameters.
type Parameters struct {
	// K is the number of nodes to poll.
	K int

	// AlphaPreference is the vote threshold to change preference.
	AlphaPreference int

	// AlphaConfidence is the vote threshold for confidence.
	AlphaConfidence int

	// Beta is the number of consecutive successful polls required for finalization.
	Beta int

	// ConcurrentRepolls is the number of concurrent polls.
	ConcurrentRepolls int

	// OptimalProcessing is the number of blocks to process optimally.
	OptimalProcessing int

	// MaxOutstandingItems is the maximum number of outstanding items.
	MaxOutstandingItems int

	// MaxItemProcessingTime is the maximum time to process an item.
	MaxItemProcessingTime int64
}


