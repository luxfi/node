// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocktest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/consensus/engine/linear/block"
	"github.com/luxfi/node/consensus/linear"
	"github.com/luxfi/node/ids"
)

var (
	errGetAncestor       = errors.New("unexpectedly called GetAncestor")
	errBatchedParseBlock = errors.New("unexpectedly called BatchedParseBlock")

	_ block.BatchedChainVM = (*BatchedVM)(nil)
)

// BatchedVM is a BatchedVM that is useful for testing.
type BatchedVM struct {
	T *testing.T

	CantGetAncestors    bool
	CantBatchParseBlock bool

	GetAncestorsF func(
		ctx context.Context,
		blkID ids.ID,
		maxBlocksNum int,
		maxBlocksSize int,
		maxBlocksRetrivalTime time.Duration,
	) ([][]byte, error)

	BatchedParseBlockF func(
		ctx context.Context,
		blks [][]byte,
	) ([]linear.Block, error)
}

func (vm *BatchedVM) Default(cant bool) {
	vm.CantGetAncestors = cant
	vm.CantBatchParseBlock = cant
}

func (vm *BatchedVM) GetAncestors(
	ctx context.Context,
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrivalTime time.Duration,
) ([][]byte, error) {
	if vm.GetAncestorsF != nil {
		return vm.GetAncestorsF(
			ctx,
			blkID,
			maxBlocksNum,
			maxBlocksSize,
			maxBlocksRetrivalTime,
		)
	}
	if vm.CantGetAncestors && vm.T != nil {
		require.FailNow(vm.T, errGetAncestor.Error())
	}
	return nil, errGetAncestor
}

func (vm *BatchedVM) BatchedParseBlock(
	ctx context.Context,
	blks [][]byte,
) ([]linear.Block, error) {
	if vm.BatchedParseBlockF != nil {
		return vm.BatchedParseBlockF(ctx, blks)
	}
	if vm.CantBatchParseBlock && vm.T != nil {
		require.FailNow(vm.T, errBatchedParseBlock.Error())
	}
	return nil, errBatchedParseBlock
}
