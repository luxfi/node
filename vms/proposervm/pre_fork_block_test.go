// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/choices"
	"github.com/luxfi/node/consensus/consensustest"
	"github.com/luxfi/node/consensus/engine/chain/block"
	"github.com/luxfi/node/consensus/chain"
	"github.com/luxfi/node/consensus/chain/chaintest"
	"github.com/luxfi/node/consensus/chain/chainmock"
	"github.com/luxfi/node/consensus/validators/validatorsmock"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/timer/mockable"

	statelessblock "github.com/luxfi/node/vms/proposervm/block"
)

func TestOracle_PreForkBlkImplementsInterface(t *testing.T) {
	require := require.New(t)

	// setup
	proBlk := preForkBlock{
		Block: chaintest.BuildChild(chaintest.Genesis),
	}

	// test
	_, err := proBlk.Options(context.Background())
	require.Equal(chain.ErrNotOracle, err)

	// setup
	proBlk = preForkBlock{
		Block: &TestOptionsBlock{},
	}

	// test
	_, err = proBlk.Options(context.Background())
	require.NoError(err)
}

func TestOracle_PreForkBlkCanBuiltOnPreForkOption(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = mockable.MaxTime
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create pre fork oracle block ...
	coreTestBlk := chaintest.BuildChild(chaintest.Genesis)
	preferredTestBlk := chaintest.BuildChild(coreTestBlk)
	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]chain.Block{
			preferredTestBlk,
			chaintest.BuildChild(coreTestBlk),
		},
	}

	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case chaintest.GenesisID:
			return chaintest.Genesis, nil
		case oracleCoreBlk.ID():
			return oracleCoreBlk, nil
		case oracleCoreBlk.opts[0].ID():
			return oracleCoreBlk.opts[0], nil
		case oracleCoreBlk.opts[1].ID():
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// retrieve options ...
	require.IsType(&preForkBlock{}, parentBlk)
	preForkOracleBlk := parentBlk.(*preForkBlock)
	opts, err := preForkOracleBlk.Options(context.Background())
	require.NoError(err)
	require.NoError(opts[0].Verify(context.Background()))

	// ... show a block can be built on top of an option
	require.NoError(proVM.SetPreference(context.Background(), opts[0].ID()))

	lastCoreBlk := &TestOptionsBlock{
		Block: *chaintest.BuildChild(preferredTestBlk),
	}
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return lastCoreBlk, nil
	}

	preForkChild, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, preForkChild)
}

func TestOracle_PostForkBlkCanBuiltOnPreForkOption(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = chaintest.GenesisTimestamp.Add(10 * time.Second)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create pre fork oracle block pre activation time...
	coreTestBlk := chaintest.BuildChild(chaintest.Genesis)
	coreTestBlk.TimestampV = activationTime.Add(-1 * time.Second)

	// ... whose options are post activation time
	preferredBlk := chaintest.BuildChild(coreTestBlk)
	preferredBlk.TimestampV = activationTime.Add(time.Second)

	unpreferredBlk := chaintest.BuildChild(coreTestBlk)
	unpreferredBlk.TimestampV = activationTime.Add(time.Second)

	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]chain.Block{
			preferredBlk,
			unpreferredBlk,
		},
	}

	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case chaintest.GenesisID:
			return chaintest.Genesis, nil
		case oracleCoreBlk.ID():
			return oracleCoreBlk, nil
		case oracleCoreBlk.opts[0].ID():
			return oracleCoreBlk.opts[0], nil
		case oracleCoreBlk.opts[1].ID():
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// retrieve options ...
	require.IsType(&preForkBlock{}, parentBlk)
	preForkOracleBlk := parentBlk.(*preForkBlock)
	opts, err := preForkOracleBlk.Options(context.Background())
	require.NoError(err)
	require.NoError(opts[0].Verify(context.Background()))

	// ... show a block can be built on top of an option
	require.NoError(proVM.SetPreference(context.Background(), opts[0].ID()))

	lastCoreBlk := &TestOptionsBlock{
		Block: *chaintest.BuildChild(preferredBlk),
	}
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return lastCoreBlk, nil
	}

	postForkChild, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, postForkChild)
}

func TestBlockVerify_PreFork_ParentChecks(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = chaintest.GenesisTimestamp.Add(10 * time.Second)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create parent block ...
	parentCoreBlk := chaintest.BuildChild(chaintest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return parentCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case chaintest.GenesisID:
			return chaintest.Genesis, nil
		case parentCoreBlk.ID():
			return parentCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, chaintest.GenesisBytes):
			return chaintest.Genesis, nil
		case bytes.Equal(b, parentCoreBlk.Bytes()):
			return parentCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// .. create child block ...
	childCoreBlk := chaintest.BuildChild(parentCoreBlk)
	childBlk := preForkBlock{
		Block: childCoreBlk,
		vm:    proVM,
	}

	{
		// child block referring unknown parent does not verify
		unknownID := ids.GenerateTestID()
		childCoreBlk.ParentV = unknownID
		err = childBlk.Verify(context.Background())
		require.ErrorIs(err, database.ErrNotFound)
	}

	{
		// child block referring known parent does verify
		childCoreBlk.ParentV = parentBlk.ID()
		require.NoError(childBlk.Verify(context.Background()))
	}
}

func TestBlockVerify_BlocksBuiltOnPreForkGenesis(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = chaintest.GenesisTimestamp.Add(10 * time.Second)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	preActivationTime := activationTime.Add(-1 * time.Second)
	proVM.Set(preActivationTime)

	coreBlk := chaintest.BuildChild(chaintest.Genesis)
	coreBlk.TimestampV = preActivationTime
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk, nil
	}

	// preFork block verifies if parent is before fork activation time
	preForkChild, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, preForkChild)

	require.NoError(preForkChild.Verify(context.Background()))

	// postFork block does NOT verify if parent is before fork activation time
	postForkStatelessChild, err := statelessblock.Build(
		chaintest.GenesisID,
		coreBlk.Timestamp(),
		0, // pChainHeight
		proVM.StakingCertLeaf,
		coreBlk.Bytes(),
		proVM.ctx.ChainID,
		proVM.StakingLeafSigner,
	)
	require.NoError(err)
	postForkChild := &postForkBlock{
		SignedBlock: postForkStatelessChild,
		postForkCommonComponents: postForkCommonComponents{
			vm:       proVM,
			innerBlk: coreBlk,
			status:   choices.Processing,
		},
	}

	require.True(postForkChild.Timestamp().Before(activationTime))
	err = postForkChild.Verify(context.Background())
	require.ErrorIs(err, errProposersNotActivated)

	// once activation time is crossed postForkBlock are produced
	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreVM.SetPreferenceF = func(context.Context, ids.ID) error {
		return nil
	}
	require.NoError(proVM.SetPreference(context.Background(), preForkChild.ID()))

	secondCoreBlk := chaintest.BuildChild(coreBlk)
	secondCoreBlk.TimestampV = postActivationTime
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return secondCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, id ids.ID) (chain.Block, error) {
		switch id {
		case chaintest.GenesisID:
			return chaintest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			require.FailNow("attempt to get unknown block")
			return nil, nil
		}
	}

	lastPreForkBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, lastPreForkBlk)

	require.NoError(lastPreForkBlk.Verify(context.Background()))

	require.NoError(proVM.SetPreference(context.Background(), lastPreForkBlk.ID()))
	thirdCoreBlk := chaintest.BuildChild(secondCoreBlk)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return thirdCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, id ids.ID) (chain.Block, error) {
		switch id {
		case chaintest.GenesisID:
			return chaintest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		case secondCoreBlk.ID():
			return secondCoreBlk, nil
		default:
			require.FailNow("attempt to get unknown block")
			return nil, nil
		}
	}

	firstPostForkBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, firstPostForkBlk)

	require.NoError(firstPostForkBlk.Verify(context.Background()))
}

func TestBlockVerify_BlocksBuiltOnPostForkGenesis(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = chaintest.GenesisTimestamp.Add(-1 * time.Second)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	proVM.Set(activationTime)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// build parent block after fork activation time ...
	coreBlock := chaintest.BuildChild(chaintest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlock, nil
	}

	// postFork block verifies if parent is after fork activation time
	postForkChild, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, postForkChild)

	require.NoError(postForkChild.Verify(context.Background()))

	// preFork block does NOT verify if parent is after fork activation time
	preForkChild := preForkBlock{
		Block: coreBlock,
		vm:    proVM,
	}
	err = preForkChild.Verify(context.Background())
	require.ErrorIs(err, errUnexpectedBlockType)
}

func TestBlockAccept_PreFork_SetsLastAcceptedBlock(t *testing.T) {
	require := require.New(t)

	// setup
	var (
		activationTime = mockable.MaxTime
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreBlk := chaintest.BuildChild(chaintest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case chaintest.GenesisID:
			return chaintest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, chaintest.GenesisBytes):
			return chaintest.Genesis, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	builtBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// test
	require.NoError(builtBlk.Accept(context.Background()))

	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		// Check if the coreBlk was accepted using consensustest.Status
		if coreBlk.Status == consensustest.Accepted {
			return coreBlk.ID(), nil
		}
		return chaintest.GenesisID, nil
	}
	acceptedID, err := proVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(builtBlk.ID(), acceptedID)
}

// ProposerBlock.Reject tests section
func TestBlockReject_PreForkBlock_InnerBlockIsRejected(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = mockable.MaxTime
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreBlk := chaintest.BuildChild(chaintest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk, nil
	}

	sb, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, sb)
	proBlk := sb.(*preForkBlock)

	require.NoError(proBlk.Reject(context.Background()))
	// Pre-fork blocks always report Processing status, check inner block instead
	require.Equal(consensustest.Rejected, coreBlk.Status)
}

func TestBlockVerify_ForkBlockIsOracleBlock(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = chaintest.GenesisTimestamp.Add(10 * time.Second)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreTestBlk := chaintest.BuildChild(chaintest.Genesis)
	coreTestBlk.TimestampV = postActivationTime
	coreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]chain.Block{
			chaintest.BuildChild(coreTestBlk),
			chaintest.BuildChild(coreTestBlk),
		},
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case chaintest.GenesisID:
			return chaintest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		case coreBlk.opts[0].ID():
			return coreBlk.opts[0], nil
		case coreBlk.opts[1].ID():
			return coreBlk.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, chaintest.GenesisBytes):
			return chaintest.Genesis, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		case bytes.Equal(b, coreBlk.opts[0].Bytes()):
			return coreBlk.opts[0], nil
		case bytes.Equal(b, coreBlk.opts[1].Bytes()):
			return coreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	firstBlock, err := proVM.ParseBlock(context.Background(), coreBlk.Bytes())
	require.NoError(err)

	require.NoError(firstBlock.Verify(context.Background()))

	oracleBlock, ok := firstBlock.(chain.OracleBlock)
	require.True(ok)

	options, err := oracleBlock.Options(context.Background())
	require.NoError(err)

	require.NoError(options[0].Verify(context.Background()))

	require.NoError(options[1].Verify(context.Background()))
}

func TestBlockVerify_ForkBlockIsOracleBlockButChildrenAreSigned(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = chaintest.GenesisTimestamp.Add(10 * time.Second)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreTestBlk := chaintest.BuildChild(chaintest.Genesis)
	coreTestBlk.TimestampV = postActivationTime
	coreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]chain.Block{
			chaintest.BuildChild(coreTestBlk),
			chaintest.BuildChild(coreTestBlk),
		},
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case chaintest.GenesisID:
			return chaintest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		case coreBlk.opts[0].ID():
			return coreBlk.opts[0], nil
		case coreBlk.opts[1].ID():
			return coreBlk.opts[1], nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, chaintest.GenesisBytes):
			return chaintest.Genesis, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		case bytes.Equal(b, coreBlk.opts[0].Bytes()):
			return coreBlk.opts[0], nil
		case bytes.Equal(b, coreBlk.opts[1].Bytes()):
			return coreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	firstBlock, err := proVM.ParseBlock(context.Background(), coreBlk.Bytes())
	require.NoError(err)

	require.NoError(firstBlock.Verify(context.Background()))

	slb, err := statelessblock.Build(
		firstBlock.ID(), // refer unknown parent
		firstBlock.Timestamp(),
		0, // pChainHeight,
		proVM.StakingCertLeaf,
		coreBlk.opts[0].Bytes(),
		proVM.ctx.ChainID,
		proVM.StakingLeafSigner,
	)
	require.NoError(err)

	invalidChild, err := proVM.ParseBlock(context.Background(), slb.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify(context.Background())
	require.ErrorIs(err, errUnexpectedBlockType)
}

// Assert that when the underlying VM implements ChainVMWithBuildBlockContext
// and the proposervm is activated, we only call the VM's BuildBlockWithContext
// when a P-chain height can be correctly provided from the parent block.
func TestPreForkBlock_BuildBlockWithContext(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	pChainHeight := uint64(1337)
	blkID := ids.GenerateTestID()
	innerBlk := chainmock.NewBlock(ctrl)
	innerBlk.EXPECT().ID().Return(blkID).AnyTimes()
	innerBlk.EXPECT().Timestamp().Return(mockable.MaxTime)
	builtBlk := chainmock.NewBlock(ctrl)
	builtBlk.EXPECT().Bytes().Return([]byte{1, 2, 3}).AnyTimes()
	builtBlk.EXPECT().ID().Return(ids.GenerateTestID()).AnyTimes()
	builtBlk.EXPECT().Height().Return(pChainHeight).AnyTimes()
	innerVM := block.NewMockChainVM(ctrl)
	innerVM.EXPECT().BuildBlock(gomock.Any()).Return(builtBlk, nil).AnyTimes()
	vdrState := validatorsmock.NewState(ctrl)
	vdrState.EXPECT().GetMinimumHeight(context.Background()).Return(pChainHeight, nil).AnyTimes()

	vm := &VM{
		ChainVM: innerVM,
		ctx: &consensus.Context{
			ValidatorState: vdrState,
			Log:            logging.NoLog{},
		},
	}

	blk := &preForkBlock{
		Block: innerBlk,
		vm:    vm,
	}

	// Should call BuildBlock since proposervm won't have a P-chain height
	gotChild, err := blk.buildChild(context.Background())
	require.NoError(err)
	require.Equal(builtBlk, gotChild.(*postForkBlock).innerBlk)

	// Should call BuildBlock since proposervm is not activated
	innerBlk.EXPECT().Timestamp().Return(time.Time{})
	vm.ActivationTime = mockable.MaxTime

	gotChild, err = blk.buildChild(context.Background())
	require.NoError(err)
	require.Equal(builtBlk, gotChild.(*preForkBlock).Block)
}
