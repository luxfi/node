// Copyright (C) 2025, Lux Partners Limited. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/v2/quasar"
	"github.com/luxfi/node/v2/quasar/choices"
	"github.com/luxfi/node/v2/quasar/engine/core"
	db "github.com/luxfi/database"
)

var (
	// ErrRemoteVMNotImplemented is returned when the remote VM doesn't implement an interface
	ErrRemoteVMNotImplemented = errors.New("remote VM does not implement this interface")
	
	// ErrStateSyncableVMNotImplemented is returned when the VM doesn't implement state sync
	ErrStateSyncableVMNotImplemented = errors.New("state syncable VM not implemented")
)

// Block is a basic block interface
type Block interface {
	choices.Decidable

	// Parent returns the ID of this block's parent
	Parent() ids.ID

	// Height returns the height of this block
	Height() uint64

	// Time returns the time this block was created (as Unix timestamp)
	Time() uint64

	// Verify that this block is well-formed
	Verify(context.Context) error

	// Bytes returns the binary representation of this block
	Bytes() []byte

	// Accept accepts the block (overrides choices.Decidable)
	Accept() error
}

// WithVerifyContext is a block that can be verified with additional context
type WithVerifyContext interface {
	// VerifyWithContext verifies the block with the provided context
	VerifyWithContext(context.Context, *Context) error

	// ShouldVerifyWithContext returns true if the block should be verified with context
	ShouldVerifyWithContext(context.Context) (bool, error)
}

// Context provides additional context for block verification
type Context struct {
	// PChainHeight is the P-Chain height used for block verification
	PChainHeight uint64
}

// BuildBlockWithContextChainVM defines the interface a ChainVM can optionally implement
// to build blocks with additional context
type BuildBlockWithContextChainVM interface {
	// BuildBlockWithContext attempts to build a new block with the provided context
	BuildBlockWithContext(context.Context, *Context) (Block, error)
}

// StateSyncMode defines the mode of state sync
type StateSyncMode uint8

const (
	// StateSyncSkipped indicates state sync was skipped
	StateSyncSkipped StateSyncMode = iota
	// StateSyncStatic indicates static state sync
	StateSyncStatic
	// StateSyncDynamic indicates dynamic state sync
	StateSyncDynamic
)

// StateSummary defines a state summary interface
type StateSummary interface {
	// ID returns the summary ID
	ID() ids.ID

	// Height returns the height of the summary
	Height() uint64

	// Bytes returns the binary representation
	Bytes() []byte

	// Accept accepts the state summary
	Accept(context.Context) (StateSyncMode, error)
}

// ChainVM defines the interface for a blockchain VM
type ChainVM interface {
	// GetBlock retrieves a block by ID
	GetBlock(context.Context, ids.ID) (Block, error)

	// ParseBlock parses a block from bytes
	ParseBlock(context.Context, []byte) (Block, error)

	// BuildBlock builds a new block
	BuildBlock(context.Context) (Block, error)

	// SetPreference sets the preferred block
	SetPreference(context.Context, ids.ID) error

	// LastAccepted returns the last accepted block ID
	LastAccepted(context.Context) (ids.ID, error)

	// Initialize initializes the VM
	Initialize(
		ctx context.Context,
		chainCtx *quasar.Context,
		database db.Database,
		genesisBytes []byte,
		upgradeBytes []byte,
		configBytes []byte,
		fxs []*core.Fx,
		appSender core.AppSender,
	) error

	// SetState sets the VM state
	SetState(context.Context, quasar.State) error

	// Shutdown shuts down the VM
	Shutdown(context.Context) error

	// WaitForEvent waits for the next event
	WaitForEvent(context.Context) (core.Message, error)
}

// HeightIndexedChainVM is a ChainVM that supports retrieving blocks by height
type HeightIndexedChainVM interface {
	ChainVM

	// GetBlockIDAtHeight returns the block ID at the given height
	GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error)
}

// BatchedChainVM defines batch operations for ChainVM
type BatchedChainVM interface {
	ChainVM

	// GetAncestors retrieves ancestor blocks
	GetAncestors(
		ctx context.Context,
		blkID ids.ID,
		maxBlocksNum int,
		maxBlocksSize int,
		maxBlocksRetrieval time.Duration,
	) ([][]byte, error)

	// BatchedParseBlock parses multiple blocks
	BatchedParseBlock(context.Context, [][]byte) ([]Block, error)
}

// StateSyncableVM defines state sync capabilities
type StateSyncableVM interface {
	// StateSyncEnabled returns whether state sync is enabled
	StateSyncEnabled(context.Context) (bool, error)

	// GetOngoingSyncStateSummary returns the ongoing sync state summary
	GetOngoingSyncStateSummary(context.Context) (StateSummary, error)

	// GetLastStateSummary returns the last state summary
	GetLastStateSummary(context.Context) (StateSummary, error)

	// ParseStateSummary parses a state summary from bytes
	ParseStateSummary(context.Context, []byte) (StateSummary, error)

	// GetStateSummary retrieves a state summary by height
	GetStateSummary(context.Context, uint64) (StateSummary, error)
}

// OracleBlock is an oracle block interface
type OracleBlock interface {
	Block

	// Options returns the oracle block options
	Options(context.Context) ([2]Block, error)
}

// BlockWithTimestamp provides a helper interface for blocks that need time.Time
type BlockWithTimestamp interface {
	Block
	
	// Timestamp returns the time this block was created as time.Time
	Timestamp() time.Time
}

// GetTimestamp is a helper function to get time.Time from a Block
func GetTimestamp(b Block) time.Time {
	if bts, ok := b.(BlockWithTimestamp); ok {
		return bts.Timestamp()
	}
	return time.Unix(int64(b.Time()), 0)
}