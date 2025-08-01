// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"context"

	"github.com/luxfi/node/consensus/choices"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/bag"
)

// Block represents a snowman block
type Block interface {
	choices.Decidable

	// Parent returns the block's parent ID
	Parent() ids.ID

	// Verify that the state transition this block would make if accepted is
	// valid. If the state transition is invalid, a non-nil error should be
	// returned.
	//
	// It is guaranteed that the Parent has been successfully verified.
	Verify(context.Context) error

	// Bytes returns the binary representation of this block
	//
	// This is the byte representation that will be hashed and sent to consensus.
	// The bytes should be able to be parsed into the same block
	Bytes() []byte

	// Height returns the height of the block
	Height() uint64
}

// Consensus represents a general snowman consensus instance
type Consensus interface {
	// Takes in the context, snowball parameters, and the last accepted block
	Initialize(
		ctx *Context,
		params Parameters,
		lastAcceptedID ids.ID,
		lastAcceptedHeight uint64,
		lastAcceptedTime uint64,
	) error

	// Returns the parameters that describe this snowman instance
	Parameters() Parameters

	// Adds a new decision. Assumes the dependency has already been added.
	// Returns if a critical error has occurred.
	Add(context.Context, Block) error

	// Decided returns true if the block has been decided.
	Decided(Block) bool

	// Processing returns true if the block ID is currently processing.
	Processing(ids.ID) bool

	// IsPreferred returns true if the block is currently on the preferred
	// chain.
	IsPreferred(Block) bool

	// Returns the ID of the tail of the preferred sequence
	// (i.e., the preference with no children)
	Preference() ids.ID

	// RecordPoll collects the results of a network poll. If a critical error
	// occurs, changes should be reverted. Returns true if any blocks were
	// finalized.
	RecordPoll(context.Context, bag.Bag[ids.ID]) (bool, error)

	// Finalized returns true if all decisions that have been added have been
	// finalized. Note, it is possible that after returning finalized, a new
	// decision may be added such that this instance is no longer finalized.
	Finalized() bool

	// HealthCheck returns information about the consensus health.
	HealthCheck(context.Context) (interface{}, error)
}

// Parameters are the parameters for snowman
type Parameters struct {
	// K is the number of consecutive successful queries required for finalization.
	K int

	// Alpha is the number of validators that must prefer a block for it to be
	// accepted.
	Alpha int

	// BetaVirtuous is the number of consecutive successful queries required for
	// virtuous blocks.
	BetaVirtuous int

	// BetaRogue is the number of consecutive successful queries required for
	// rogue blocks.
	BetaRogue int

	// ConcurrentRepolls is the number of concurrent re-polls to issue.
	ConcurrentRepolls int

	// OptimalProcessing is the number of blocks that should be processing at once.
	OptimalProcessing int

	// MaxOutstandingItems is the maximum number of items that can be outstanding.
	MaxOutstandingItems int

	// MaxItemProcessingTime is the maximum amount of time an item can be
	// processing for before being benched.
	MaxItemProcessingTime int64
}

// Context provides the configuration for a snowman instance
type Context struct {
	// ChainID is the chain ID of this consensus instance
	ChainID ids.ID

	// Registerer is used to register metrics
	Registerer interface{}

	// Log is used to log messages
	Log interface{}

	// BlockAcceptor is the callback that will be fired whenever a block is
	// accepted.
	BlockAcceptor func(context.Context, ids.ID) error

	// BlockRejector is the callback that will be fired whenever a block is
	// rejected.
	BlockRejector func(context.Context, ids.ID) error
}

// TopologicalFactory creates Topological instances
type TopologicalFactory interface {
	New(ctx *Context) (Consensus, error)
}

// Poll represents the results of a network poll
type Poll interface {
	// Drop any future response for this request
	Drop()

	// Finished returns true if the poll has completed, with no more required
	// responses
	Finished() bool

	// Result returns the result of this poll
	Result() bag.Bag[ids.ID]
}