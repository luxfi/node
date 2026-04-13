// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/address"
	"github.com/luxfi/vm"
	"github.com/luxfi/runtime"
	"github.com/luxfi/consensus/core/choices"
	validators "github.com/luxfi/validators"
	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/vm/chains/atomic"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/upgrade/upgradetest"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/node/vms/xvm/txs/txstest"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/secp256k1fx"
)

// Test keys for use in tests
var keys = secp256k1.TestKeys()

// durango is a shorthand for the Durango fork config
var durango = upgradetest.GetConfig(upgradetest.Durango)

// Test constants
const (
	// Increased from 50000 to 10*constants.Lux to match larger genesis allocation
	// This ensures tests have enough funds for sequential transactions
	startBalance uint64 = 10 * constants.Lux // 10 LUX = 10,000,000,000 nanoLux
	testTxFee    uint64 = 1000
)

var assetID = ids.GenerateTestID()

// envConfig configures the test environment
type envConfig struct {
	fork              upgrade.Config
	notLinearized     bool
	additionalFxs     []interface{}
	indexTransactions bool // Enable transaction indexing
}

// testEnv is the test environment
type testEnv struct {
	vm           *VM
	consensusRuntime *runtime.Runtime
	genesisBytes []byte
	genesisTx    *txs.Tx
	testLock     *sync.Mutex
	txBuilder    *txstest.Builder
	sharedMemory *atomic.Memory
}

// newGenesisBytesTest creates test genesis bytes
func newGenesisBytesTest(t *testing.T) []byte {
	require := require.New(t)

	// Format address properly as Bech32
	addr, err := address.FormatBech32(constants.GetHRP(constants.UnitTestID), keys[0].PublicKey().Address().Bytes())
	require.NoError(err)

	// Create a simple genesis with one asset (LUX)
	// Increased from 100 LUX to 1000 LUX to ensure sufficient funds for multiple transactions in tests
	genesisData := map[string]GenesisAssetDefinition{
		"LUX": {
			Name:         "Lux",
			Symbol:       "LUX",
			Denomination: 9,
			InitialState: AssetInitialState{
				FixedCap: []GenesisHolder{
					{
						Amount:  1000 * constants.Lux,
						Address: addr,
					},
				},
			},
		},
	}

	genesis, err := NewGenesis(constants.UnitTestID, genesisData)
	require.NoError(err)

	genesisBytes, err := genesis.Bytes()
	require.NoError(err)

	return genesisBytes
}

// getCreateTxFromGenesisTest extracts a create asset tx from genesis
func getCreateTxFromGenesisTest(t *testing.T, genesisBytes []byte, assetAlias string) *txs.Tx {
	require := require.New(t)

	c, err := newGenesisCodec()
	require.NoError(err)

	genesis := &Genesis{}
	_, err = c.Unmarshal(genesisBytes, genesis)
	require.NoError(err)

	for _, asset := range genesis.Txs {
		if asset.Alias == assetAlias {
			tx := &txs.Tx{Unsigned: &asset.CreateAssetTx}
			require.NoError(tx.Initialize(c))
			return tx
		}
	}

	require.FailNow("asset not found in genesis", assetAlias)
	return nil
}

// mockValidatorState is a simple validator state for tests
type mockValidatorState struct {
	chainID ids.ID
}

var _ validators.State = (*mockValidatorState)(nil)

func (m *mockValidatorState) GetChainID(ids.ID) (ids.ID, error) {
	return m.chainID, nil
}

func (m *mockValidatorState) GetNetworkID(ids.ID) (ids.ID, error) {
	return m.chainID, nil
}

func (m *mockValidatorState) GetMinimumHeight(context.Context) (uint64, error) {
	return 0, nil
}

func (m *mockValidatorState) GetCurrentHeight(context.Context) (uint64, error) {
	return 0, nil
}

func (m *mockValidatorState) GetValidatorSet(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	// Return a simple validator set with the test node
	nodeID := ids.GenerateTestNodeID()
	return map[ids.NodeID]*validators.GetValidatorOutput{
		nodeID: {
			NodeID: nodeID,
			Weight: 1000,
		},
	}, nil
}

func (m *mockValidatorState) GetCurrentValidators(ctx context.Context, height uint64, netID ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
	return m.GetValidatorSet(ctx, height, netID)
}

func (m *mockValidatorState) GetWarpValidatorSet(ctx context.Context, height uint64, netID ids.ID) (*validators.WarpSet, error) {
	return &validators.WarpSet{
		Height:     height,
		Validators: make(map[ids.NodeID]*validators.WarpValidator),
	}, nil
}

func (m *mockValidatorState) GetWarpValidatorSets(ctx context.Context, heights []uint64, netIDs []ids.ID) (map[ids.ID]map[uint64]*validators.WarpSet, error) {
	result := make(map[ids.ID]map[uint64]*validators.WarpSet)
	for _, netID := range netIDs {
		result[netID] = make(map[uint64]*validators.WarpSet)
		for _, height := range heights {
			result[netID][height] = &validators.WarpSet{
				Height:     height,
				Validators: make(map[ids.NodeID]*validators.WarpValidator),
			}
		}
	}
	return result, nil
}

// testSharedMemory wraps atomic.SharedMemory to match VM's SharedMemory interface
type testSharedMemory struct {
	mem atomic.SharedMemory
}

func (t *testSharedMemory) Get(peerChainID ids.ID, keys [][]byte) ([][]byte, error) {
	return t.mem.Get(peerChainID, keys)
}

func (t *testSharedMemory) Apply(requests map[ids.ID]interface{}, _ ...interface{}) error {
	// Convert interface{} map to *atomic.Requests map
	atomicRequests := make(map[ids.ID]*atomic.Requests)
	for chainID, req := range requests {
		if atomicReq, ok := req.(*atomic.Requests); ok {
			atomicRequests[chainID] = atomicReq
		}
	}
	return t.mem.Apply(atomicRequests)
}

// setup creates a test environment
func setup(t testing.TB, config *envConfig) *testEnv {
	require := require.New(t)

	if config == nil {
		config = &envConfig{
			fork: upgradetest.GetConfig(upgradetest.Latest),
		}
	}

	chainID := ids.GenerateTestID()
	rt := &runtime.Runtime{
		NetworkID:      constants.UnitTestID,
		ChainID:        chainID,
		XChainID:       ids.GenerateTestID(),
		CChainID:       ids.GenerateTestID(),
		NodeID:         ids.GenerateTestNodeID(),
		ValidatorState: &mockValidatorState{chainID: chainID},
	}

	baseDB := memdb.New()
	sharedMemory := atomic.NewMemory(memdb.New())

	vmImpl := &VM{}
	genesisBytes := newGenesisBytesTest(t.(*testing.T))
	// Create shared memory wrapper that matches VM's interface
	atomicMem := sharedMemory.NewSharedMemory(rt.ChainID)
	vmImpl.SharedMemory = &testSharedMemory{mem: atomicMem}

	testLock := &sync.Mutex{}
	testLock.Lock()

	// Create a mock Sender
	appSender := &noOpSender{}

	// ALWAYS include secp256k1fx first (required for genesis parsing)
	// Then add additional Fxs if provided, or default to nftfx and propertyfx
	fxs := []interface{}{
		&vm.Fx{
			ID: secp256k1fx.ID,
			Fx: &secp256k1fx.Fx{},
		},
	}

	if len(config.additionalFxs) == 0 {
		// No additional Fxs specified - add default nftfx and propertyfx
		fxs = append(fxs,
			&vm.Fx{
				ID: nftfx.ID,
				Fx: &nftfx.Fx{},
			},
			&vm.Fx{
				ID: propertyfx.ID,
				Fx: &propertyfx.Fx{},
			},
		)
	} else {
		// Additional Fxs specified - append them after secp256k1fx
		fxs = append(fxs, config.additionalFxs...)
	}

	// Create config for VM with optional indexing
	vmConfig := DefaultConfig
	if config.indexTransactions {
		vmConfig.IndexTransactions = true
	}
	configBytes, err := json.Marshal(vmConfig)
	require.NoError(err)

	toEngine := make(chan vm.Message, 1)
	require.NoError(vmImpl.Initialize(
		context.Background(),
		vm.Init{
			Runtime: rt,
			DB:      baseDB,
			Genesis: genesisBytes,
			Upgrade: nil,
			Config:  configBytes,
			ToEngine: toEngine,
			Fx:      fxs,
			Sender:  appSender,
		},
	))

	// Get the genesis transaction
	genesisTx := getCreateTxFromGenesisTest(t.(*testing.T), genesisBytes, "LUX")

	// Create transaction builder with SharedMemory
	atomicMemForBuilder := sharedMemory.NewSharedMemory(rt.ChainID)
	txBuilder := txstest.New(
		vmImpl.parser.Codec(),
		context.Background(),
		&vmImpl.Config,
		vmImpl.feeAssetID,
		vmImpl.state,
		atomicMemForBuilder,
	)

	// Set the context IDs from the Runtime
	txBuilder.SetContextIDs(rt.NetworkID, rt.ChainID)

	env := &testEnv{
		vm:           vmImpl,
		consensusRuntime: rt,
		genesisBytes: genesisBytes,
		genesisTx:    genesisTx,
		testLock:     testLock,
		txBuilder:    txBuilder,
		sharedMemory: sharedMemory,
	}

	// Register cleanup to prevent goroutine leaks
	// This ensures PushGossip and PullGossip goroutines are properly terminated
	t.Cleanup(func() {
		// Shutdown the VM to cancel onShutdownCtx and stop gossip goroutines
		_ = vmImpl.Shutdown()
	})

	// Linearize the DAG to initialize the network
	// This simulates what happens during normal VM bootstrap
	if !config.notLinearized {
		// Use the genesis transaction ID as the stop vertex
		stopVertexID := genesisTx.ID()
		toEngineChan := make(chan vm.Message, 1)
		require.NoError(vmImpl.Linearize(context.Background(), stopVertexID, toEngineChan))

		// Mark the backend as bootstrapped so tests can issue transactions
		vmImpl.txBackend.Bootstrapped = true
	}

	// Lock the VM so tests can unlock it when ready
	vmImpl.Lock.Lock()

	return env
}

// issueAndAccept issues and accepts a transaction
func issueAndAccept(require *require.Assertions, vm *VM, tx *txs.Tx) {
	// Issue the transaction to the network
	require.NoError(vm.network.IssueTxFromRPC(tx))

	// Build a block containing the transaction
	blkIntf, err := vm.BuildBlock(context.Background())
	require.NoError(err)

	// Verify the block
	require.NoError(blkIntf.Verify(context.Background()))

	// Accept the block
	require.NoError(blkIntf.Accept(context.Background()))

	// Update the preferred block (normally done by consensus engine)
	require.NoError(vm.SetPreference(context.Background(), blkIntf.ID()))

	// Commit the versiondb so indexed data is visible
	require.NoError(vm.db.Commit())

	// Verify the block status is accepted
	require.Equal(uint8(choices.Accepted), uint8(blkIntf.Status()))
}

// newTx creates a simple test transaction
func newTx(tb testing.TB, genesisBytes []byte, chainID ids.ID, parser txs.Parser, assetName string) *txs.Tx {
	require := require.New(tb)

	createTx := getCreateTxFromGenesisTest(tb.(*testing.T), genesisBytes, assetName)
	// Genesis creates 1000 LUX for keys[0]
	// This tx spends the entire UTXO and creates a change output back to keys[0]
	// Must account for transaction fee (testTxFee = 1000 nanoLux)
	inputAmt := uint64(1000 * constants.Lux)
	outputAmt := inputAmt - testTxFee // Deduct fee from output

	tx := &txs.Tx{Unsigned: &txs.BaseTx{
		BaseTx: lux.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Ins: []*lux.TransferableInput{{
				UTXOID: lux.UTXOID{
					TxID:        createTx.ID(),
					OutputIndex: 0, // First output (fixed cap holder output)
				},
				Asset: lux.Asset{ID: createTx.ID()},
				In: &secp256k1fx.TransferInput{
					Amt: inputAmt, // Must match UTXO amount
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
					},
				},
			}},
			Outs: []*lux.TransferableOutput{{
				Asset: lux.Asset{ID: createTx.ID()},
				Out: &secp256k1fx.TransferOutput{
					Amt: outputAmt, // Output amount after fee deduction
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
					},
				},
			}},
		},
	}}
	require.NoError(
		tx.SignSECP256K1Fx(parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}}),
	)
	return tx
}
