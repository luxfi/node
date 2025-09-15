// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"context"
	"errors"
	"time"

	consensusblock "github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/ids"
)

var (
	ErrRemoteVMNotImplemented = errors.New("remote VM not implemented")
	ErrStateSyncableVMNotImplemented = errors.New("state syncable VM not implemented")
)

var (
	_ consensusblock.ChainVM         = (*ChangeNotifier)(nil)
	_ consensusblock.BatchedChainVM  = (*ChangeNotifier)(nil)
	_ consensusblock.StateSyncableVM = (*ChangeNotifier)(nil)
)

type FullVM interface {
	consensusblock.StateSyncableVM
	consensusblock.BatchedChainVM
	consensusblock.ChainVM
}

type ChangeNotifier struct {
	consensusblock.ChainVM

	// OnChange is used to signal the NotificationForwarder to stop its current subscription and re-subscribe.
	// This is needed in case a block has been accepted that changes when a VM considers the need to build a block.
	// In order for the subscription to be correlated to the latest data, it needs to be retried.
	OnChange func()
	// lastPref is the last block ID that was set as preferred via SetPreference.
	lastPref ids.ID
	invoked  bool
}

func (cn *ChangeNotifier) GetAncestors(ctx context.Context, blkID ids.ID, maxBlocksNum int, maxBlocksSize int, maxBlocksRetrivalTime time.Duration) ([][]byte, error) {
	if batchedVM, ok := cn.ChainVM.(consensusblock.BatchedChainVM); ok {
		return batchedVM.GetAncestors(ctx, blkID, maxBlocksNum, maxBlocksSize, maxBlocksRetrivalTime)
	}
	return nil, ErrRemoteVMNotImplemented
}

func (cn *ChangeNotifier) BatchedParseBlock(ctx context.Context, blks [][]byte) ([]consensusblock.Block, error) {
	if batchedVM, ok := cn.ChainVM.(consensusblock.BatchedChainVM); ok {
		return batchedVM.BatchedParseBlock(ctx, blks)
	}
	return nil, ErrRemoteVMNotImplemented
}

func (cn *ChangeNotifier) StateSyncEnabled(ctx context.Context) (bool, error) {
	if ssVM, isSSVM := cn.ChainVM.(consensusblock.StateSyncableVM); isSSVM {
		return ssVM.StateSyncEnabled(ctx)
	}
	return false, nil
}

func (cn *ChangeNotifier) GetOngoingSyncStateSummary(ctx context.Context) (consensusblock.StateSummary, error) {
	if ssVM, isSSVM := cn.ChainVM.(consensusblock.StateSyncableVM); isSSVM {
		return ssVM.GetOngoingSyncStateSummary(ctx)
	}
	return nil, ErrStateSyncableVMNotImplemented
}

func (cn *ChangeNotifier) GetLastStateSummary(ctx context.Context) (consensusblock.StateSummary, error) {
	if ssVM, isSSVM := cn.ChainVM.(consensusblock.StateSyncableVM); isSSVM {
		return ssVM.GetLastStateSummary(ctx)
	}
	return nil, ErrStateSyncableVMNotImplemented
}

func (cn *ChangeNotifier) ParseStateSummary(ctx context.Context, summaryBytes []byte) (consensusblock.StateSummary, error) {
	if ssVM, isSSVM := cn.ChainVM.(consensusblock.StateSyncableVM); isSSVM {
		return ssVM.ParseStateSummary(ctx, summaryBytes)
	}
	return nil, ErrStateSyncableVMNotImplemented
}

func (cn *ChangeNotifier) GetStateSummary(ctx context.Context, summaryHeight uint64) (consensusblock.StateSummary, error) {
	if ssVM, isSSVM := cn.ChainVM.(consensusblock.StateSyncableVM); isSSVM {
		return ssVM.GetStateSummary(ctx, summaryHeight)
	}
	return nil, ErrStateSyncableVMNotImplemented
}

func (cn *ChangeNotifier) SetPreference(ctx context.Context, blkID ids.ID) error {
	// Only call OnChange if the preference has changed.
	if !cn.invoked || cn.lastPref != blkID {
		cn.lastPref = blkID
		cn.invoked = true
		defer cn.OnChange()
	}

	return cn.ChainVM.SetPreference(ctx, blkID)
}

func (cn *ChangeNotifier) BuildBlock(ctx context.Context) (consensusblock.Block, error) {
	defer cn.OnChange()
	return cn.ChainVM.BuildBlock(ctx)
}