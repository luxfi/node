// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/mock/gomock"

	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/consensustest"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/engine/chain/block/blockmock"
	"github.com/luxfi/consensus/engine/chain/block/blocktest"
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
}

// Override methods to return block.Block instead of blockmock.Block
func (m *ContextEnabledVMMock) BuildBlock(ctx context.Context) (block.Block, error) {
	if m.BuildBlockF != nil {
		mockBlock, err := m.BuildBlockF(ctx)
		if err != nil {
			return nil, err
		}
		return &blocktest.Block{
			IDV:     mockBlock.ID(),
			HeightV: mockBlock.Height(),
			TimestampV: mockBlock.Timestamp(),
			ParentV: mockBlock.ParentID(),
			BytesV:  mockBlock.Bytes(),
		}, nil
	}
	return nil, nil
}

func (m *ContextEnabledVMMock) ParseBlock(ctx context.Context, bytes []byte) (block.Block, error) {
	if m.ParseBlockF != nil {
		mockBlock, err := m.ParseBlockF(ctx, bytes)
		if err != nil {
			return nil, err
		}
		return &blocktest.Block{
			IDV:     mockBlock.ID(),
			HeightV: mockBlock.Height(),
			TimestampV: mockBlock.Timestamp(),
			ParentV: mockBlock.ParentID(),
			BytesV:  mockBlock.Bytes(),
		}, nil
	}
	return nil, nil
}

func (m *ContextEnabledVMMock) GetBlock(ctx context.Context, id ids.ID) (block.Block, error) {
	if m.GetBlockF != nil {
		mockBlock, err := m.GetBlockF(ctx, id)
		if err != nil {
			return nil, err
		}
		return &blocktest.Block{
			IDV:     mockBlock.ID(),
			HeightV: mockBlock.Height(),
			TimestampV: mockBlock.Timestamp(),
			ParentV: mockBlock.ParentID(),
			BytesV:  mockBlock.Bytes(),
		}, nil
	}
	return nil, nil
}

// Override BuildBlockWithContext
func (m *ContextEnabledVMMock) BuildBlockWithContext(ctx context.Context, blockCtx *block.Context) (block.Block, error) {
	if m.BuildBlockWithContextF != nil {
		// Convert block.Context to blockmock.Context for the mock function
		mockCtx := &blockmock.Context{}
		mockBlock, err := m.BuildBlockWithContextF(ctx, mockCtx)
		if err != nil {
			return nil, err
		}
		return &blocktest.Block{
			IDV:     mockBlock.ID(),
			HeightV: mockBlock.Height(),
			TimestampV: mockBlock.Timestamp(),
			ParentV: mockBlock.ParentID(),
			BytesV:  mockBlock.Bytes(),
		}, nil
	}
	return nil, nil
}

type ContextEnabledBlockMock struct {
	*chainmock.Block
	*blockmock.WithVerifyContext
}

// Implement SetStatus for chain.Block
func (b *ContextEnabledBlockMock) SetStatus(status choices.Status) {
	// For testing, we don't need to actually set status
}

// Override VerifyWithContext to convert context types
func (b *ContextEnabledBlockMock) VerifyWithContext(ctx context.Context, blockCtx *block.Context) error {
	if b.VerifyWithContextF != nil {
		// Convert block.Context to blockmock.Context for the mock function
		mockCtx := &blockmock.Context{}
		return b.VerifyWithContextF(ctx, mockCtx)
	}
	return nil
}

func contextEnabledTestPlugin(t *testing.T, loadExpectations bool) block.ChainVM {
	// test key is "contextTestKey"

	// create mock
	ctxVM := ContextEnabledVMMock{
		ChainVM:                      blockmock.ChainVM{},
		BuildBlockWithContextChainVM: blockmock.BuildBlockWithContextChainVM{},
	}

	if loadExpectations {
		ctxBlock := ContextEnabledBlockMock{
			Block:             chainmock.NewBlock(blkID, parentID, 1),
			WithVerifyContext: blockmock.NewWithVerifyContext(ctrl),
		}
		gomock.InOrder(
			// Initialize
			ctxVM.ChainVM.EXPECT().Initialize(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			).Return(nil).Times(1),
			ctxVM.ChainVM.EXPECT().LastAccepted(gomock.Any()).Return(preSummaryBlk.ID(), nil).Times(1),
			ctxVM.ChainVM.EXPECT().GetBlock(gomock.Any(), gomock.Any()).Return(preSummaryBlk, nil).Times(1),

			// BuildBlockWithContext
			ctxVM.BuildBlockWithContextChainVM.EXPECT().BuildBlockWithContext(gomock.Any(), blockContext).Return(ctxBlock, nil).Times(1),
			ctxBlock.WithVerifyContext.EXPECT().ShouldVerifyWithContext(gomock.Any()).Return(true, nil).Times(1),
			ctxBlock.Block.EXPECT().ID().Return(blkID).Times(1),
			ctxBlock.Block.EXPECT().Parent().Return(parentID).Times(1),
			ctxBlock.Block.EXPECT().Bytes().Return(blkBytes).Times(1),
			ctxBlock.Block.EXPECT().Height().Return(uint64(1)).Times(1),
			ctxBlock.Block.EXPECT().Timestamp().Return(time.Now()).Times(1),

			// VerifyWithContext
			ctxVM.ChainVM.EXPECT().ParseBlock(gomock.Any(), blkBytes).Return(ctxBlock, nil).Times(1),
			ctxBlock.WithVerifyContext.EXPECT().VerifyWithContext(gomock.Any(), blockContext).Return(nil).Times(1),
			ctxBlock.Block.EXPECT().Timestamp().Return(time.Now()).Times(1),
		)
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

	require.NoError(vm.Initialize(context.Background(), ctx, memdb.New(), nil, nil, nil, nil, nil))

	blkIntf, err := vm.BuildBlockWithContext(context.Background(), blockContext)
	require.NoError(err)

	blk, ok := blkIntf.(block.WithVerifyContext)
	require.True(ok)

	shouldVerify, err := blk.ShouldVerifyWithContext(context.Background())
	require.NoError(err)
	require.True(shouldVerify)

	require.NoError(blk.VerifyWithContext(context.Background(), blockContext))
}
