// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package getter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/consensus/engine/core"
	"github.com/luxfi/node/consensus/engine/enginetest"
	"github.com/luxfi/node/consensus/engine/linear/block/blockmock"
	"github.com/luxfi/node/consensus/engine/linear/block/blocktest"
	"github.com/luxfi/node/consensus/linear"
	"github.com/luxfi/node/consensus/linear/lineartest"
	log "github.com/luxfi/log"
	"github.com/luxfi/node/utils/set"
)

var errUnknownBlock = errors.New("unknown block")

type StateSyncEnabledMock struct {
	*blocktest.VM
	*blockmock.StateSyncableVM
}

func newTest(t *testing.T) (core.AllGetsServer, StateSyncEnabledMock, *enginetest.Sender) {
	ctrl := gomock.NewController(t)

	vm := StateSyncEnabledMock{
		VM:              &blocktest.VM{},
		StateSyncableVM: blockmock.NewStateSyncableVM(ctrl),
	}

	sender := &enginetest.Sender{
		T: t,
	}
	sender.Default(true)

	bs, err := New(
		vm,
		sender,
		log.NewNoOpLogger(),
		time.Second,
		2000,
		prometheus.NewRegistry(),
	)
	require.NoError(t, err)

	return bs, vm, sender
}

func TestAcceptedFrontier(t *testing.T) {
	require := require.New(t)
	bs, vm, sender := newTest(t)

	blkID := ids.GenerateTestID()
	vm.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return blkID, nil
	}

	var accepted ids.ID
	sender.SendAcceptedFrontierF = func(_ context.Context, _ ids.NodeID, _ uint32, containerID ids.ID) {
		accepted = containerID
	}

	require.NoError(bs.GetAcceptedFrontier(context.Background(), ids.EmptyNodeID, 0))
	require.Equal(blkID, accepted)
}

func TestFilterAccepted(t *testing.T) {
	require := require.New(t)
	bs, vm, sender := newTest(t)

	acceptedBlk := lineartest.BuildChild(lineartest.Genesis)
	require.NoError(acceptedBlk.Accept(context.Background()))

	var (
		allBlocks = []*lineartest.Block{
			lineartest.Genesis,
			acceptedBlk,
		}
		unknownBlkID = ids.GenerateTestID()
	)

	vm.LastAcceptedF = lineartest.MakeLastAcceptedBlockF(allBlocks)
	vm.GetBlockIDAtHeightF = lineartest.MakeGetBlockIDAtHeightF(allBlocks)
	vm.GetBlockF = func(_ context.Context, blkID ids.ID) (linear.Block, error) {
		for _, blk := range allBlocks {
			if blk.ID() == blkID {
				return blk, nil
			}
		}

		require.Equal(unknownBlkID, blkID)
		return nil, errUnknownBlock
	}

	var accepted []ids.ID
	sender.SendAcceptedF = func(_ context.Context, _ ids.NodeID, _ uint32, frontier []ids.ID) {
		accepted = frontier
	}

	blkIDs := set.Of(lineartest.GenesisID, acceptedBlk.ID(), unknownBlkID)
	require.NoError(bs.GetAccepted(context.Background(), ids.EmptyNodeID, 0, blkIDs))

	require.Len(accepted, 2)
	require.Contains(accepted, lineartest.GenesisID)
	require.Contains(accepted, acceptedBlk.ID())
	require.NotContains(accepted, unknownBlkID)
}
