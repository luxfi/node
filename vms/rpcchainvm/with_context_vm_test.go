// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/consensustest"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/engine/chain/block/blockmock"
	"github.com/luxfi/consensus/engine/chain/chainmock"
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
	blockmock.BuildBlockWithContextChainVM
	BuildBlockWithContextFunc func(context.Context, *block.Context) (block.Block, error)
}

// Override methods to return block.Block instead of blockmock.Block
func (m *ContextEnabledVMMock) BuildBlock(ctx context.Context) (block.Block, error) {
	if m.BuildBlockF != nil {
		return m.BuildBlockF(ctx)
	}
	return nil, nil
}

func (m *ContextEnabledVMMock) ParseBlock(ctx context.Context, bytes []byte) (block.Block, error) {
	if m.ParseBlockF != nil {
		return m.ParseBlockF(ctx, bytes)
	}
	return nil, nil
}

func (m *ContextEnabledVMMock) GetBlock(ctx context.Context, id ids.ID) (block.Block, error) {
	if m.GetBlockF != nil {
		return m.GetBlockF(ctx, id)
	}
	return nil, nil
}

// Override BuildBlockWithContext
func (m *ContextEnabledVMMock) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (block.Block, error) {
	if m.BuildBlockWithContextFunc != nil {
		return m.BuildBlockWithContextFunc(ctx, blockCtx)
	}
	return nil, nil
}

type ContextEnabledBlockMock struct {
	*chainmock.Block
	*blockmock.WithVerifyContext
	VerifyWithContextFunc func(context.Context, *block.Context) error
}

// Implement SetStatus for chain.Block
func (b *ContextEnabledBlockMock) SetStatus(status choices.Status) {
	// For testing, we don't need to actually set status
}

// Override VerifyWithContext to convert context types
func (b *ContextEnabledBlockMock) VerifyWithContext(ctx context.Context, blockCtx *block.Context) error {
	if b.VerifyWithContextFunc != nil {
		return b.VerifyWithContextFunc(ctx, blockCtx)
	}
	return nil
}

func contextEnabledTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "contextTestKey"

	// create mock
	ctxVM := &ContextEnabledVMMock{
		ChainVM:                      blockmock.ChainVM{},
		BuildBlockWithContextChainVM: blockmock.BuildBlockWithContextChainVM{},
	}

	if loadExpectations {
		ctxBlock := &ContextEnabledBlockMock{
			Block:             chainmock.NewBlock(blkID, parentID, 1),
			WithVerifyContext: &blockmock.WithVerifyContext{},
		}

		// Setup mock functions
		ctxVM.InitializeF = func(ctx context.Context, chainCtx interface{}, db interface{}, genesisBytes []byte, upgradeBytes []byte, configBytes []byte, msgChan interface{}, fxs []interface{}, appSender interface{}) error {
			return nil
		}

		ctxVM.LastAcceptedF = func(ctx context.Context) (ids.ID, error) {
			return preSummaryBlk.ID(), nil
		}

		ctxVM.GetBlockF = func(ctx context.Context, id ids.ID) (block.Block, error) {
			return preSummaryBlk, nil
		}

		ctxVM.BuildBlockWithContextFunc = func(ctx context.Context, blockCtx *block.Context) (block.Block, error) {
			return ctxBlock, nil
		}

		ctxVM.ParseBlockF = func(ctx context.Context, bytes []byte) (block.Block, error) {
			return ctxBlock, nil
		}

		ctxBlock.VerifyWithContextFunc = func(ctx context.Context, blockCtx *block.Context) error {
			return nil
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

	require.NoError(vm.Initialize(context.Background(), ctx, memdb.New(), nil, nil, nil, nil, nil, nil))

	blkIntf, err := vm.BuildBlockWithContext(context.Background(), blockContext)
	require.NoError(err)

	blk, ok := blkIntf.(block.WithVerifyContext)
	require.True(ok)

	shouldVerify, err := blk.ShouldVerifyWithContext(context.Background())
	require.NoError(err)
	require.True(shouldVerify)

	require.NoError(blk.VerifyWithContext(context.Background(), blockContext))
}
