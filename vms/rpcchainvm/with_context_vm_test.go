// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/consensustest"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/engine/chain/block/blockmock"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/chain"
)

var (
	_ block.ChainVM                      = &ContextEnabledVMMock{}
	_ block.BuildBlockWithContextChainVM = &ContextEnabledVMMock{}

	_ chain.Block             = &ContextEnabledBlockMock{}
	_ block.WithVerifyContext = &ContextEnabledBlockMock{}

	blockContext = &block.Context{
		PChainHeight: 1,
	}

	blkID    = ids.ID{1}
	parentID = ids.ID{0}
	blkBytes = []byte{0}
)

type ContextEnabledVMMock struct {
	blockmock.ChainVM

	// Override BuildBlockWithContext to return the correct type
	BuildBlockWithContextF func(context.Context, *block.Context) (block.Block, error)
}

// BuildBlockWithContext implements block.BuildBlockWithContextChainVM
func (vm *ContextEnabledVMMock) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (block.Block, error) {
	if vm.BuildBlockWithContextF != nil {
		return vm.BuildBlockWithContextF(ctx, blockCtx)
	}
	return nil, nil
}

type ContextEnabledBlockMock struct {
	idV        ids.ID
	parentV    ids.ID
	heightV    uint64
	timestampV time.Time
	statusV    choices.Status
	bytesV     []byte

	verifyWithContextF func(context.Context, *block.Context) error
	shouldVerifyWithContextF func(context.Context) (bool, error)
	verifyF func(context.Context) error
	acceptF func(context.Context) error
	rejectF func(context.Context) error
}

// ID returns the block ID
func (b *ContextEnabledBlockMock) ID() ids.ID { return b.idV }

// Parent returns the parent ID
func (b *ContextEnabledBlockMock) Parent() ids.ID { return b.parentV }

// ParentID returns the parent ID
func (b *ContextEnabledBlockMock) ParentID() ids.ID { return b.parentV }

// Height returns the block height
func (b *ContextEnabledBlockMock) Height() uint64 { return b.heightV }

// Timestamp returns the block timestamp
func (b *ContextEnabledBlockMock) Timestamp() time.Time { return b.timestampV }

// Status returns the block status
func (b *ContextEnabledBlockMock) Status() uint8 { return uint8(b.statusV) }

// Bytes returns the block bytes
func (b *ContextEnabledBlockMock) Bytes() []byte { return b.bytesV }

// Verify verifies the block
func (b *ContextEnabledBlockMock) Verify(ctx context.Context) error {
	if b.verifyF != nil {
		return b.verifyF(ctx)
	}
	return nil
}

// Accept accepts the block
func (b *ContextEnabledBlockMock) Accept(ctx context.Context) error {
	if b.acceptF != nil {
		return b.acceptF(ctx)
	}
	b.statusV = choices.Accepted
	return nil
}

// Reject rejects the block
func (b *ContextEnabledBlockMock) Reject(ctx context.Context) error {
	if b.rejectF != nil {
		return b.rejectF(ctx)
	}
	b.statusV = choices.Rejected
	return nil
}

// SetStatus implements chain.Block interface
func (b *ContextEnabledBlockMock) SetStatus(status choices.Status) {
	b.statusV = status
}

// VerifyWithContext implements WithVerifyContext interface
func (b *ContextEnabledBlockMock) VerifyWithContext(ctx context.Context, blockCtx *block.Context) error {
	if b.verifyWithContextF != nil {
		return b.verifyWithContextF(ctx, blockCtx)
	}
	return nil
}

// ShouldVerifyWithContext implements WithVerifyContext interface
func (b *ContextEnabledBlockMock) ShouldVerifyWithContext(ctx context.Context) (bool, error) {
	if b.shouldVerifyWithContextF != nil {
		return b.shouldVerifyWithContextF(ctx)
	}
	return false, nil
}

func contextEnabledTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "contextTestKey"

	// create mock
	chainVM := blockmock.NewChainVM()
	ctxVM := &ContextEnabledVMMock{
		ChainVM: *chainVM,
	}

	if loadExpectations {
		ctxBlock := &ContextEnabledBlockMock{
			idV:        blkID,
			parentV:    parentID,
			heightV:    1,
			timestampV: time.Now(),
			statusV:    choices.Processing,
			bytesV:     blkBytes,
			shouldVerifyWithContextF: func(context.Context) (bool, error) {
				return true, nil
			},
			verifyWithContextF: func(context.Context, *block.Context) error {
				return nil
			},
		}

		// Set the BuildBlockWithContext function
		ctxVM.BuildBlockWithContextF = func(ctx context.Context, blockCtx *block.Context) (block.Block, error) {
			return ctxBlock, nil
		}

		// Set up chainVM expectations
		chainVM.InitializeF = func(
			ctx context.Context,
			chainCtx interface{},
			db interface{},
			genesisBytes []byte,
			upgradeBytes []byte,
			configBytes []byte,
			msgChan interface{},
			fxs []interface{},
			appSender interface{},
		) error {
			return nil
		}

		chainVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
			return preSummaryBlk.ID(), nil
		}

		chainVM.GetBlockF = func(context.Context, ids.ID) (block.Block, error) {
			return preSummaryBlk, nil
		}

		chainVM.ParseBlockF = func(context.Context, []byte) (block.Block, error) {
			return ctxBlock, nil
		}
	}

	return ctxVM
}

func TestContextVMSummary(t *testing.T) {
	require := require.New(t)
	testKey := contextTestKey

	// Create and start the plugin
	vm := buildClientHelper(require, testKey)
	defer vm.runtime.Stop(context.Background())

	ctx := consensustest.Context(t, consensustest.CChainID)

	require.NoError(vm.Initialize(context.Background(), ctx, memdb.New(), nil, nil, nil, nil, []interface{}{}, nil))

	blkIntf, err := vm.BuildBlockWithContext(context.Background(), blockContext)
	require.NoError(err)

	blk, ok := blkIntf.(block.WithVerifyContext)
	require.True(ok)

	shouldVerify, err := blk.ShouldVerifyWithContext(context.Background())
	require.NoError(err)
	require.True(shouldVerify)

	require.NoError(blk.VerifyWithContext(context.Background(), blockContext))
}
