// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"testing"
	"time"

	"github.com/luxfi/metric"
	"github.com/stretchr/testify/require"
	"github.com/luxfi/mock/gomock"

	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/node/vms/components/chain/blocktest"
	"github.com/luxfi/consensus/engine/chain/chainmock"
	"github.com/luxfi/consensus/validators"
	"github.com/luxfi/consensus/validators/validatorsmock"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/consensus/engine/chain/block/blockmock"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/vms/components/chain"
	"github.com/luxfi/node/vms/proposervm/proposer"
	"github.com/luxfi/node/vms/proposervm/scheduler"
	consensuscontext "github.com/luxfi/consensus/context"
)

// testContextWrapper wraps a context to provide NodeID access for testing
type testContextWrapper struct {
	context.Context
	NodeID ids.NodeID
}

// testValidatorMockStateAdapter adapts validatorsmock.State to consensus.ValidatorState
type testValidatorMockStateAdapter struct {
	valState *validatorsmock.State
}

func (t *testValidatorMockStateAdapter) GetChainID(chainID ids.ID) (ids.ID, error) {
	if t.valState.GetChainIDF != nil {
		return t.valState.GetChainIDF(chainID)
	}
	return chainID, nil
}

func (t *testValidatorMockStateAdapter) GetNetID(chainID ids.ID) (ids.ID, error) {
	if t.valState.GetNetIDF != nil {
		return t.valState.GetNetIDF(context.Background(), chainID)
	}
	return ids.Empty, nil
}

func (t *testValidatorMockStateAdapter) GetSubnetID(chainID ids.ID) (ids.ID, error) {
	// validatorsmock.State doesn't have GetSubnetIDF, return empty for now
	return ids.Empty, nil
}

func (t *testValidatorMockStateAdapter) GetValidatorSet(height uint64, netID ids.ID) (map[ids.NodeID]uint64, error) {
	// validatorsmock.State doesn't have GetValidatorSetSimpleF, return empty for now
	return nil, nil
}

func (t *testValidatorMockStateAdapter) GetCurrentHeight() (uint64, error) {
	if t.valState.GetCurrentHeightF != nil {
		return t.valState.GetCurrentHeightF(context.Background())
	}
	return t.valState.GetCurrentHeight(context.Background())
}

func (t *testValidatorMockStateAdapter) GetMinimumHeight(ctx context.Context) (uint64, error) {
	if t.valState.GetMinimumHeightF != nil {
		return t.valState.GetMinimumHeightF(ctx)
	}
	return t.valState.GetMinimumHeight(ctx)
}

// TestChainVM provides a simple test implementation
type TestChainVM struct {
	t *testing.T
	BuildBlockWithContextF func(context.Context, *block.Context) (block.Block, error)
	BuildBlockF func(context.Context) (block.Block, error)
	GetBlockF func(context.Context, ids.ID) (block.Block, error)
	ParseBlockF func(context.Context, []byte) (block.Block, error)
	LastAcceptedF func(context.Context) (ids.ID, error)
	SetPreferenceF func(context.Context, ids.ID) error
	GetBlockIDAtHeightF func(context.Context, uint64) (ids.ID, error)
}

// BuildBlockWithContext implements the BuildBlockWithContextChainVM interface
func (vm *TestChainVM) BuildBlockWithContext(ctx context.Context, blkCtx *block.Context) (block.Block, error) {
	if vm.BuildBlockWithContextF != nil {
		return vm.BuildBlockWithContextF(ctx, blkCtx)
	}
	return nil, fmt.Errorf("BuildBlockWithContextF not set")
}

// BuildBlock implements the ChainVM interface
func (vm *TestChainVM) BuildBlock(ctx context.Context) (block.Block, error) {
	if vm.BuildBlockF != nil {
		return vm.BuildBlockF(ctx)
	}
	return nil, fmt.Errorf("BuildBlockF not set")
}

// GetBlock implements the ChainVM interface
func (vm *TestChainVM) GetBlock(ctx context.Context, id ids.ID) (block.Block, error) {
	if vm.GetBlockF != nil {
		return vm.GetBlockF(ctx, id)
	}
	return nil, fmt.Errorf("GetBlockF not set")
}

// ParseBlock implements the ChainVM interface
func (vm *TestChainVM) ParseBlock(ctx context.Context, data []byte) (block.Block, error) {
	if vm.ParseBlockF != nil {
		return vm.ParseBlockF(ctx, data)
	}
	return nil, fmt.Errorf("ParseBlockF not set")
}

// LastAccepted implements the ChainVM interface
func (vm *TestChainVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	if vm.LastAcceptedF != nil {
		return vm.LastAcceptedF(ctx)
	}
	return ids.Empty, nil
}

// SetPreference implements the ChainVM interface
func (vm *TestChainVM) SetPreference(ctx context.Context, id ids.ID) error {
	if vm.SetPreferenceF != nil {
		return vm.SetPreferenceF(ctx, id)
	}
	return nil
}

// GetBlockIDAtHeight implements the ChainVM interface
func (vm *TestChainVM) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	if vm.GetBlockIDAtHeightF != nil {
		return vm.GetBlockIDAtHeightF(ctx, height)
	}
	return ids.Empty, nil
}

// Initialize implements the ChainVM interface
func (vm *TestChainVM) Initialize(
	ctx context.Context,
	chainCtx interface{},
	dbManager interface{},
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	msgChan interface{},
	fxs []interface{},
	appSender interface{},
) error {
	return nil
}

// Assert that when the underlying VM implements ChainVMWithBuildBlockContext
// and the proposervm is activated, we call the VM's BuildBlockWithContext
// method to build a block rather than BuildBlockWithContext. If the proposervm
// isn't activated, we should call BuildBlock rather than BuildBlockWithContext.
func TestPostForkCommonComponents_buildChild(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	var (
		nodeID                 = ids.GenerateTestNodeID()
		pChainHeight    uint64 = 1337
		parentID               = ids.GenerateTestID()
		parentTimestamp        = time.Now().Truncate(time.Second)
		parentHeight    uint64 = 1234
		blkID                  = ids.GenerateTestID()
	)

	innerBlk := chainmock.NewBlock(blkID, parentID, parentHeight+1)

	builtBlk := chainmock.NewBlock(ids.GenerateTestID(), blkID, pChainHeight)

	// Use a simple mock implementation instead of gomock for ChainVM
	innerVM := &TestChainVM{t: t}

	// Mock the block builder VM functionality
	innerVM.BuildBlockWithContextF = func(ctx context.Context, blkCtx *block.Context) (block.Block, error) {
		if blkCtx.PChainHeight == pChainHeight-1 {
			return builtBlk, nil
		}
		return nil, fmt.Errorf("unexpected pchain height")
	}

	vdrState := validatorsmock.NewState(t)
	vdrState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	windower := proposer.NewMockWindower(ctrl)
	windower.EXPECT().ExpectedProposer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nodeID, nil).AnyTimes()

	pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(err)
	// Create a context with the node ID stored in it for testing
	testCtx := context.WithValue(context.Background(), "nodeID", nodeID)

	vm := &VM{
		Config: Config{
			ActivationTime:    time.Unix(0, 0),
			DurangoTime:       time.Unix(0, 0),
			StakingCertLeaf:   &staking.Certificate{},
			StakingLeafSigner: pk,
			Registerer:        metric.NewNoOpMetrics("test").Registry(),
		},
		ChainVM:        innerVM,
		blockBuilderVM: innerVM, // Use the same implementation for both
		ctx:            testCtx,
		Windower:       windower,
	}

	// Store nodeID in a way that can be accessed by ctx.NodeID pattern
	// This is a workaround for testing - create a simple context wrapper
	vm.ctx = &testContextWrapper{
		Context: testCtx,
		NodeID: nodeID,
	}

	blk := &postForkCommonComponents{
		innerBlk: innerBlk,
		vm:       vm,
	}

	// Should call BuildBlockWithContext since proposervm is activated
	gotChild, err := blk.buildChild(
		context.Background(),
		parentID,
		parentTimestamp,
		pChainHeight-1,
	)
	require.NoError(err)
	require.Equal(builtBlk, gotChild.(*postForkBlock).innerBlk)
}

func TestPreDurangoValidatorNodeBlockBuiltDelaysTests(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = mockable.MaxTime
	)
	coreVM, valState, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(ctx))
	}()

	// Build a post fork block. It'll be the parent block in our test cases
	parentTime := time.Now().Truncate(time.Second)
	proVM.Set(parentTime)

	coreParentBlk := blocktest.BuildChild(blocktest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreParentBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case coreParentBlk.ID():
			return coreParentBlk, nil
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) { // needed when setting preference
		switch {
		case bytes.Equal(b, coreParentBlk.Bytes()):
			return coreParentBlk, nil
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(ctx)
	require.NoError(err)
	require.NoError(parentBlk.Verify(ctx))
	require.NoError(parentBlk.Accept(ctx))

	// Make sure preference is duly set
	require.NoError(proVM.SetPreference(ctx, parentBlk.ID()))
	require.Equal(proVM.preferred, parentBlk.ID())
	_, err = proVM.getPostForkBlock(ctx, parentBlk.ID())
	require.NoError(err)

	// Force this node to be the only validator, so to guarantee
	// it'd be picked if block build time was before MaxVerifyDelay
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		// a validator with a weight large enough to fully fill the proposers list
		weight := uint64(proposer.MaxBuildWindows * 2)
		nodeID := consensuscontext.GetNodeID(proVM.ctx)

		return map[ids.NodeID]*validators.GetValidatorOutput{
			nodeID: {
				NodeID: nodeID,
				Weight: weight,
			},
		}, nil
	}

	coreChildBlk := blocktest.BuildChild(coreParentBlk)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreChildBlk, nil
	}

	{
		// Set local clock before MaxVerifyDelay from parent timestamp.
		// Check that child block is signed.
		localTime := parentBlk.Timestamp().Add(proposer.MaxVerifyDelay - time.Second)
		proVM.Set(localTime)

		childBlkIntf, err := proVM.BuildBlock(ctx)
		require.NoError(err)
		require.IsType(&postForkBlock{}, childBlkIntf)

		childBlk := childBlkIntf.(*postForkBlock)
		require.Equal(consensuscontext.GetNodeID(proVM.ctx), childBlk.Proposer()) // signed block
	}

	{
		// Set local clock exactly MaxVerifyDelay from parent timestamp.
		// Check that child block is unsigned.
		localTime := parentBlk.Timestamp().Add(proposer.MaxVerifyDelay)
		proVM.Set(localTime)

		childBlkIntf, err := proVM.BuildBlock(ctx)
		require.NoError(err)
		require.IsType(&postForkBlock{}, childBlkIntf)

		childBlk := childBlkIntf.(*postForkBlock)
		require.Equal(ids.EmptyNodeID, childBlk.Proposer()) // unsigned block
	}

	{
		// Set local clock between MaxVerifyDelay and MaxBuildDelay from parent
		// timestamp.
		// Check that child block is unsigned.
		localTime := parentBlk.Timestamp().Add((proposer.MaxVerifyDelay + proposer.MaxBuildDelay) / 2)
		proVM.Set(localTime)

		childBlkIntf, err := proVM.BuildBlock(ctx)
		require.NoError(err)
		require.IsType(&postForkBlock{}, childBlkIntf)

		childBlk := childBlkIntf.(*postForkBlock)
		require.Equal(ids.EmptyNodeID, childBlk.Proposer()) // unsigned block
	}

	{
		// Set local clock after MaxBuildDelay from parent timestamp.
		// Check that child block is unsigned.
		localTime := parentBlk.Timestamp().Add(proposer.MaxBuildDelay)
		proVM.Set(localTime)

		childBlkIntf, err := proVM.BuildBlock(ctx)
		require.NoError(err)
		require.IsType(&postForkBlock{}, childBlkIntf)

		childBlk := childBlkIntf.(*postForkBlock)
		require.Equal(ids.EmptyNodeID, childBlk.Proposer()) // unsigned block
	}
}

func TestPreDurangoNonValidatorNodeBlockBuiltDelaysTests(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = mockable.MaxTime
	)
	coreVM, valState, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(ctx))
	}()

	// Build a post fork block. It'll be the parent block in our test cases
	parentTime := time.Now().Truncate(time.Second)
	proVM.Set(parentTime)

	coreParentBlk := blocktest.BuildChild(blocktest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreParentBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case coreParentBlk.ID():
			return coreParentBlk, nil
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) { // needed when setting preference
		switch {
		case bytes.Equal(b, coreParentBlk.Bytes()):
			return coreParentBlk, nil
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(ctx)
	require.NoError(err)
	require.NoError(parentBlk.Verify(ctx))
	require.NoError(parentBlk.Accept(ctx))

	// Make sure preference is duly set
	require.NoError(proVM.SetPreference(ctx, parentBlk.ID()))
	require.Equal(proVM.preferred, parentBlk.ID())
	_, err = proVM.getPostForkBlock(ctx, parentBlk.ID())
	require.NoError(err)

	// Mark node as non validator
	valState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		var (
			aValidator = ids.GenerateTestNodeID()

			// a validator with a weight large enough to fully fill the proposers list
			weight = uint64(proposer.MaxBuildWindows * 2)
		)
		return map[ids.NodeID]*validators.GetValidatorOutput{
			aValidator: {
				NodeID: aValidator,
				Weight: weight,
			},
		}, nil
	}

	coreChildBlk := blocktest.BuildChild(coreParentBlk)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreChildBlk, nil
	}

	{
		// Set local clock before MaxVerifyDelay from parent timestamp.
		// Check that child block is not built.
		localTime := parentBlk.Timestamp().Add(proposer.MaxVerifyDelay - time.Second)
		proVM.Set(localTime)

		_, err := proVM.BuildBlock(ctx)
		require.ErrorIs(err, errProposerWindowNotStarted)
	}

	{
		// Set local clock exactly MaxVerifyDelay from parent timestamp.
		// Check that child block is not built.
		localTime := parentBlk.Timestamp().Add(proposer.MaxVerifyDelay)
		proVM.Set(localTime)

		_, err := proVM.BuildBlock(ctx)
		require.ErrorIs(err, errProposerWindowNotStarted)
	}

	{
		// Set local clock among MaxVerifyDelay and MaxBuildDelay from parent timestamp
		// Check that child block is not built.
		localTime := parentBlk.Timestamp().Add((proposer.MaxVerifyDelay + proposer.MaxBuildDelay) / 2)
		proVM.Set(localTime)

		_, err := proVM.BuildBlock(ctx)
		require.ErrorIs(err, errProposerWindowNotStarted)
	}

	{
		// Set local clock after MaxBuildDelay from parent timestamp
		// Check that child block is built and it is unsigned
		localTime := parentBlk.Timestamp().Add(proposer.MaxBuildDelay)
		proVM.Set(localTime)

		childBlkIntf, err := proVM.BuildBlock(ctx)
		require.NoError(err)
		require.IsType(&postForkBlock{}, childBlkIntf)

		childBlk := childBlkIntf.(*postForkBlock)
		require.Equal(ids.EmptyNodeID, childBlk.Proposer()) // unsigned block
	}
}

// We consider cases where this node is not current proposer (may be scheduled in the next future or not).
// We check that scheduler is called nonetheless, to be able to process innerVM block requests
func TestPostDurangoBuildChildResetScheduler(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	var (
		thisNodeID              = ids.GenerateTestNodeID()
		selectedProposer        = ids.GenerateTestNodeID()
		pChainHeight     uint64 = 1337
		parentID                = ids.GenerateTestID()
		parentTimestamp         = time.Now().Truncate(time.Second)
		now                     = parentTimestamp.Add(12 * time.Second)
		parentHeight     uint64 = 1234
	)

	innerBlk := chainmock.NewBlock(ids.GenerateTestID(), parentID, parentHeight+1)

	vdrState := validatorsmock.NewState(t)
	vdrState.GetMinimumHeightF = func(context.Context) (uint64, error) {
		return pChainHeight, nil
	}

	windower := proposer.NewMockWindower(ctrl)
	windower.EXPECT().ExpectedProposer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(selectedProposer, nil).AnyTimes() // return a proposer different from thisNode, to check whether scheduler is reset

	scheduler := scheduler.NewMockScheduler(ctrl)

	pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(err)
	vm := &VM{
		Config: Config{
			ActivationTime:    time.Unix(0, 0),
			DurangoTime:       time.Unix(0, 0),
			StakingCertLeaf:   &staking.Certificate{},
			StakingLeafSigner: pk,
			Registerer:        metric.NewNoOpMetrics("test").Registry(),
		},
		ChainVM: blockmock.NewChainVM(),
		ctx: consensuscontext.WithContext(context.Background(), &consensuscontext.Context{
			NodeID:         thisNodeID,
			ValidatorState: &testValidatorMockStateAdapter{valState: vdrState},
		}),
		Windower:               windower,
		Scheduler:              scheduler,
		proposerBuildSlotGauge: metric.NewGauge(metric.GaugeOpts{}),
	}
	vm.Clock.Set(now)

	blk := &postForkCommonComponents{
		innerBlk: innerBlk,
		vm:       vm,
	}

	delays := []time.Duration{
		proposer.MaxLookAheadWindow - time.Minute,
		proposer.MaxLookAheadWindow,
		proposer.MaxLookAheadWindow + time.Minute,
	}

	for _, delay := range delays {
		windower.EXPECT().MinDelayForProposer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(delay, nil).Times(1)

		// we mock the scheduler setting the exact time we expect it to be reset
		// to
		expectedSchedulerTime := parentTimestamp.Add(delay)
		scheduler.EXPECT().SetBuildBlockTime(expectedSchedulerTime).Times(1)

		_, err = blk.buildChild(
			context.Background(),
			parentID,
			parentTimestamp,
			pChainHeight-1,
		)
		require.ErrorIs(err, errUnexpectedProposer)
	}
}
