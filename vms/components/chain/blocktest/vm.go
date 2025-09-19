// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocktest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/luxfi/consensus/engine/chain/block"
	chain "github.com/luxfi/consensus/protocol/chain"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/metric"
)

// VM is a test VM that can be used for testing
type VM struct {
	T *testing.T

	InitializeF            func(context.Context, context.Context, database.Database, []byte, []byte, []byte, interface{}, []ids.ID, metric.Registry) error
	BuildBlockF            func(context.Context) (chain.Block, error)
	ParseBlockF            func(context.Context, []byte) (chain.Block, error)
	GetBlockF              func(context.Context, ids.ID) (chain.Block, error)
	LastAcceptedF          func(context.Context) (ids.ID, error)
	SetPreferenceF         func(context.Context, ids.ID) error
	SetStateF              func(context.Context, uint8) error
	VerifyHeightIndexF     func(context.Context) error
	GetBlockIDAtHeightF    func(context.Context, uint64) (ids.ID, error)
	GetStatelessBlockF     func(context.Context, ids.ID) (block.Block, error)
}

// BatchedVM is a test VM that supports batch operations
type BatchedVM struct {
	T *testing.T

	GetAncestorsF                      func(context.Context, ids.ID, int, time.Duration) ([][]byte, error)
	BatchedParseBlockF                 func(context.Context, [][]byte) ([]block.Block, error)
	GetBlockIDAtHeightF                func(context.Context, uint64) (ids.ID, error)
}

// StateSyncableVM is a test VM that supports state sync
type StateSyncableVM struct {
	T *testing.T

	StateSyncEnabledF           func(context.Context) (bool, error)
	GetOngoingSyncStateSummaryF func(context.Context) (block.StateSummary, error)
	GetLastStateSummaryF        func(context.Context) (block.StateSummary, error)
	ParseStateSummaryF          func(context.Context, []byte) (block.StateSummary, error)
	GetStateSummaryF            func(context.Context, uint64) (block.StateSummary, error)
}

// Standard method implementations - these can be overridden by setting the F fields

func (vm *VM) Initialize(ctx context.Context, chainCtx context.Context, db database.Database, genesisBytes []byte, upgradeBytes []byte, configBytes []byte, msgSender interface{}, validators []ids.ID, registry metric.Registry) error {
	if vm.InitializeF != nil {
		return vm.InitializeF(ctx, chainCtx, db, genesisBytes, upgradeBytes, configBytes, msgSender, validators, registry)
	}
	return nil
}

func (vm *VM) BuildBlock(ctx context.Context) (chain.Block, error) {
	if vm.BuildBlockF != nil {
		return vm.BuildBlockF(ctx)
	}
	return nil, errors.New("not implemented")
}

func (vm *VM) ParseBlock(ctx context.Context, blockBytes []byte) (chain.Block, error) {
	if vm.ParseBlockF != nil {
		return vm.ParseBlockF(ctx, blockBytes)
	}
	return nil, errors.New("not implemented")
}

func (vm *VM) GetBlock(ctx context.Context, blkID ids.ID) (chain.Block, error) {
	if vm.GetBlockF != nil {
		return vm.GetBlockF(ctx, blkID)
	}
	return nil, errors.New("not implemented")
}

func (vm *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	if vm.LastAcceptedF != nil {
		return vm.LastAcceptedF(ctx)
	}
	return ids.Empty, nil
}

func (vm *VM) SetPreference(ctx context.Context, blkID ids.ID) error {
	if vm.SetPreferenceF != nil {
		return vm.SetPreferenceF(ctx, blkID)
	}
	return nil
}

func (vm *VM) SetState(ctx context.Context, state uint8) error {
	if vm.SetStateF != nil {
		return vm.SetStateF(ctx, state)
	}
	return nil
}

func (vm *VM) VerifyHeightIndex(ctx context.Context) error {
	if vm.VerifyHeightIndexF != nil {
		return vm.VerifyHeightIndexF(ctx)
	}
	return nil
}

func (vm *VM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	if vm.GetBlockIDAtHeightF != nil {
		return vm.GetBlockIDAtHeightF(ctx, height)
	}
	return ids.Empty, database.ErrNotFound
}

func (vm *VM) GetStatelessBlock(ctx context.Context, blkID ids.ID) (block.Block, error) {
	if vm.GetStatelessBlockF != nil {
		return vm.GetStatelessBlockF(ctx, blkID)
	}
	return nil, errors.New("not implemented")
}

// BatchedVM methods

func (vm *BatchedVM) GetAncestors(ctx context.Context, blkID ids.ID, maxBlocksNum int, timeout time.Duration) ([][]byte, error) {
	if vm.GetAncestorsF != nil {
		return vm.GetAncestorsF(ctx, blkID, maxBlocksNum, timeout)
	}
	return nil, errors.New("not implemented")
}

func (vm *BatchedVM) BatchedParseBlock(ctx context.Context, blks [][]byte) ([]block.Block, error) {
	if vm.BatchedParseBlockF != nil {
		return vm.BatchedParseBlockF(ctx, blks)
	}
	return nil, errors.New("not implemented")
}

func (vm *BatchedVM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	if vm.GetBlockIDAtHeightF != nil {
		return vm.GetBlockIDAtHeightF(ctx, height)
	}
	return ids.Empty, database.ErrNotFound
}

// StateSyncableVM methods

func (vm *StateSyncableVM) StateSyncEnabled(ctx context.Context) (bool, error) {
	if vm.StateSyncEnabledF != nil {
		return vm.StateSyncEnabledF(ctx)
	}
	return false, nil
}

func (vm *StateSyncableVM) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	if vm.GetOngoingSyncStateSummaryF != nil {
		return vm.GetOngoingSyncStateSummaryF(ctx)
	}
	return nil, database.ErrNotFound
}

func (vm *StateSyncableVM) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	if vm.GetLastStateSummaryF != nil {
		return vm.GetLastStateSummaryF(ctx)
	}
	return nil, database.ErrNotFound
}

func (vm *StateSyncableVM) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	if vm.ParseStateSummaryF != nil {
		return vm.ParseStateSummaryF(ctx, summaryBytes)
	}
	return nil, errors.New("not implemented")
}

func (vm *StateSyncableVM) GetStateSummary(ctx context.Context, height uint64) (block.StateSummary, error) {
	if vm.GetStateSummaryF != nil {
		return vm.GetStateSummaryF(ctx, height)
	}
	return nil, database.ErrNotFound
}