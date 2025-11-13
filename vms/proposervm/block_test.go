// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"
	"time"

	"github.com/luxfi/metric"
	"github.com/luxfi/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/upgrade/upgradetest"

	consensusblock "github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/engine/chain/block/blockmock"
	consensustest "github.com/luxfi/consensus/test/helpers"
	validators "github.com/luxfi/consensus/validator"
	validatorsmock "github.com/luxfi/consensus/validator/validatorsmock"
	"github.com/luxfi/node/utils/timer/mockable"
	componentblocktest "github.com/luxfi/node/vms/components/chain/blocktest"
	"github.com/luxfi/node/vms/proposervm/proposer"

	statelessblock "github.com/luxfi/node/vms/proposervm/block"
)

// Assert that when the underlying VM implements ChainVMWithBuildBlockContext
// and the proposervm is activated, we call the VM's BuildBlockWithContext
// method to build a block rather than BuildBlockWithContext. If the proposervm
// isn't activated, we should call BuildBlock rather than BuildBlockWithContext.
func TestPostForkCommonComponents_buildChild(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	var (
		nodeID                 = ids.GenerateTestNodeID()
		pChainHeight    uint64 = 1337
		parentID               = ids.GenerateTestID()
		parentTimestamp        = time.Now().Truncate(time.Second)
		parentHeight    uint64 = 1234
		blkID                  = ids.GenerateTestID()
		parentEpoch            = statelessblock.Epoch{}
	)

	innerBlk := blockmock.NewMockBlock(ctrl)
	innerBlk.EXPECT().ID().Return(blkID).AnyTimes()
	innerBlk.EXPECT().Height().Return(parentHeight + 1).AnyTimes()

	builtBlk := blockmock.NewMockBlock(ctrl)
	builtBlk.EXPECT().Bytes().Return([]byte{1, 2, 3}).AnyTimes()
	builtBlk.EXPECT().ID().Return(ids.GenerateTestID()).AnyTimes()
	builtBlk.EXPECT().Height().Return(pChainHeight).AnyTimes()

	innerVM := blockmock.NewMockChainVM(ctrl)
	innerBlockBuilderVM := blockmock.NewMockBuildBlockWithContextVM(ctrl)
	innerBlockBuilderVM.EXPECT().BuildBlockWithContext(gomock.Any(), &consensusblock.Context{
		PChainHeight: pChainHeight,
	}).Return(builtBlk, nil).AnyTimes()

	vdrState := validatorsmock.NewState(ctrl)
	vdrState.EXPECT().GetCurrentHeight(context.Background()).Return(pChainHeight, nil).AnyTimes()

	windower := proposer.NewMockWindower(ctrl)
	windower.EXPECT().ExpectedProposer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nodeID, nil).AnyTimes()

	pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(err)

	// Create consensus context with NodeID
	consensusCtx := consensustest.Context(t, consensustest.PChainID)

	vm := &VM{
		Config: Config{
			Upgrades:          upgradetest.GetConfig(upgradetest.Latest),
			StakingCertLeaf:   &staking.Certificate{},
			StakingLeafSigner: pk,
			Registerer:        metric.NewNoOp().Registry(),
		},
		ChainVM:        innerVM,
		blockBuilderVM: innerBlockBuilderVM,
		ctx:            consensusCtx,
		Windower:       windower,
	}

	blk := &postForkCommonComponents{
		innerBlk: innerBlk,
		vm:       vm,
	}

	// Should call BuildBlockWithContext since proposervm is activated
	_, err = blk.buildChild(
		context.Background(),
		parentID,
		parentTimestamp,
		pChainHeight,
		toChainBlockEpoch(parentEpoch),
	)
	require.NoError(err)
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

	coreParentBlk := componentblocktest.BuildChild(componentblocktest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (consensusblock.Block, error) {
		return coreParentBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (consensusblock.Block, error) {
		switch blkID {
		case coreParentBlk.ID():
			return coreParentBlk, nil
		case componentblocktest.GenesisID:
			return componentblocktest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (consensusblock.Block, error) { // needed when setting preference
		switch {
		case bytes.Equal(b, coreParentBlk.Bytes()):
			return coreParentBlk, nil
		case bytes.Equal(b, componentblocktest.GenesisBytes):
			return componentblocktest.Genesis, nil
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
		nodeID := proVM.ctx.NodeID
		return map[ids.NodeID]*validators.GetValidatorOutput{
			nodeID: {
				NodeID: nodeID,
				Weight: uint64(proposer.MaxBuildWindows * 2),
			},
		}, nil
	}

	coreChildBlk := componentblocktest.BuildChild(coreParentBlk)
	coreVM.BuildBlockF = func(context.Context) (consensusblock.Block, error) {
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
		require.Equal(proVM.ctx.NodeID, childBlk.Proposer()) // signed block
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

	coreParentBlk := componentblocktest.BuildChild(componentblocktest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (consensusblock.Block, error) {
		return coreParentBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (consensusblock.Block, error) {
		switch blkID {
		case coreParentBlk.ID():
			return coreParentBlk, nil
		case componentblocktest.GenesisID:
			return componentblocktest.Genesis, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (consensusblock.Block, error) { // needed when setting preference
		switch {
		case bytes.Equal(b, coreParentBlk.Bytes()):
			return coreParentBlk, nil
		case bytes.Equal(b, componentblocktest.GenesisBytes):
			return componentblocktest.Genesis, nil
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

	coreChildBlk := componentblocktest.BuildChild(coreParentBlk)
	coreVM.BuildBlockF = func(context.Context) (consensusblock.Block, error) {
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

// Confirm that prior to Etna activation, the P-chain height passed to the
// VM building the inner block is P-Chain height of the parent block.
func TestPreEtnaContextPChainHeight(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	var (
		nodeID                   = ids.GenerateTestNodeID()
		pChainHeight      uint64 = 1337
		parentPChainHeght        = pChainHeight - 1
		parentID                 = ids.GenerateTestID()
		parentTimestamp          = time.Now().Truncate(time.Second)
		parentEpoch              = statelessblock.Epoch{}
	)

	innerParentBlock := componentblocktest.Genesis
	innerChildBlock := componentblocktest.BuildChild(innerParentBlock)

	innerBlockBuilderVM := blockmock.NewMockBuildBlockWithContextVM(ctrl)
	// Expect the that context passed in has parent's P-Chain height
	innerBlockBuilderVM.EXPECT().BuildBlockWithContext(gomock.Any(), &consensusblock.Context{
		PChainHeight: parentPChainHeght,
	}).Return(innerChildBlock, nil).AnyTimes()

	vdrState := validatorsmock.NewState(ctrl)
	vdrState.EXPECT().GetCurrentHeight(context.Background()).Return(pChainHeight, nil).AnyTimes()

	windower := proposer.NewMockWindower(ctrl)
	windower.EXPECT().ExpectedProposer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nodeID, nil).AnyTimes()

	consensusCtx := consensustest.Context(t, consensustest.PChainID)
	vm := &VM{
		Config: Config{
			Upgrades:          upgradetest.GetConfig(upgradetest.Durango), // Use Durango for pre-Etna behavior
			StakingCertLeaf:   pTestCert,
			StakingLeafSigner: pTestSigner,
			Registerer:        metric.NewNoOp().Registry(),
		},
		blockBuilderVM: innerBlockBuilderVM,
		ctx:            consensusCtx,
		Windower:       windower,
	}

	blk := &postForkCommonComponents{
		innerBlk: innerChildBlock,
		vm:       vm,
	}

	// Should call BuildBlockWithContext since proposervm is activated
	_, err := blk.buildChild(
		context.Background(),
		parentID,
		parentTimestamp,
		parentPChainHeght,
		toChainBlockEpoch(parentEpoch),
	)
	require.NoError(err)
}

// Confirm that VM rejects blocks with non-zero epoch prior to granite upgrade activation
func TestPreGraniteBlock_NonZeroEpoch(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	_, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	innerBlk := componentblocktest.BuildChild(componentblocktest.Genesis)
	slb, err := statelessblock.Build(
		proVM.preferred,
		proVM.Time(),
		100, // pChainHeight,
		statelessblock.Epoch{
			PChainHeight: 1,
			Number:       1,
			StartTime:    1,
		},
		proVM.StakingCertLeaf,
		innerBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.StakingLeafSigner,
	)
	require.NoError(err)
	proBlk := postForkBlock{
		SignedBlock: slb,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: innerBlk,
		},
	}
	err = proBlk.Verify(context.Background())
	require.ErrorIs(err, errEpochMismatch)
}

// Verify that post-fork blocks are validated to contain the correct epoch
// information.
func TestPostGraniteBlock_EpochMatches(t *testing.T) {
	ctx := context.Background()

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(t, proVM.Shutdown(ctx))
	}()

	coreParentBlk := componentblocktest.BuildChild(componentblocktest.Genesis)
	coreChildBlk := componentblocktest.BuildChild(coreParentBlk)
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (consensusblock.Block, error) { // needed when setting preference
		switch {
		case bytes.Equal(b, componentblocktest.GenesisBytes):
			return componentblocktest.Genesis, nil
		case bytes.Equal(b, coreParentBlk.Bytes()):
			return coreParentBlk, nil
		case bytes.Equal(b, coreChildBlk.Bytes()):
			return coreChildBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.BuildBlockF = func(context.Context) (consensusblock.Block, error) {
		return coreParentBlk, nil
	}

	// Build the first proposervm block so that verification is on top of a
	// post-fork block.
	parentTime := upgrade.InitiallyActiveTime.Add(24 * time.Hour) // Some arbitrary time after initial activations
	proVM.Set(parentTime)

	parentBlk, err := proVM.BuildBlock(ctx)
	require.NoError(t, err)
	require.NoError(t, parentBlk.Verify(ctx))
	require.NoError(t, proVM.SetPreference(ctx, parentBlk.ID()))
	require.NoError(t, waitForProposerWindow(proVM, parentBlk, parentBlk.(*postForkBlock).PChainHeight()))

	tests := []struct {
		name    string
		epoch   statelessblock.Epoch
		wantErr error
	}{
		{
			name: "valid",
			epoch: statelessblock.Epoch{
				PChainHeight: 0,
				Number:       1,
				StartTime:    parentBlk.Timestamp().Unix(),
			},
			wantErr: nil,
		},
		{
			name:    "missing_epoch",
			epoch:   statelessblock.Epoch{},
			wantErr: errEpochMismatch,
		},
		{
			name: "wrong_p_chain_height",
			epoch: statelessblock.Epoch{
				PChainHeight: 1,
				Number:       1,
				StartTime:    parentBlk.Timestamp().Unix(),
			},
			wantErr: errEpochMismatch,
		},
		{
			name: "wrong_number",
			epoch: statelessblock.Epoch{
				PChainHeight: 0,
				Number:       2,
				StartTime:    parentBlk.Timestamp().Unix(),
			},
			wantErr: errEpochMismatch,
		},
		{
			name: "wrong_start_time",
			epoch: statelessblock.Epoch{
				PChainHeight: 0,
				Number:       1,
				StartTime:    parentBlk.Timestamp().Unix() + 1,
			},
			wantErr: errEpochMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			statelessBlock, err := statelessblock.Build(
				parentBlk.ID(),
				proVM.Time(),
				defaultPChainHeight,
				test.epoch,
				proVM.StakingCertLeaf,
				coreChildBlk.Bytes(),
				proVM.ctx.ChainID,
				proVM.StakingLeafSigner,
			)
			require.NoError(err)

			blockBytes := statelessBlock.Bytes()
			block, err := proVM.ParseBlock(ctx, blockBytes)
			require.NoError(err)

			err = block.Verify(ctx)
			require.ErrorIs(err, test.wantErr)
		})
	}
}
