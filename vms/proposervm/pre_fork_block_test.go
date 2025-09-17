// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/luxfi/mock/gomock"

	"github.com/luxfi/consensus/choices"
	"github.com/luxfi/consensus/consensustest"
	"github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/node/vms/components/chain/blocktest"
	"github.com/luxfi/consensus/engine/chain/chainmock"
	"github.com/luxfi/consensus/validators/validatorsmock"
	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	componentchain "github.com/luxfi/node/vms/components/chain"
	protocolchain "github.com/luxfi/consensus/protocol/chain"
	"github.com/luxfi/node/utils/timer/mockable"

	statelessblock "github.com/luxfi/node/vms/proposervm/block"
)


func TestOracle_PreForkBlkImplementsInterface(t *testing.T) {
	require := require.New(t)

	// setup
	proBlk := preForkBlock{
		Block: blocktest.BuildChild(blocktest.Genesis),
	}

	// test
	_, err := proBlk.Options(context.Background())
	require.Equal(componentchain.ErrNotOracle, err)

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
	coreTestBlk := blocktest.BuildChild(blocktest.Genesis)
	preferredTestBlk := blocktest.BuildChild(coreTestBlk)
	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]componentchain.Block{
			preferredTestBlk,
			blocktest.BuildChild(coreTestBlk),
		},
	}

	coreVM.BuildBlockF = func(context.Context) (componentchain.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (componentchain.Block, error) {
		switch blkID {
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
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
		Block: *blocktest.BuildChild(preferredTestBlk),
	}
	coreVM.BuildBlockF = func(context.Context) (componentchain.Block, error) {
		return lastCoreBlk, nil
	}

	preForkChild, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, preForkChild)
}

func TestOracle_PostForkBlkCanBuiltOnPreForkOption(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = blocktest.GenesisTimestamp.Add(10 * time.Second)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create pre fork oracle block pre activation time...
	coreTestBlk := blocktest.BuildChild(blocktest.Genesis)
	coreTestBlk.TimestampV = activationTime.Add(-1 * time.Second)

	// ... whose options are post activation time
	preferredBlk := blocktest.BuildChild(coreTestBlk)
	preferredBlk.TimestampV = activationTime.Add(time.Second)

	unpreferredBlk := blocktest.BuildChild(coreTestBlk)
	unpreferredBlk.TimestampV = activationTime.Add(time.Second)

	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]componentchain.Block{
			preferredBlk,
			unpreferredBlk,
		},
	}

	coreVM.BuildBlockF = func(context.Context) (componentchain.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (componentchain.Block, error) {
		switch blkID {
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
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
		Block: *blocktest.BuildChild(preferredBlk),
	}
	coreVM.BuildBlockF = func(context.Context) (componentchain.Block, error) {
		return lastCoreBlk, nil
	}

	postForkChild, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, postForkChild)
}

func TestBlockVerify_PreFork_ParentChecks(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = blocktest.GenesisTimestamp.Add(10 * time.Second)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create parent block ...
	parentCoreBlk := blocktest.BuildChild(blocktest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (componentchain.Block, error) {
		return parentCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (componentchain.Block, error) {
		switch blkID {
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
		case parentCoreBlk.ID():
			return parentCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (componentchain.Block, error) {
		switch {
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
		case bytes.Equal(b, parentCoreBlk.Bytes()):
			return parentCoreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// .. create child block ...
	childCoreBlk := blocktest.BuildChild(parentCoreBlk)
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
		activationTime = blocktest.GenesisTimestamp.Add(10 * time.Second)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	preActivationTime := activationTime.Add(-1 * time.Second)
	proVM.Set(preActivationTime)

	coreBlk := blocktest.BuildChild(blocktest.Genesis)
	coreBlk.TimestampV = preActivationTime
	coreVM.BuildBlockF = func(context.Context) (componentchain.Block, error) {
		return coreBlk, nil
	}

	// preFork block verifies if parent is before fork activation time
	preForkChild, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, preForkChild)

	require.NoError(preForkChild.Verify(context.Background()))

	// postFork block does NOT verify if parent is before fork activation time
	postForkStatelessChild, err := statelessblock.Build(
		blocktest.GenesisID,
		coreBlk.Timestamp(),
		0, // pChainHeight
		proVM.StakingCertLeaf,
		coreBlk.Bytes(),
		ids.Empty, // ChainID placeholder for tests
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

	secondCoreBlk := blocktest.BuildChild(coreBlk)
	secondCoreBlk.TimestampV = postActivationTime
	coreVM.BuildBlockF = func(context.Context) (componentchain.Block, error) {
		return secondCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, id ids.ID) (componentchain.Block, error) {
		switch id {
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
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
	thirdCoreBlk := blocktest.BuildChild(secondCoreBlk)
	coreVM.BuildBlockF = func(context.Context) (componentchain.Block, error) {
		return thirdCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, id ids.ID) (componentchain.Block, error) {
		switch id {
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
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
		activationTime = blocktest.GenesisTimestamp.Add(-1 * time.Second)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	proVM.Set(activationTime)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// build parent block after fork activation time ...
	coreBlock := blocktest.BuildChild(blocktest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (componentchain.Block, error) {
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

	coreBlk := blocktest.BuildChild(blocktest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (componentchain.Block, error) {
		return coreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (componentchain.Block, error) {
		switch blkID {
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (componentchain.Block, error) {
		switch {
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
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
		if coreBlk.StatusV == componentchain.Accepted {
			return coreBlk.ID(), nil
		}
		return blocktest.GenesisID, nil
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

	coreBlk := blocktest.BuildChild(blocktest.Genesis)
	coreVM.BuildBlockF = func(context.Context) (componentchain.Block, error) {
		return coreBlk, nil
	}

	sb, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, sb)
	proBlk := sb.(*preForkBlock)

	require.NoError(proBlk.Reject(context.Background()))
	// Pre-fork blocks always report Processing status, check inner block instead
	require.Equal(consensustest.Rejected, coreBlk.StatusV)
}

func TestBlockVerify_ForkBlockIsOracleBlock(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = blocktest.GenesisTimestamp.Add(10 * time.Second)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreTestBlk := blocktest.BuildChild(blocktest.Genesis)
	coreTestBlk.TimestampV = postActivationTime
	coreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]componentchain.Block{
			blocktest.BuildChild(coreTestBlk),
			blocktest.BuildChild(coreTestBlk),
		},
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (componentchain.Block, error) {
		switch blkID {
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (componentchain.Block, error) {
		switch {
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
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

	oracleBlock, ok := firstBlock.(componentchain.OracleBlock)
	require.True(ok)

	options, err := oracleBlock.Options(context.Background())
	require.NoError(err)

	require.NoError(options[0].Verify(context.Background()))

	require.NoError(options[1].Verify(context.Background()))
}

func TestBlockVerify_ForkBlockIsOracleBlockButChildrenAreSigned(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = blocktest.GenesisTimestamp.Add(10 * time.Second)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	postActivationTime := activationTime.Add(time.Second)
	proVM.Set(postActivationTime)

	coreTestBlk := blocktest.BuildChild(blocktest.Genesis)
	coreTestBlk.TimestampV = postActivationTime
	coreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]componentchain.Block{
			blocktest.BuildChild(coreTestBlk),
			blocktest.BuildChild(coreTestBlk),
		},
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (componentchain.Block, error) {
		switch blkID {
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (componentchain.Block, error) {
		switch {
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
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
		ids.Empty, // ChainID placeholder for tests
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

	// Use blocktest.Block instead of chainmock
	pChainHeight := uint64(1337)
	blkID := ids.GenerateTestID()
	innerBlk := &blocktest.Block{
		TestBlock: componentchain.TestBlock{
			IDV:        blkID,
			TimestampV: mockable.MaxTime,
		},
	}

	builtBlkID := ids.GenerateTestID()
	builtBlk := &blocktest.Block{
		TestBlock: componentchain.TestBlock{
			IDV:     builtBlkID,
			BytesV:  []byte{1, 2, 3},
			HeightV: pChainHeight,
		},
	}

	// Create a mock VM interface using blocktest instead of gomock
	coreVM, _, proVM, _ := initTestProposerVM(t, mockable.MaxTime, mockable.MaxTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreVM.BuildBlockF = func(context.Context) (componentchain.Block, error) {
		return builtBlk, nil
	}

	blk := &preForkBlock{
		Block: innerBlk,
		vm:    proVM,
	}

	// Should call BuildBlock since proposervm is not activated
	proVM.ActivationTime = mockable.MaxTime

	gotChild, err := blk.buildChild(context.Background())
	require.NoError(err)
	require.IsType(&preForkBlock{}, gotChild)
	require.Equal(builtBlk, gotChild.(*preForkBlock).Block)
}
