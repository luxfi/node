// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metervm

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/luxfi/node/database"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/linear"
	"github.com/luxfi/node/consensus/engine"
	"github.com/luxfi/node/consensus/engine/chain/block"
	"github.com/luxfi/node/utils/timer/mockable"
)

var (
	_ block.ChainVM                      = (*blockVM)(nil)
	_ block.BuildBlockWithContextChainVM = (*blockVM)(nil)
	_ block.BatchedChainVM               = (*blockVM)(nil)
	_ block.StateSyncableVM              = (*blockVM)(nil)
)

type blockVM struct {
	block.ChainVM
	buildBlockVM block.BuildBlockWithContextChainVM
	batchedVM    block.BatchedChainVM
	ssVM         block.StateSyncableVM

	blockMetrics
	registry prometheus.Registerer
	clock    mockable.Clock
}

func NewBlockVM(
	vm block.ChainVM,
	reg prometheus.Registerer,
) block.ChainVM {
	buildBlockVM, _ := vm.(block.BuildBlockWithContextChainVM)
	batchedVM, _ := vm.(block.BatchedChainVM)
	ssVM, _ := vm.(block.StateSyncableVM)
	return &blockVM{
		ChainVM:      vm,
		buildBlockVM: buildBlockVM,
		batchedVM:    batchedVM,
		ssVM:         ssVM,
		registry:     reg,
	}
}

func (vm *blockVM) Initialize(
	ctx context.Context,
	chainCtx *consensus.Context,
	db database.Database,
	genesisBytes,
	upgradeBytes,
	configBytes []byte,
	fxs []*engine.Fx,
	appSender engine.AppSender,
) error {
	err := vm.blockMetrics.Initialize(
		vm.buildBlockVM != nil,
		vm.batchedVM != nil,
		vm.ssVM != nil,
		vm.registry,
	)
	if err != nil {
		return err
	}

	return vm.ChainVM.Initialize(ctx, chainCtx, db, genesisBytes, upgradeBytes, configBytes, fxs, appSender)
}

func (vm *blockVM) BuildBlock(ctx context.Context) (chain.Block, error) {
	start := vm.clock.Time()
	blk, err := vm.ChainVM.BuildBlock(ctx)
	end := vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		vm.blockMetrics.buildBlockErr.Observe(duration)
		return nil, err
	}
	vm.blockMetrics.buildBlock.Observe(duration)
	return &meterBlock{
		Block: blk,
		vm:    vm,
	}, nil
}

func (vm *blockVM) ParseBlock(ctx context.Context, b []byte) (chain.Block, error) {
	start := vm.clock.Time()
	blk, err := vm.ChainVM.ParseBlock(ctx, b)
	end := vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		vm.blockMetrics.parseBlockErr.Observe(duration)
		return nil, err
	}
	vm.blockMetrics.parseBlock.Observe(duration)
	return &meterBlock{
		Block: blk,
		vm:    vm,
	}, nil
}

func (vm *blockVM) GetBlock(ctx context.Context, id ids.ID) (chain.Block, error) {
	start := vm.clock.Time()
	blk, err := vm.ChainVM.GetBlock(ctx, id)
	end := vm.clock.Time()
	duration := float64(end.Sub(start))
	if err != nil {
		vm.blockMetrics.getBlockErr.Observe(duration)
		return nil, err
	}
	vm.blockMetrics.getBlock.Observe(duration)
	return &meterBlock{
		Block: blk,
		vm:    vm,
	}, nil
}

func (vm *blockVM) SetPreference(ctx context.Context, id ids.ID) error {
	start := vm.clock.Time()
	err := vm.ChainVM.SetPreference(ctx, id)
	end := vm.clock.Time()
	vm.blockMetrics.setPreference.Observe(float64(end.Sub(start)))
	return err
}

func (vm *blockVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	start := vm.clock.Time()
	lastAcceptedID, err := vm.ChainVM.LastAccepted(ctx)
	end := vm.clock.Time()
	vm.blockMetrics.lastAccepted.Observe(float64(end.Sub(start)))
	return lastAcceptedID, err
}

func (vm *blockVM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	start := vm.clock.Time()
	blockID, err := vm.ChainVM.GetBlockIDAtHeight(ctx, height)
	end := vm.clock.Time()
	vm.blockMetrics.getBlockIDAtHeight.Observe(float64(end.Sub(start)))
	return blockID, err
}
