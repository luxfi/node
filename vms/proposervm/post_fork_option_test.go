// Copyright (C) 2019-2024, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/database"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/chain"
	"github.com/luxfi/node/consensus/chain/chaintest"
	"github.com/luxfi/node/consensus/engine/common"
	"github.com/luxfi/node/consensus/snowtest"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/vms/proposervm/block"
)

var _ chain.OracleBlock = (*TestOptionsBlock)(nil)

type TestOptionsBlock struct {
	chaintest.Block
	opts    [2]*chaintest.Block
	optsErr error
}

func (tob TestOptionsBlock) Options(context.Context) ([2]chain.Block, error) {
	return [2]chain.Block{tob.opts[0], tob.opts[1]}, tob.optsErr
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
	coreTestBlk := chaintest.BuildChild(chaintest.Genesis)
	preferredBlk := chaintest.BuildChild(coreTestBlk)
	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*chaintest.Block{
			preferredBlk,
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, chaintest.GenesisBytes):
			return chaintest.Genesis, nil
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

	childCoreBlk := chaintest.BuildChild(preferredBlk)
	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
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
	coreTestBlk := chaintest.BuildChild(chaintest.Genesis)
	coreOpt0 := chaintest.BuildChild(coreTestBlk)
	coreOpt1 := chaintest.BuildChild(coreTestBlk)
	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*chaintest.Block{
			coreOpt0,
			coreOpt1,
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, chaintest.GenesisBytes):
			return chaintest.Genesis, nil
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
	coreOpt0.VerifyV = errDuplicateVerify
	coreOpt1.VerifyV = errDuplicateVerify

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
	coreTestBlk := chaintest.BuildChild(chaintest.Genesis)
	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*chaintest.Block{
			chaintest.BuildChild(coreTestBlk),
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, chaintest.GenesisBytes):
			return chaintest.Genesis, nil
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

	coreVM.LastAcceptedF = chaintest.MakeLastAcceptedBlockF(
		[]*chaintest.Block{
			chaintest.Genesis,
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
	coreTestBlk := chaintest.BuildChild(chaintest.Genesis)
	oracleCoreBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*chaintest.Block{
			chaintest.BuildChild(coreTestBlk),
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, chaintest.GenesisBytes):
			return chaintest.Genesis, nil
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
	require.NotEqual(snowtest.Rejected, oracleCoreBlk.Status)

	// reject an option
	require.IsType(&postForkBlock{}, builtBlk)
	postForkOracleBlk := builtBlk.(*postForkBlock)
	opts, err := postForkOracleBlk.Options(context.Background())
	require.NoError(err)

	require.NoError(opts[0].Reject(context.Background()))
	require.NotEqual(snowtest.Rejected, oracleCoreBlk.opts[0].Status)
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

	coreTestBlk := chaintest.BuildChild(chaintest.Genesis)
	coreBlk := &TestOptionsBlock{
		Block:   *coreTestBlk,
		optsErr: chain.ErrNotOracle,
	}

	coreChildBlk := chaintest.BuildChild(coreTestBlk)

	coreVM.BuildBlockF = func(context.Context) (chain.Block, error) {
		return coreBlk, nil
	}
	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case chaintest.GenesisID:
			return chaintest.Genesis, nil
		case coreBlk.ID():
			return coreBlk, nil
		case coreChildBlk.ID():
			return coreChildBlk, nil
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
	require.Equal(chain.ErrNotOracle, err)

	// Build the child
	statelessChild, err := block.BuildOption(
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

	coreTestBlk := chaintest.BuildChild(chaintest.Genesis)
	coreOracleBlk := &TestOptionsBlock{
		Block: *coreTestBlk,
		opts: [2]*chaintest.Block{
			chaintest.BuildChild(coreTestBlk),
			chaintest.BuildChild(coreTestBlk),
		},
	}

	oracleBlkTime := proVM.Time().Truncate(time.Second)
	statelessBlock, err := block.BuildUnsigned(
		chaintest.GenesisID,
		oracleBlkTime,
		0,
		coreOracleBlk.Bytes(),
	)
	require.NoError(err)

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case chaintest.GenesisID:
			return chaintest.Genesis, nil
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, chaintest.GenesisBytes):
			return chaintest.Genesis, nil
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

	coreVM.GetBlockF = func(context.Context, ids.ID) (chain.Block, error) {
		require.FailNow("called GetBlock when unable to handle the error")
		return nil, nil
	}
	coreVM.ParseBlockF = func(context.Context, []byte) (chain.Block, error) {
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
			Registerer:          prometheus.NewRegistry(),
		},
	)

	coreVM.InitializeF = func(
		context.Context,
		*snow.Context,
		database.Database,
		[]byte,
		[]byte,
		[]byte,
		[]*common.Fx,
		common.AppSender,
	) error {
		return nil
	}
	coreVM.LastAcceptedF = func(context.Context) (ids.ID, error) {
		return coreOracleBlk.opts[0].ID(), nil
	}

	coreVM.GetBlockF = func(_ context.Context, blkID ids.ID) (chain.Block, error) {
		switch blkID {
		case chaintest.GenesisID:
			return chaintest.Genesis, nil
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
	coreVM.ParseBlockF = func(_ context.Context, b []byte) (chain.Block, error) {
		switch {
		case bytes.Equal(b, chaintest.GenesisBytes):
			return chaintest.Genesis, nil
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

	coreVM.GetBlockF = func(context.Context, ids.ID) (chain.Block, error) {
		require.FailNow("called GetBlock when unable to handle the error")
		return nil, nil
	}
	coreVM.ParseBlockF = func(context.Context, []byte) (chain.Block, error) {
		require.FailNow("called ParseBlock when unable to handle the error")
		return nil, nil
	}

	require.Equal(oracleBlkTime, statefulOptionBlock.Timestamp())
}
