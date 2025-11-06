// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database"
	"github.com/luxfi/ids"
	"github.com/luxfi/consensus/engine/chain"
	"github.com/luxfi/consensus/engine/chain/block/blocktest"
	"github.com/luxfi/consensus/core"
	"github.com/luxfi/node/upgrade/upgradetest"

	engineBlock "github.com/luxfi/consensus/engine/chain/block"
	proposerBlock "github.com/luxfi/node/vms/proposervm/block"
)

var (
	_ chain.OracleBlock = (*TestOptionsBlock)(nil)

	// ErrNotOracle is returned when a block doesn't implement options
	ErrNotOracle = errors.New("not an oracle block")
)

type TestOptionsBlock struct {
	blocktest.Block
	opts    [2]*blocktest.Block
	optsErr error
}

func (tob TestOptionsBlock) Options(context.Context) ([2]engineBlock.Block, error) {
	return [2]engineBlock.Block{tob.opts[0], tob.opts[1]}, tob.optsErr
}

// ProposerBlock.Verify tests section
func TestBlockVerify_PostForkOption_ParentChecks(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create post fork oracle block ...
	coreTestBlk := blocktest.BuildChild(blocktest.Genesis)
	preferredBlk := blocktest.BuildChild(coreTestBlk)
	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*blocktest.Block{
			preferredBlk,
			blocktest.BuildChild(coreTestBlk),
		},
	}

	coreVM.BuildBlockF = func(context.Context) (engineBlock.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (engineBlock.Block, error) {
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (engineBlock.Block, error) {
		switch {
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
		case bytes.Equal(b, oracleCoreBlk.Bytes()):
			return oracleCoreBlk, nil
		case bytes.Equal(b, oracleCoreBlk.opts[0].Bytes()):
			return oracleCoreBlk.opts[0], nil
		case bytes.Equal(b, oracleCoreBlk.opts[1].Bytes()):
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(parentBlk.Verify(context.Background()))
	require.NoError(proVM.SetPreference(context.Background(), parentBlk.ID()))

	// retrieve options ...
	require.IsType(&postForkBlock{}, parentBlk)
	postForkOracleBlk := parentBlk.(*postForkBlock)
	opts, err := postForkOracleBlk.Options(context.Background())
	require.NoError(err)
	require.IsType(&postForkOption{}, opts[0])

	// ... and verify them
	require.NoError(opts[0].Verify(context.Background()))
	require.NoError(opts[1].Verify(context.Background()))

	// show we can build on options
	require.NoError(proVM.SetPreference(context.Background(), opts[0].ID()))

	childCoreBlk := blocktest.BuildChild(preferredBlk)
	coreVM.BuildBlockF = func(context.Context) (engineBlock.Block, error) {
		return childCoreBlk, nil
	}
	require.NoError(waitForProposerWindow(proVM, opts[0], postForkOracleBlk.PChainHeight()))

	proChild, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&postForkBlock{}, proChild)
	require.NoError(proChild.Verify(context.Background()))
}

// ProposerBlock.Accept tests section
func TestBlockVerify_PostForkOption_CoreBlockVerifyIsCalledOnce(t *testing.T) {
	require := require.New(t)

	// Verify an option once; then show that another verify call would not call coreBlk.Verify()
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create post fork oracle block ...
	coreTestBlk := blocktest.BuildChild(blocktest.Genesis)
	coreOpt0 := blocktest.BuildChild(coreTestBlk)
	coreOpt1 := blocktest.BuildChild(coreTestBlk)
	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*blocktest.Block{
			coreOpt0,
			coreOpt1,
		},
	}

	coreVM.BuildBlockF = func(context.Context) (engineBlock.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (engineBlock.Block, error) {
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (engineBlock.Block, error) {
		switch {
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
		case bytes.Equal(b, oracleCoreBlk.Bytes()):
			return oracleCoreBlk, nil
		case bytes.Equal(b, oracleCoreBlk.opts[0].Bytes()):
			return oracleCoreBlk.opts[0], nil
		case bytes.Equal(b, oracleCoreBlk.opts[1].Bytes()):
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(parentBlk.Verify(context.Background()))
	require.NoError(proVM.SetPreference(context.Background(), parentBlk.ID()))

	// retrieve options ...
	require.IsType(&postForkBlock{}, parentBlk)
	postForkOracleBlk := parentBlk.(*postForkBlock)
	opts, err := postForkOracleBlk.Options(context.Background())
	require.NoError(err)
	require.IsType(&postForkOption{}, opts[0])

	// ... and verify them the first time
	require.NoError(opts[0].Verify(context.Background()))
	require.NoError(opts[1].Verify(context.Background()))

	// set error on coreBlock.Verify and recall Verify()
	coreOpt0.ErrV = errDuplicateVerify
	coreOpt1.ErrV = errDuplicateVerify

	// ... and verify them again. They verify without call to innerBlk
	require.NoError(opts[0].Verify(context.Background()))
	require.NoError(opts[1].Verify(context.Background()))
}

func TestBlockAccept_PostForkOption_SetsLastAcceptedBlock(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create post fork oracle block ...
	coreTestBlk := blocktest.BuildChild(blocktest.Genesis)
	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*blocktest.Block{
			blocktest.BuildChild(coreTestBlk),
			blocktest.BuildChild(coreTestBlk),
		},
	}

	coreVM.BuildBlockF = func(context.Context) (engineBlock.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (engineBlock.Block, error) {
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (engineBlock.Block, error) {
		switch {
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
		case bytes.Equal(b, oracleCoreBlk.Bytes()):
			return oracleCoreBlk, nil
		case bytes.Equal(b, oracleCoreBlk.opts[0].Bytes()):
			return oracleCoreBlk.opts[0], nil
		case bytes.Equal(b, oracleCoreBlk.opts[1].Bytes()):
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// accept oracle block
	require.NoError(parentBlk.Accept(context.Background()))

	coreVM.LastAcceptedF = blocktest.MakeLastAcceptedBlockF(
		[]*blocktest.Block{
			blocktest.Genesis,
			&oracleCoreBlk.Block,
		},
		oracleCoreBlk.opts[:],
	)
	acceptedID, err := proVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(parentBlk.ID(), acceptedID)

	// accept one of the options
	require.IsType(&postForkBlock{}, parentBlk)
	postForkOracleBlk := parentBlk.(*postForkBlock)
	opts, err := postForkOracleBlk.Options(context.Background())
	require.NoError(err)

	require.NoError(opts[0].Accept(context.Background()))

	acceptedID, err = proVM.LastAccepted(context.Background())
	require.NoError(err)
	require.Equal(opts[0].ID(), acceptedID)
}

// ProposerBlock.Reject tests section
func TestBlockReject_InnerBlockIsNotRejected(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// create post fork oracle block ...
	coreTestBlk := blocktest.BuildChild(blocktest.Genesis)
	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*blocktest.Block{
			blocktest.BuildChild(coreTestBlk),
			blocktest.BuildChild(coreTestBlk),
		},
	}

	coreVM.BuildBlockF = func(context.Context) (engineBlock.Block, error) {
		return oracleCoreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (engineBlock.Block, error) {
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (engineBlock.Block, error) {
		switch {
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
		case bytes.Equal(b, oracleCoreBlk.Bytes()):
			return oracleCoreBlk, nil
		case bytes.Equal(b, oracleCoreBlk.opts[0].Bytes()):
			return oracleCoreBlk.opts[0], nil
		case bytes.Equal(b, oracleCoreBlk.opts[1].Bytes()):
			return oracleCoreBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	builtBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	// reject oracle block
	require.NoError(builtBlk.Reject(context.Background()))
	require.NotEqual(engineBlock.StatusRejected, oracleCoreBlk.StatusV)

	// reject an option
	require.IsType(&postForkBlock{}, builtBlk)
	postForkOracleBlk := builtBlk.(*postForkBlock)
	opts, err := postForkOracleBlk.Options(context.Background())
	require.NoError(err)

	require.NoError(opts[0].Reject(context.Background()))
	require.NotEqual(engineBlock.StatusRejected, oracleCoreBlk.opts[0].StatusV)
}

func TestBlockVerify_PostForkOption_ParentIsNotOracleWithError(t *testing.T) {
	require := require.New(t)

	// Verify an option once; then show that another verify call would not call coreBlk.Verify()
	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, _ := initTestProposerVM(t, activationTime, durangoTime, 0)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	coreTestBlk := blocktest.BuildChild(blocktest.Genesis)
	coreBlk := &TestOptionsBlock{
		Block:   *coreTestBlk,
		optsErr: ErrNotOracle,
	}

	coreChildBlk := blocktest.BuildChild(coreTestBlk)

	coreVM.BuildBlockF = func(context.Context) (engineBlock.Block, error) {
		return coreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (engineBlock.Block, error) {
		switch blkID {
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		case coreChildBlk.ID():
			return coreChildBlk, nil
		default:
			return nil, database.ErrNotFound
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (engineBlock.Block, error) {
		switch {
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
		case bytes.Equal(b, coreBlk.Bytes()):
			return coreBlk, nil
		case bytes.Equal(b, coreChildBlk.Bytes()):
			return coreChildBlk, nil
		default:
			return nil, errUnknownBlock
		}
	}

	parentBlk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)

	require.IsType(&postForkBlock{}, parentBlk)
	postForkBlk := parentBlk.(*postForkBlock)
	_, err = postForkBlk.Options(context.Background())
	require.Equal(ErrNotOracle, err)

	// Build the child
	statelessChild, err := proposerBlock.BuildOption(
		postForkBlk.ID(),
		coreChildBlk.Bytes(),
	)
	require.NoError(err)

	invalidChild, err := proVM.ParseBlock(context.Background(), statelessChild.Bytes())
	if err != nil {
		// A failure to parse is okay here
		return
	}

	err = invalidChild.Verify(context.Background())
	require.ErrorIs(err, database.ErrNotFound)
}

func TestOptionTimestampValidity(t *testing.T) {
	require := require.New(t)

	var (
		activationTime = time.Unix(0, 0)
		durangoTime    = activationTime
	)
	coreVM, _, proVM, db := initTestProposerVM(t, activationTime, durangoTime, 0)

	coreTestBlk := blocktest.BuildChild(blocktest.Genesis)
	coreOracleBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*blocktest.Block{
			blocktest.BuildChild(coreTestBlk),
			blocktest.BuildChild(coreTestBlk),
		},
	}

	oracleBlkTime := proVM.Time().Truncate(time.Second)
	statelessBlock, err := proposerBlock.BuildUnsigned(
		blocktest.GenesisID,
		oracleBlkTime,
		0,
		coreOracleBlk.Bytes(),
	)
	require.NoError(err)

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (engineBlock.Block, error) {
		switch blkID {
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
		case coreOracleBlk.ID():
			return coreOracleBlk, nil
		case coreOracleBlk.opts[0].ID():
			return coreOracleBlk.opts[0], nil
		case coreOracleBlk.opts[1].ID():
			return coreOracleBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (engineBlock.Block, error) {
		switch {
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
		case bytes.Equal(b, coreOracleBlk.Bytes()):
			return coreOracleBlk, nil
		case bytes.Equal(b, coreOracleBlk.opts[0].Bytes()):
			return coreOracleBlk.opts[0], nil
		case bytes.Equal(b, coreOracleBlk.opts[1].Bytes()):
			return coreOracleBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	statefulBlock, err := proVM.ParseBlock(context.Background(), statelessBlock.Bytes())
	require.NoError(err)

	require.NoError(statefulBlock.Verify(context.Background()))

	statefulOracleBlock, ok := statefulBlock.(chain.OracleBlock)
	require.True(ok)

	options, err := statefulOracleBlock.Options(context.Background())
	require.NoError(err)

	option := options[0]
	require.NoError(option.Verify(context.Background()))

	require.NoError(statefulBlock.Accept(context.Background()))

	coreVM.GetBlockF = func(context.Context, ids.ID) (engineBlock.Block, error) {
		require.FailNow("called GetBlock when unable to handle the error")
		return nil, nil
	}
	coreVM.ParseBlockF = func(context.Context, []byte) (engineBlock.Block, error) {
		require.FailNow("called ParseBlock when unable to handle the error")
		return nil, nil
	}

	require.Equal(oracleBlkTime, option.Timestamp())

	require.NoError(option.Accept(context.Background()))
	require.NoError(proVM.Shutdown(context.Background()))

	// Restart the node.
	ctx := proVM.ctx
	proVM = New(
		coreVM,
		Config{
			Upgrades:            upgradetest.GetConfig(upgradetest.Latest),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   pTestSigner,
			StakingCertLeaf:     pTestCert,
			Registerer:          metric.NewNoOp().Registry(),
		},
	)

	coreVM.InitializeF = func(
		context.Context,
		interface{},
		interface{},
		[]byte,
		[]byte,
		[]byte,
		[]*core.Fx,
		core.AppSender,
	) error {
		return nil
	}
	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return coreOracleBlk.opts[0].ID(), nil
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (engineBlock.Block, error) {
		switch blkID {
		case blocktest.GenesisID:
			return blocktest.Genesis, nil
		case coreOracleBlk.ID():
			return coreOracleBlk, nil
		case coreOracleBlk.opts[0].ID():
			return coreOracleBlk.opts[0], nil
		case coreOracleBlk.opts[1].ID():
			return coreOracleBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (engineBlock.Block, error) {
		switch {
		case bytes.Equal(b, blocktest.GenesisBytes):
			return blocktest.Genesis, nil
		case bytes.Equal(b, coreOracleBlk.Bytes()):
			return coreOracleBlk, nil
		case bytes.Equal(b, coreOracleBlk.opts[0].Bytes()):
			return coreOracleBlk.opts[0], nil
		case bytes.Equal(b, coreOracleBlk.opts[1].Bytes()):
			return coreOracleBlk.opts[1], nil
		default:
			return nil, errUnknownBlock
		}
	}

	require.NoError(proVM.Initialize(
		context.Background(),
		ctx,
		db,
		nil,
		nil,
		nil,
		nil,
		nil,
	))
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	statefulOptionBlock, err := proVM.ParseBlock(context.Background(), option.Bytes())
	require.NoError(err)

	require.LessOrEqual(statefulOptionBlock.Height(), proVM.lastAcceptedHeight)

	coreVM.GetBlockF = func(context.Context, ids.ID) (engineBlock.Block, error) {
		require.FailNow("called GetBlock when unable to handle the error")
		return nil, nil
	}
	coreVM.ParseBlockF = func(context.Context, []byte) (engineBlock.Block, error) {
		require.FailNow("called ParseBlock when unable to handle the error")
		return nil, nil
	}

	require.Equal(oracleBlkTime, statefulOptionBlock.Timestamp())
}
