// Copyright (C) 2019-2025, Lux Industries Inc All rights reserved.
// See the file LICENSE for licensing terms.

package exchangevm

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	consensusctx "github.com/luxfi/consensus/context"
	"github.com/luxfi/consensus/core/choices"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/utils/units"
	"github.com/luxfi/node/upgrade"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/vms/exchangevm/txs"
	"github.com/luxfi/node/vms/exchangevm/txs/txstest"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/secp256k1fx"
)

// Test keys for use in tests
var keys = secp256k1.TestKeys()

// durango is a shorthand for the Durango fork config
var durango = upgradetest.GetConfig(upgradetest.Durango)

// Test constants
const (
	startBalance uint64 = 50000
	testTxFee    uint64 = 1000
)

var assetID = ids.GenerateTestID()

// envConfig configures the test environment
type envConfig struct {
	fork          upgrade.Config
	notLinearized bool
	additionalFxs []interface{}
}

// testEnv is the test environment
type testEnv struct {
	vm            *VM
	consensusCtx  *consensusctx.Context
	genesisBytes  []byte
	genesisTx     *txs.Tx
	testLock      *sync.Mutex
	txBuilder     *txstest.Builder
	sharedMemory  *atomic.Memory
}

// newGenesisBytesTest creates test genesis bytes
func newGenesisBytesTest(t *testing.T) []byte {
	require := require.New(t)

	// Create a simple genesis with one asset (LUX)
	genesisData := map[string]GenesisAssetDefinition{
		"LUX": {
			Name:         "Lux",
			Symbol:       "LUX",
			Denomination: 9,
			InitialState: AssetInitialState{
				FixedCap: []GenesisHolder{
					{
						Amount:  100 * units.Lux,
						Address: keys[0].PublicKey().Address().String(),
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

	ctx := &consensusctx.Context{
		NetworkID: constants.UnitTestID,
		ChainID:   ids.GenerateTestID(),
		XChainID:  ids.GenerateTestID(),
		CChainID:  ids.GenerateTestID(),
	}

	baseDB := memdb.New()
	sharedMemory := atomic.NewMemory(memdb.New())

	vm := &VM{}
	genesisBytes := newGenesisBytesTest(t.(*testing.T))
	// Create shared memory wrapper that matches VM's interface
	atomicMem := sharedMemory.NewSharedMemory(ctx.ChainID)
	vm.SharedMemory = &testSharedMemory{mem: atomicMem}

	testLock := &sync.Mutex{}
	testLock.Lock()

	// Create a mock AppSender
	appSender := &noOpAppSender{}

	toEngine := make(chan interface{}, 1)
	err := vm.Initialize(
		context.Background(),
		ctx,
		baseDB,
		genesisBytes,
		nil,
		nil,
		toEngine,
		config.additionalFxs,
		appSender,
	)
	require.NoError(err)

	// Get the genesis transaction
	genesisTx := getCreateTxFromGenesisTest(t.(*testing.T), genesisBytes, "LUX")

	// Create transaction builder
	txBuilder := txstest.New(
		vm.parser.Codec(),
		context.Background(),
		&vm.Config,
		vm.feeAssetID,
		vm.state,
		nil, // SharedMemory not needed for basic tests
	)

	env := &testEnv{
		vm:            vm,
		consensusCtx:  ctx,
		genesisBytes:  genesisBytes,
		genesisTx:     genesisTx,
		testLock:      testLock,
		txBuilder:     txBuilder,
		sharedMemory:  sharedMemory,
	}

	return env
}

// issueAndAccept issues and accepts a transaction
func issueAndAccept(require *require.Assertions, vm *VM, tx *txs.Tx) {
	// Issue the transaction to the network
	err := vm.network.IssueTxFromRPC(tx)
	require.NoError(err)

	// Build a block containing the transaction
	blkIntf, err := vm.BuildBlock(context.Background())
	require.NoError(err)

	// Verify the block
	require.NoError(blkIntf.Verify(context.Background()))

	// Accept the block
	require.NoError(blkIntf.Accept(context.Background()))

	// Verify the block status is accepted
	require.Equal(choices.Accepted, blkIntf.Status())
}



// newTx creates a simple test transaction
func newTx(tb testing.TB, genesisBytes []byte, chainID ids.ID, parser txs.Parser, assetName string) *txs.Tx {
	require := require.New(tb)

	createTx := getCreateTxFromGenesisTest(tb.(*testing.T), genesisBytes, assetName)
	tx := &txs.Tx{Unsigned: &txs.BaseTx{
		BaseTx: lux.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Ins: []*lux.TransferableInput{{
				UTXOID: lux.UTXOID{
					TxID:        createTx.ID(),
					OutputIndex: 2,
				},
				Asset: lux.Asset{ID: createTx.ID()},
				In: &secp256k1fx.TransferInput{
					Amt: startBalance,
					Input: secp256k1fx.Input{
						SigIndices: []uint32{0},
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
