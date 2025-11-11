// Copyright (C) 2019-2025, Lux Industries, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/log"
	"github.com/luxfi/metric"
	consensusblock "github.com/luxfi/consensus/engine/chain/block"
	"github.com/luxfi/consensus/engine/chain/block/blockmock"
	consensustest "github.com/luxfi/consensus/test/helpers"
	validators "github.com/luxfi/consensus/validator"
	validatorstest "github.com/luxfi/consensus/validator/validatorstest"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/proposervm/proposer"
	"github.com/luxfi/node/vms/proposervm/state"
)

func TestServiceGetProposedHeight(t *testing.T) {
	require := require.New(t)

	// Setup using existing patterns from vm_test.go
	coreVM, valState, proVM, _ := initTestProposerVM(t, time.Unix(0, 0), 0, nil)
	defer func() {
		require.NoError(proVM.Shutdown(context.Background()))
	}()

	// Set up validator state to return a known height
	currentPChainHeight := uint64(100)
	valState.GetCurrentHeightFunc = func(context.Context) (uint64, error) {
		return currentPChainHeight, nil
	}

	// Build a block so we have a preferred block
	coreBlk := &blockmock.Block{
		MockBlock: blockmock.MockBlock{},
	}
	coreBlk.IDFunc = func() ids.ID { return ids.GenerateTestID() }
	coreBlk.ParentFunc = func() ids.ID { return ids.Empty }
	coreBlk.HeightFunc = func() uint64 { return 1 }
	coreBlk.TimestampFunc = func() int64 { return 1000 }
	coreBlk.BytesFunc = func() []byte { return []byte{1, 2, 3} }
	coreBlk.VerifyFunc = func(context.Context) error { return nil }

	coreVM.BuildBlockFunc = func(context.Context) (consensusblock.Block, error) {
		return coreBlk, nil
	}

	// Build and accept a block
	blk, err := proVM.BuildBlock(context.Background())
	require.NoError(err)
	require.NoError(blk.Verify(context.Background()))
	require.NoError(proVM.SetPreference(context.Background(), blk.ID()))

	// Create the service
	service := &Service{vm: proVM}

	// Create a JSON-RPC request
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"getProposedHeight","params":{}}`
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "application/json")

	// Create args and reply
	args := &GetProposedHeightArgs{}
	reply := &GetProposedHeightReply{}

	// Call the service method directly
	err = service.GetProposedHeight(req, args, reply)
	require.NoError(err)

	// The proposed height should be >= the current P-Chain height
	require.GreaterOrEqual(reply.ProposedHeight, currentPChainHeight)
}

// initTestProposerVM initializes a proposer VM for testing
// This is a simplified version based on vm_test.go patterns
func initTestProposerVM(
	t *testing.T,
	activationTime time.Time,
	minPChainHeight uint64,
	coreGenBlk *blockmock.Block,
) (*blockmock.ChainVM, *validatorstest.State, *VM, database.Database) {
	require := require.New(t)

	// Create core VM
	coreVM := &blockmock.ChainVM{}
	coreVM.InitializeFunc = func(context.Context, interface{}, database.Database, []byte, []byte, []byte, interface{}, []interface{}, interface{}) error {
		return nil
	}
	coreVM.LastAcceptedFunc = func(context.Context) (ids.ID, error) {
		if coreGenBlk == nil {
			return ids.Empty, nil
		}
		return coreGenBlk.ID(), nil
	}
	coreVM.GetBlockFunc = func(_ context.Context, blkID ids.ID) (consensusblock.Block, error) {
		if coreGenBlk != nil && blkID == coreGenBlk.ID() {
			return coreGenBlk, nil
		}
		return nil, database.ErrNotFound
	}

	// Create validator state
	valState := &validatorstest.State{
		T: t,
	}
	valState.GetMinimumHeightFunc = func(context.Context) (uint64, error) {
		return minPChainHeight, nil
	}
	valState.GetCurrentHeightFunc = func(context.Context) (uint64, error) {
		return minPChainHeight, nil
	}
	valState.GetValidatorSetFunc = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return nil, nil
	}

	// Create the proposer VM
	db := prefixdb.New([]byte{0}, memdb.New())
	ctx := consensustest.NewContext(t)
	ctx.ValidatorState = valState

	upgrades := upgradetest.GetConfig(upgradetest.Latest)

	proVM := New(
		coreVM,
		Config{
			Upgrades:            upgrades,
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			StakingLeafSigner:   nil,
			StakingCertLeaf:     nil,
			Registerer:          metric.NewRegistry(),
		},
	)

	// Initialize the VM
	require.NoError(proVM.Initialize(
		context.Background(),
		ctx,
		db,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	))

	// Set the VM state to normal operation
	require.NoError(proVM.SetState(context.Background(), uint32(consensus.core.VMNormalOp)))

	return coreVM, valState, proVM, db
}

func TestNewHTTPHandler(t *testing.T) {
	require := require.New(t)

	// Create a minimal VM
	db := prefixdb.New([]byte{0}, memdb.New())
	coreVM := &blockmock.ChainVM{}

	valState := &validatorstest.State{T: t}
	valState.GetMinimumHeightFunc = func(context.Context) (uint64, error) {
		return 0, nil
	}

	ctx := consensustest.NewContext(t)
	ctx.ValidatorState = valState

	proVM := New(
		coreVM,
		Config{
			Upgrades:            upgradetest.GetConfig(upgradetest.Latest),
			MinBlkDelay:         DefaultMinBlockDelay,
			NumHistoricalBlocks: DefaultNumHistoricalBlocks,
			Registerer:          metric.NewRegistry(),
		},
	)

	baseState, err := state.NewMetered(db, "test", metric.NewRegistry())
	require.NoError(err)
	proVM.State = baseState
	proVM.ctx = ctx
	proVM.validatorState = valState
	proVM.Windower = proposer.New(valState, ctx.NetworkID, ctx.ChainID)

	// Test creating the HTTP handler
	handler, err := NewHTTPHandler(proVM)
	require.NoError(err)
	require.NotNil(handler)
}
