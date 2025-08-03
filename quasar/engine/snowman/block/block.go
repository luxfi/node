// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"time"

	"github.com/luxfi/node/consensus/choices"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/snow/engine/common"
)

// Block is the block interface
type Block interface {
	choices.Decidable

	Parent() ids.ID
	Height() uint64
	Timestamp() time.Time
	Verify(context.Context) error
	Bytes() []byte
}

// ChainVM defines the functionality required to build a blockchain
type ChainVM interface {
	common.Engine

	// Initialize the VM with the given context
	Initialize(
		ctx context.Context,
		snowCtx *snow.Context,
		db interface{},
		genesisBytes []byte,
		upgradeBytes []byte,
		configBytes []byte,
		toEngine chan<- common.AppMessage,
		fxs []*common.Fx,
		appSender common.AppSender,
	) error

	// Shutdown the VM
	Shutdown(context.Context) error

	// CreateHandlers returns a map of extensions to VM-specific handlers
	CreateHandlers(context.Context) (map[string]interface{}, error)

	// CreateStaticHandlers returns a map of extensions to VM-specific static handlers
	CreateStaticHandlers(context.Context) (map[string]interface{}, error)

	// VerifyHeightIndex verifies that the provided block heights are valid
	VerifyHeightIndex(context.Context) error

	// GetBlockIDAtHeight returns the block ID at the given height
	GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error)

	// SetState sets the current state of the VM
	SetState(ctx context.Context, state snow.State) error

	// Version returns the version of the VM
	Version(context.Context) (string, error)

	// Block functionality
	BuildBlock(context.Context) (Block, error)
	ParseBlock(context.Context, []byte) (Block, error)
	GetBlock(context.Context, ids.ID) (Block, error)
	SetPreference(context.Context, ids.ID) error
	LastAccepted(context.Context) (ids.ID, error)
}

// HeightIndexedChainVM extends ChainVM to support height-based indexing
type HeightIndexedChainVM interface {
	ChainVM

	// VerifyHeightIndex should return:
	// - nil if the height index is fully verified
	// - ErrIndexIncomplete if the height index is not fully verified
	// - Any other error if the height index is invalid
	VerifyHeightIndex(context.Context) error

	// GetBlockIDAtHeight returns the ID of the block that was accepted with
	// [height]. Note that all accepted blocks should be accessible by their
	// height.
	GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error)
}

// Getter defines the functionality for fetching a block by its ID
type Getter interface {
	// GetBlock attempts to load a block by its ID. If the block does not exist,
	// an error should be returned.
	GetBlock(context.Context, ids.ID) (Block, error)
}

// Parser defines the functionality for parsing a block from bytes
type Parser interface {
	// ParseBlock parses the provided bytes into a block.
	ParseBlock(context.Context, []byte) (Block, error)
}

// Fx represents an extension feature
type Fx struct{}

// AppMessage represents an application message
type AppMessage interface{}

// AppSender sends application messages
type AppSender interface{}

// State represents the state of the chain
type State uint8

const (
	// Bootstrapping state
	Bootstrapping State = iota
	// NormalOp state
	NormalOp
)