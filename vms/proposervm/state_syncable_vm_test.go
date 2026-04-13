// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"testing"

	"github.com/luxfi/metric"
	statelessblock "github.com/luxfi/node/vms/proposervm/block"

	"github.com/stretchr/testify/require"

	consensustest "github.com/luxfi/consensus/test/helpers"
	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/vms/proposervm/summary"
	validators "github.com/luxfi/validators"
	validatorstest "github.com/luxfi/validators/validatorstest"
	"github.com/luxfi/vm"
	chain "github.com/luxfi/vm/chain"
	consensusblock "github.com/luxfi/vm/chain"
	"github.com/luxfi/vm/chain/blocktest"
)

func helperBuildStateSyncTestObjects(t *testing.T) (*fullVM, *VM) {
	require := require.New(t)

	innerVM := &fullVM{
		ChainVM: &blocktest.ChainVM{
			T: t,
		},
		StateSyncableVM: &blocktest.StateSyncableVM{
			T: t,
		},
	}

	// load innerVM expectations
	innerVM.InitializeF = func(_ context.Context, _ vm.Init) error {
		return nil
	}
	innerVM.LastAcceptedF = blocktest.MakeLastAcceptedBlockF(
		[]*blocktest.Block{blocktest.Genesis},
	)
	innerVM.GetBlockF = func(_ context.Context, blkID ids.ID) (consensusblock.Block, error) {
		if blkID != blocktest.Genesis.ID() {
			return nil, database.ErrNotFound
		}
		return blocktest.Genesis, nil
	}

	// create the VM
	vmImpl := New(
		innerVM,
		Config{
			Upgrades:            upgradetest.GetConfig(upgradetest.Latest),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          metric.NewNoOp().Registry(),
		},
	)

	rt := consensustest.Runtime(t, consensustest.CChainID)
	rt.NodeID = ids.NodeIDFromCert(&ids.Certificate{
		Raw:       pTestCert.Raw,
		PublicKey: pTestCert.PublicKey,
	})

	valState := validatorstest.NewTestState()
	valState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return defaultPChainHeight, nil
	}
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{
			rt.NodeID: {
				NodeID: rt.NodeID,
				Light:  10,
				Weight: 10,
			},
		}, nil
	}

	rt.ValidatorState = valState
	require.NoError(vmImpl.Initialize(
		context.Background(),
		vm.Init{
			Runtime: rt,
			DB:      prefixdb.New([]byte{}, memdb.New()),
			Genesis: blocktest.GenesisBytes,
			Log:     log.NoLog{},
		},
	))
	require.NoError(vmImpl.SetState(context.Background(), uint32(vm.Syncing)))

	return innerVM, vmImpl
}

func TestStateSyncEnabled(t *testing.T) {
	require := require.New(t)

	innerVM, vmImpl := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vmImpl.Shutdown(context.Background()))
	}()

	// ProposerVM State Sync disabled if innerVM State sync is disabled
	innerVM.StateSyncEnabledF = func(context.Context) (bool, error) {
		return false, nil
	}
	enabled, err := vmImpl.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.False(enabled)

	// ProposerVM State Sync enabled if innerVM State sync is enabled
	innerVM.StateSyncEnabledF = func(context.Context) (bool, error) {
		return true, nil
	}
	enabled, err = vmImpl.StateSyncEnabled(context.Background())
	require.NoError(err)
	require.True(enabled)
}

func TestStateSyncGetOngoingSyncStateSummary(t *testing.T) {
	require := require.New(t)

	innerVM, vmImpl := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vmImpl.Shutdown(context.Background()))
	}()

	innerSummary := &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: uint64(2022),
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	// No ongoing state summary case
	innerVM.GetOngoingSyncStateSummaryF = func(context.Context) (consensusblock.StateSummary, error) {
		return nil, database.ErrNotFound
	}
	summary, err := vmImpl.GetOngoingSyncStateSummary(context.Background())
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(summary)

	// Pre fork summary case, fork height not reached hence not set yet
	innerVM.GetOngoingSyncStateSummaryF = func(context.Context) (consensusblock.StateSummary, error) {
		return innerSummary, nil
	}
	_, err = vmImpl.GetForkHeight()
	require.ErrorIs(err, database.ErrNotFound)
	summary, err = vmImpl.GetOngoingSyncStateSummary(context.Background())
	require.NoError(err)
	require.Equal(innerSummary.ID(), summary.ID())
	require.Equal(innerSummary.Height(), summary.Height())
	require.Equal(innerSummary.Bytes(), summary.Bytes())

	// Pre fork summary case, fork height already reached
	innerVM.GetOngoingSyncStateSummaryF = func(context.Context) (consensusblock.StateSummary, error) {
		return innerSummary, nil
	}
	require.NoError(vmImpl.SetForkHeight(innerSummary.Height() + 1))
	summary, err = vmImpl.GetOngoingSyncStateSummary(context.Background())
	require.NoError(err)
	require.Equal(innerSummary.ID(), summary.ID())
	require.Equal(innerSummary.Height(), summary.Height())
	require.Equal(innerSummary.Bytes(), summary.Bytes())

	// Post fork summary case
	require.NoError(vmImpl.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &blocktest.Block{
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: innerSummary.Height(),
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (consensusblock.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	slb, err := statelessblock.Build(
		vmImpl.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{PChainHeight: 100, Number: 0, StartTime: 0},
		vmImpl.StakingCertLeaf,
		innerBlk.Bytes(),
		vmImpl.rt.ChainID,
		vmImpl.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vmImpl,
			innerBlk: innerBlk,
		},
	}
	require.NoError(vmImpl.acceptPostForkBlock(proBlk))

	summary, err = vmImpl.GetOngoingSyncStateSummary(context.Background())
	require.NoError(err)
	require.Equal(innerSummary.Height(), summary.Height())
}

func TestStateSyncGetLastStateSummary(t *testing.T) {
	require := require.New(t)

	innerVM, vmImpl := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vmImpl.Shutdown(context.Background()))
	}()

	innerSummary := &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: uint64(2022),
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	// No last state summary case
	innerVM.GetLastStateSummaryF = func(context.Context) (consensusblock.StateSummary, error) {
		return nil, database.ErrNotFound
	}
	summary, err := vmImpl.GetLastStateSummary(context.Background())
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(summary)

	// Pre fork summary case, fork height not reached hence not set yet
	innerVM.GetLastStateSummaryF = func(context.Context) (consensusblock.StateSummary, error) {
		return innerSummary, nil
	}
	_, err = vmImpl.GetForkHeight()
	require.ErrorIs(err, database.ErrNotFound)
	summary, err = vmImpl.GetLastStateSummary(context.Background())
	require.NoError(err)
	require.Equal(innerSummary.ID(), summary.ID())
	require.Equal(innerSummary.Height(), summary.Height())
	require.Equal(innerSummary.Bytes(), summary.Bytes())

	// Pre fork summary case, fork height already reached
	innerVM.GetLastStateSummaryF = func(context.Context) (consensusblock.StateSummary, error) {
		return innerSummary, nil
	}
	require.NoError(vmImpl.SetForkHeight(innerSummary.Height() + 1))
	summary, err = vmImpl.GetLastStateSummary(context.Background())
	require.NoError(err)
	require.Equal(innerSummary.ID(), summary.ID())
	require.Equal(innerSummary.Height(), summary.Height())
	require.Equal(innerSummary.Bytes(), summary.Bytes())

	// Post fork summary case
	require.NoError(vmImpl.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &blocktest.Block{
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: innerSummary.Height(),
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (consensusblock.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	slb, err := statelessblock.Build(
		vmImpl.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{PChainHeight: 100, Number: 0, StartTime: 0},
		vmImpl.StakingCertLeaf,
		innerBlk.Bytes(),
		vmImpl.rt.ChainID,
		vmImpl.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vmImpl,
			innerBlk: innerBlk,
		},
	}
	require.NoError(vmImpl.acceptPostForkBlock(proBlk))

	summary, err = vmImpl.GetLastStateSummary(context.Background())
	require.NoError(err)
	require.Equal(innerSummary.Height(), summary.Height())
}

func TestStateSyncGetStateSummary(t *testing.T) {
	require := require.New(t)

	innerVM, vmImpl := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vmImpl.Shutdown(context.Background()))
	}()
	reqHeight := uint64(1969)

	innerSummary := &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: reqHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	// No state summary case
	innerVM.GetStateSummaryF = func(context.Context, uint64) (consensusblock.StateSummary, error) {
		return nil, database.ErrNotFound
	}
	summary, err := vmImpl.GetStateSummary(context.Background(), reqHeight)
	require.ErrorIs(err, database.ErrNotFound)
	require.Nil(summary)

	// Pre fork summary case, fork height not reached hence not set yet
	innerVM.GetStateSummaryF = func(_ context.Context, h uint64) (consensusblock.StateSummary, error) {
		require.Equal(reqHeight, h)
		return innerSummary, nil
	}
	_, err = vmImpl.GetForkHeight()
	require.ErrorIs(err, database.ErrNotFound)
	summary, err = vmImpl.GetStateSummary(context.Background(), reqHeight)
	require.NoError(err)
	require.Equal(innerSummary.ID(), summary.ID())
	require.Equal(innerSummary.Height(), summary.Height())
	require.Equal(innerSummary.Bytes(), summary.Bytes())

	// Pre fork summary case, fork height already reached
	innerVM.GetStateSummaryF = func(_ context.Context, h uint64) (consensusblock.StateSummary, error) {
		require.Equal(reqHeight, h)
		return innerSummary, nil
	}
	require.NoError(vmImpl.SetForkHeight(innerSummary.Height() + 1))
	summary, err = vmImpl.GetStateSummary(context.Background(), reqHeight)
	require.NoError(err)
	require.Equal(innerSummary.ID(), summary.ID())
	require.Equal(innerSummary.Height(), summary.Height())
	require.Equal(innerSummary.Bytes(), summary.Bytes())

	// Post fork summary case
	require.NoError(vmImpl.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &blocktest.Block{
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: innerSummary.Height(),
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (consensusblock.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	slb, err := statelessblock.Build(
		vmImpl.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{PChainHeight: 100, Number: 0, StartTime: 0},
		vmImpl.StakingCertLeaf,
		innerBlk.Bytes(),
		vmImpl.rt.ChainID,
		vmImpl.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vmImpl,
			innerBlk: innerBlk,
		},
	}
	require.NoError(vmImpl.acceptPostForkBlock(proBlk))

	summary, err = vmImpl.GetStateSummary(context.Background(), reqHeight)
	require.NoError(err)
	require.Equal(innerSummary.Height(), summary.Height())
}

func TestParseStateSummary(t *testing.T) {
	require := require.New(t)
	innerVM, vmImpl := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vmImpl.Shutdown(context.Background()))
	}()
	reqHeight := uint64(1969)

	innerSummary := &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: reqHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}
	innerVM.ParseStateSummaryF = func(_ context.Context, summaryBytes []byte) (consensusblock.StateSummary, error) {
		require.Equal(summaryBytes, innerSummary.Bytes())
		return innerSummary, nil
	}
	innerVM.GetStateSummaryF = func(_ context.Context, h uint64) (consensusblock.StateSummary, error) {
		require.Equal(reqHeight, h)
		return innerSummary, nil
	}

	// Get a pre fork block than parse it
	require.NoError(vmImpl.SetForkHeight(innerSummary.Height() + 1))
	summary, err := vmImpl.GetStateSummary(context.Background(), reqHeight)
	require.NoError(err)

	parsedSummary, err := vmImpl.ParseStateSummary(context.Background(), summary.Bytes())
	require.NoError(err)
	require.Equal(summary.ID(), parsedSummary.ID())
	require.Equal(summary.Height(), parsedSummary.Height())
	require.Equal(summary.Bytes(), parsedSummary.Bytes())

	// Get a post fork block than parse it
	require.NoError(vmImpl.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &blocktest.Block{
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: innerSummary.Height(),
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (consensusblock.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	slb, err := statelessblock.Build(
		vmImpl.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{PChainHeight: 100, Number: 0, StartTime: 0},
		vmImpl.StakingCertLeaf,
		innerBlk.Bytes(),
		vmImpl.rt.ChainID,
		vmImpl.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vmImpl,
			innerBlk: innerBlk,
		},
	}
	require.NoError(vmImpl.acceptPostForkBlock(proBlk))
	require.NoError(vmImpl.SetForkHeight(innerSummary.Height() - 1))
	summary, err = vmImpl.GetStateSummary(context.Background(), reqHeight)
	require.NoError(err)

	parsedSummary, err = vmImpl.ParseStateSummary(context.Background(), summary.Bytes())
	require.NoError(err)
	require.Equal(summary.ID(), parsedSummary.ID())
	require.Equal(summary.Height(), parsedSummary.Height())
	require.Equal(summary.Bytes(), parsedSummary.Bytes())
}

func TestStateSummaryAccept(t *testing.T) {
	require := require.New(t)

	innerVM, vmImpl := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vmImpl.Shutdown(context.Background()))
	}()
	reqHeight := uint64(1969)

	innerSummary := &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: reqHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	require.NoError(vmImpl.SetForkHeight(innerSummary.Height() - 1))

	// store post fork block associated with summary
	innerBlk := &blocktest.Block{
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: innerSummary.Height(),
	}

	slb, err := statelessblock.Build(
		vmImpl.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{PChainHeight: 100, Number: 0, StartTime: 0},
		vmImpl.StakingCertLeaf,
		innerBlk.Bytes(),
		vmImpl.rt.ChainID,
		vmImpl.StakingLeafSigner,
	)
	require.NoError(err)

	statelessSummary, err := summary.Build(innerSummary.Height()-1, slb.Bytes(), innerSummary.Bytes())
	require.NoError(err)

	innerVM.ParseStateSummaryF = func(_ context.Context, summaryBytes []byte) (consensusblock.StateSummary, error) {
		require.Equal(innerSummary.BytesV, summaryBytes)
		return innerSummary, nil
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (consensusblock.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	summary, err := vmImpl.ParseStateSummary(context.Background(), statelessSummary.Bytes())
	require.NoError(err)

	// test Accept accepted
	innerSummary.AcceptF = func(context.Context) (chain.StateSyncMode, error) {
		return chain.StateSyncStatic, nil
	}
	status, err := summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(chain.StateSyncStatic, status)

	// test Accept skipped
	innerSummary.AcceptF = func(context.Context) (chain.StateSyncMode, error) {
		return chain.StateSyncSkipped, nil
	}
	status, err = summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(chain.StateSyncSkipped, status)
}

func TestStateSummaryAcceptOlderBlock(t *testing.T) {
	require := require.New(t)

	innerVM, vmImpl := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vmImpl.Shutdown(context.Background()))
	}()
	reqHeight := uint64(1969)

	innerSummary := &blocktest.StateSummary{
		IDV:     ids.ID{'s', 'u', 'm', 'm', 'a', 'r', 'y', 'I', 'D'},
		HeightV: reqHeight,
		BytesV:  []byte{'i', 'n', 'n', 'e', 'r'},
	}

	require.NoError(vmImpl.SetForkHeight(innerSummary.Height() - 1))

	// Set the last accepted block height to be higher than the state summary
	// we are going to attempt to accept
	vmImpl.lastAcceptedHeight = innerSummary.Height() + 1

	// store post fork block associated with summary
	innerBlk := &blocktest.Block{
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: innerSummary.Height(),
	}
	innerVM.GetStateSummaryF = func(_ context.Context, h uint64) (consensusblock.StateSummary, error) {
		require.Equal(reqHeight, h)
		return innerSummary, nil
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (consensusblock.Block, error) {
		require.Equal(innerBlk.Bytes(), b)
		return innerBlk, nil
	}

	slb, err := statelessblock.Build(
		vmImpl.preferred,
		innerBlk.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{PChainHeight: 100, Number: 0, StartTime: 0},
		vmImpl.StakingCertLeaf,
		innerBlk.Bytes(),
		vmImpl.rt.ChainID,
		vmImpl.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk := &postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vmImpl,
			innerBlk: innerBlk,
		},
	}
	require.NoError(vmImpl.acceptPostForkBlock(proBlk))

	summary, err := vmImpl.GetStateSummary(context.Background(), reqHeight)
	require.NoError(err)
	require.Equal(summary.Height(), reqHeight)

	// test Accept summary invokes innerVM
	calledInnerAccept := false
	innerSummary.AcceptF = func(context.Context) (chain.StateSyncMode, error) {
		innerVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
			return innerSummary.ID(), nil
		}
		innerVM.GetBlockF = func(context.Context, ids.ID) (consensusblock.Block, error) {
			return innerBlk, nil
		}
		calledInnerAccept = true
		return chain.StateSyncStatic, nil
	}
	status, err := summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(chain.StateSyncStatic, status)
	require.True(calledInnerAccept)

	require.NoError(vmImpl.SetState(context.Background(), uint32(vm.Bootstrapping)))
	require.Equal(summary.Height(), vmImpl.lastAcceptedHeight)
	lastAcceptedID, err := vmImpl.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(proBlk.ID(), lastAcceptedID)
}

// TestStateSummaryAcceptOlderBlockSkipStateSync tests the case where we accept
// a state summary older than the last accepted block. In this case, we should not
// roll the ProposerVM back to match the state summary, but we should invoke the
// innerVM to accept the state summary and re-align the ProposerVM with the innerVM
// during the transition out of state sync.
func TestStateSummaryAcceptOlderBlockSkipStateSync(t *testing.T) {
	require := require.New(t)

	innerVM, vmImpl := helperBuildStateSyncTestObjects(t)
	defer func() {
		require.NoError(vmImpl.Shutdown(context.Background()))
	}()

	// store post fork block associated with summary
	innerBlk1 := &blocktest.Block{
		IDV:     ids.GenerateTestID(),
		BytesV:  []byte{1},
		ParentV: ids.GenerateTestID(),
		HeightV: 1969,
		StatusV: blocktest.Processing,
	}
	innerBlk2 := blocktest.BuildChild(innerBlk1)

	innerSummary1 := &blocktest.StateSummary{
		IDV:     innerBlk1.ID(),
		HeightV: innerBlk1.Height(),
		BytesV:  innerBlk1.BytesV,
	}

	require.NoError(vmImpl.SetForkHeight(innerSummary1.Height() - 1))

	// Set the last accepted block height to be higher than the state summary
	// we are going to attempt to accept
	vmImpl.lastAcceptedHeight = innerBlk2.Height()

	innerVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return innerBlk2.IDV, nil
	}

	innerVM.GetBlockF = func(_ context.Context, blkID ids.ID) (consensusblock.Block, error) {
		switch blkID {
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
		case innerBlk1.ID():
			return innerBlk1, nil
		case innerBlk2.ID():
			return innerBlk2, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	innerVM.GetStateSummaryF = func(context.Context, uint64) (consensusblock.StateSummary, error) {
		return innerSummary1, nil
	}
	innerVM.ParseBlockF = func(_ context.Context, b []byte) (consensusblock.Block, error) {
		switch {
		case bytes.Equal(b, innerBlk1.BytesV):
			return innerBlk1, nil
		case bytes.Equal(b, innerBlk2.BytesV):
			return innerBlk2, nil
		default:
			require.FailNow("unexpected parse block")
			// Unreachable, but required to satisfy the compiler
			// since we use FailNow instead of panic
			return nil, nil
		}
	}
	calledInnerAccept := false
	innerSummary1.AcceptF = func(context.Context) (chain.StateSyncMode, error) {
		calledInnerAccept = true
		return chain.StateSyncSkipped, nil
	}

	slb1, err := statelessblock.Build(
		vmImpl.preferred,
		innerBlk1.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{PChainHeight: 100, Number: 0, StartTime: 0},
		vmImpl.StakingCertLeaf,
		innerBlk1.Bytes(),
		vmImpl.rt.ChainID,
		vmImpl.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk1 := &postForkBlock{
		SignedBlock: slb1,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vmImpl,
			innerBlk: innerBlk1,
		},
	}
	require.NoError(vmImpl.acceptPostForkBlock(proBlk1))

	slb2, err := statelessblock.Build(
		vmImpl.preferred,
		innerBlk2.Timestamp(),
		100, // pChainHeight,
		statelessblock.Epoch{PChainHeight: 100, Number: 0, StartTime: 0},
		vmImpl.StakingCertLeaf,
		innerBlk2.Bytes(),
		vmImpl.rt.ChainID,
		vmImpl.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk2 := &postForkBlock{
		SignedBlock: slb2,
		postForkCommonComponents: postForkCommonComponents{
			vm:       vmImpl,
			innerBlk: innerBlk2,
		},
	}
	require.NoError(vmImpl.acceptPostForkBlock(proBlk2))

	summary, err := vmImpl.GetStateSummary(context.Background(), innerBlk1.Height())
	require.NoError(err)
	require.Equal(innerBlk1.Height(), summary.Height())

	// Process a state summary that would rewind the chain
	// ProposerVM should ignore the rollback and accept the inner state summary to
	// notify the innerVM.
	// This can result in the ProposerVM and innerVM diverging their last accepted block.
	// These are re-aligned in SetState before transitioning to consensus.
	status, err := summary.Accept(context.Background())
	require.NoError(err)
	require.Equal(chain.StateSyncSkipped, status)
	require.True(calledInnerAccept)
	require.NoError(vmImpl.SetState(context.Background(), uint32(vm.Bootstrapping)))

	require.Equal(innerBlk2.Height(), vmImpl.lastAcceptedHeight)
	lastAcceptedID, err := vmImpl.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(proBlk2.ID(), lastAcceptedID)
}
