<<<<<<< HEAD:vms/avm/environment_test.go
// Copyright (C) 2019-2024, Lux Industries, Inc. All rights reserved.
=======
// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/environment_test.go
// See the file LICENSE for licensing terms.

package xvm

import (
	"context"
	"encoding/json"
<<<<<<< HEAD:vms/avm/environment_test.go
=======
	"math/rand"
	"sync"
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/environment_test.go
	"testing"

	"github.com/stretchr/testify/require"

<<<<<<< HEAD:vms/avm/environment_test.go
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/database/memdb"
	"github.com/luxfi/node/database/prefixdb"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/snow"
	"github.com/luxfi/node/snow/engine/common"
	"github.com/luxfi/node/snow/engine/enginetest"
	"github.com/luxfi/node/snow/snowtest"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/secp256k1"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/vms/avm/block/executor"
	"github.com/luxfi/node/vms/avm/config"
	"github.com/luxfi/node/vms/avm/fxs"
	"github.com/luxfi/node/vms/avm/txs"
	"github.com/luxfi/node/vms/avm/txs/txstest"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/nftfx"
	"github.com/luxfi/node/vms/secp256k1fx"
)

=======
	"github.com/luxfi/consensus"
	consensusctx "github.com/luxfi/consensus/context"
	"github.com/luxfi/consensus/core"
	"github.com/luxfi/consensus/core/appsender"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/formatting"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/utils/sampler"
	"github.com/luxfi/node/utils/timer/mockable"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/nftfx"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/vms/xvm/block/executor"
	"github.com/luxfi/node/vms/xvm/config"
	"github.com/luxfi/node/vms/xvm/fxs"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/node/vms/xvm/txs/txstest"

	avajson "github.com/luxfi/node/utils/json"
)

// testValidatorState is a mock implementation of consensus ValidatorState for tests
type testValidatorState struct{}

func (tvs *testValidatorState) GetCurrentHeight() (uint64, error) {
	return 100, nil
}

func (tvs *testValidatorState) GetValidatorSet(height uint64, netID ids.ID) (map[ids.NodeID]uint64, error) {
	// Return a simple validator set for testing
	return map[ids.NodeID]uint64{
		ids.GenerateTestNodeID(): 1000,
	}, nil
}

func (tvs *testValidatorState) GetMinimumHeight(ctx context.Context) (uint64, error) {
	return 0, nil
}

func (tvs *testValidatorState) GetNetID(chainID ids.ID) (ids.ID, error) {
	return ids.GenerateTestID(), nil
}

func (tvs *testValidatorState) GetChainID(chainID ids.ID) (ids.ID, error) {
	return chainID, nil
}

func (tvs *testValidatorState) GetSubnetID(chainID ids.ID) (ids.ID, error) {
	return ids.GenerateTestID(), nil
}

type fork uint8

>>>>>>> origin/regenesis-runtime-replay:vms/xvm/environment_test.go
const (
	testTxFee    uint64 = 1000
	startBalance uint64 = 50000

	feeAssetName   = "TEST"
	otherAssetName = "OTHER"
)

var (
<<<<<<< HEAD:vms/avm/environment_test.go
=======
	testChangeAddr = ids.GenerateTestShortID()
	testCases      = []struct {
		name     string
		luxAsset bool
	}{
		{
			name:     "genesis asset is LUX",
			luxAsset: true,
		},
		{
			name:     "genesis asset is TEST",
			luxAsset: false,
		},
	}

>>>>>>> origin/regenesis-runtime-replay:vms/xvm/environment_test.go
	assetID = ids.ID{1, 2, 3}

	keys  = secp256k1.TestKeys()[:3] // Implementation note
	addrs []ids.ShortID              // addrs[i] corresponds to keys[i]
)

func init() {
	addrs = make([]ids.ShortID, len(keys))
	for i, key := range keys {
		addrs[i] = key.Address()
	}
}

type envConfig struct {
	fork             upgradetest.Fork
	isCustomFeeAsset bool
	vmStaticConfig   *config.Config
	vmDynamicConfig  *Config
	additionalFxs    []*core.Fx
	notLinearized    bool
	notBootstrapped  bool
}

type environment struct {
	genesisBytes []byte
	genesisTx    *txs.Tx
	sharedMemory *atomic.Memory
	vm           *VM
	txBuilder    *txstest.Builder
	consensusCtx *consensusctx.Context // Store the consensus context
	testLock     *sync.RWMutex         // Lock for test synchronization
}

// setup the testing environment
func setup(tb testing.TB, c *envConfig) *environment {
	require := require.New(tb)

	var (
		networkID   = uint32(0)
		genesisData map[string]AssetDefinition
		assetName   = "LUX"
	)
	if c.isCustomFeeAsset {
		genesisData = makeCustomAssetGenesisData(tb)
		assetName = feeAssetName
	} else {
		genesisData = makeDefaultGenesisData(tb)
	}

	genesis, err := NewGenesis(networkID, genesisData)
	require.NoError(err)
	genesisBytes, err := genesis.Bytes()
	require.NoError(err)

	xChainID := ids.GenerateTestID()

	baseDB := memdb.New()
	m := atomic.NewMemory(prefixdb.New([]byte{0}, baseDB))

	// Create a mock validator state for testing
	mockValidatorState := &testValidatorState{}

	// Create a proper consensus context for the tests
	consensusCtx := &consensusctx.Context{
		QuantumID:      10001,
		NetID:          ids.GenerateTestID(),
		ChainID:        xChainID,
		NodeID:         ids.GenerateTestNodeID(),
		XChainID:       xChainID,
		CChainID:       ids.GenerateTestID(),
		XAssetID:    ids.GenerateTestID(),
		XAssetID:     ids.GenerateTestID(),
		ValidatorState: mockValidatorState,
	}

	// Create a separate lock for synchronization in tests
	testLock := &sync.RWMutex{}
	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	testLock.Lock()

<<<<<<< HEAD:vms/avm/environment_test.go
	vmStaticConfig := config.Config{
		Upgrades:         upgradetest.GetConfig(c.fork),
		TxFee:            testTxFee,
		CreateAssetTxFee: testTxFee,
	}
=======
	// Create a context with validator state for the VM
	ctx := consensus.WithValidatorState(context.Background(), mockValidatorState)

	vmStaticConfig := staticConfig(tb, c.fork)
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/environment_test.go
	if c.vmStaticConfig != nil {
		vmStaticConfig = *c.vmStaticConfig
	}

	vm := &VM{
		Config: vmStaticConfig,
	}

	vmDynamicConfig := DefaultConfig
	if c.vmDynamicConfig != nil {
		vmDynamicConfig = *c.vmDynamicConfig
	}
	configBytes, err := json.Marshal(vmDynamicConfig)
	require.NoError(err)

	// Create engine channel
	toEngine := make(chan interface{}, 1)

	// Convert FXs to []interface{}
	fxList := make([]interface{}, 0, 2+len(c.additionalFxs))
	fxList = append(fxList, &core.Fx{
		ID: secp256k1fx.ID,
		Fx: &secp256k1fx.Fx{},
	})
	fxList = append(fxList, &core.Fx{
		ID: nftfx.ID,
		Fx: &nftfx.Fx{},
	})
	for _, fx := range c.additionalFxs {
		fxList = append(fxList, fx)
	}

	require.NoError(vm.Initialize(
<<<<<<< HEAD:vms/avm/environment_test.go
		context.Background(),
		ctx,
		prefixdb.New([]byte{1}, baseDB),
		genesisBytes,
		configBytes,
		nil,
		append(
			[]*common.Fx{
				{
					ID: secp256k1fx.ID,
					Fx: &secp256k1fx.Fx{},
				},
				{
					ID: nftfx.ID,
					Fx: &nftfx.Fx{},
				},
			},
			c.additionalFxs...,
		),
		&enginetest.Sender{},
=======
		ctx,                             // context.Context
		consensusCtx,                    // chainCtx interface{}
		prefixdb.New([]byte{1}, baseDB), // dbManager interface{}
		genesisBytes,                    // genesisBytes []byte
		nil,                             // upgradeBytes []byte
		configBytes,                     // configBytes []byte
		toEngine,                        // toEngine chan<- interface{}
		fxList,                          // fxs []interface{}
		&appsender.FakeSender{},         // appSender interface{}
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/environment_test.go
	))

	stopVertexID := ids.GenerateTestID()

	env := &environment{
		genesisBytes: genesisBytes,
		genesisTx:    getCreateTxFromGenesisTest(tb, genesisBytes, assetName),
		sharedMemory: m,
		vm:           vm,
		txBuilder:    txstest.New(vm.parser.Codec(), ctx, &vm.Config, vm.feeAssetID, vm.state, m.NewSharedMemory(consensusCtx.ChainID)),
		consensusCtx: consensusCtx,
		testLock:     testLock,
	}

	require.NoError(vm.SetState(context.Background(), 0)) // 0 = Bootstrapping
	if c.notLinearized {
		return env
	}

	require.NoError(vm.Linearize(context.Background(), stopVertexID))
	if c.notBootstrapped {
		return env
	}

	require.NoError(vm.SetState(context.Background(), 1)) // 1 = NormalOp

	tb.Cleanup(func() {
		env.testLock.Lock()
		defer env.testLock.Unlock()

		env.vm.Shutdown()
	})

	return env
}

<<<<<<< HEAD:vms/avm/environment_test.go
=======
func staticConfig(tb testing.TB, f fork) config.Config {
	c := config.Config{
		TxFee:            testTxFee,
		CreateAssetTxFee: testTxFee,
		EtnaTime:         mockable.MaxTime,
	}

	switch f {
	case eUpgrade:
		c.EtnaTime = time.Time{}
	case durango:
	default:
		require.FailNow(tb, "unhandled fork", f)
	}

	return c
}

>>>>>>> origin/regenesis-runtime-replay:vms/xvm/environment_test.go
// Returns:
//
//  1. tx in genesis that creates asset
//  2. the index of the output
func getCreateTxFromGenesisTest(tb testing.TB, genesisBytes []byte, assetName string) *txs.Tx {
	require := require.New(tb)

	parser, err := txs.NewParser(
		[]fxs.Fx{
			&secp256k1fx.Fx{},
		},
	)
	require.NoError(err)

	cm := parser.GenesisCodec()
	genesis := Genesis{}
	_, err = cm.Unmarshal(genesisBytes, &genesis)
	require.NoError(err)
	require.NotEmpty(genesis.Txs)

	var assetTx *GenesisAsset
	for _, tx := range genesis.Txs {
		if tx.Name == assetName {
			assetTx = tx
			break
		}
	}
	require.NotNil(assetTx)

	tx := &txs.Tx{
		Unsigned: &assetTx.CreateAssetTx,
	}
	require.NoError(tx.Initialize(parser.GenesisCodec()))
	return tx
}

func newGenesisBytesTest(tb testing.TB) []byte {
	require := require.New(tb)
	genesisData := makeDefaultGenesisData(tb)
	genesis, err := NewGenesis(
		constants.UnitTestID,
		genesisData,
	)
	require.NoError(err)
	b, err := genesis.Bytes()
	require.NoError(err)
	require.NotEmpty(b)
	return b
}

func newTx(tb testing.TB, genesisBytes []byte, chainID ids.ID, parser txs.Parser, assetName string) *txs.Tx {
	require := require.New(tb)

	createTx := getCreateTxFromGenesisTest(tb, genesisBytes, assetName)
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
						SigIndices: []uint32{
							0,
						},
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

func makeDefaultGenesisData(tb testing.TB) map[string]AssetDefinition {
	require := require.New(tb)

	addr0Str, err := address.FormatBech32(constants.UnitTestHRP, addrs[0].Bytes())
	require.NoError(err)

	addr1Str, err := address.FormatBech32(constants.UnitTestHRP, addrs[1].Bytes())
	require.NoError(err)

	addr2Str, err := address.FormatBech32(constants.UnitTestHRP, addrs[2].Bytes())
	require.NoError(err)

	return map[string]AssetDefinition{
		"asset1": {
			Name:   "LUX",
			Symbol: "SYMB",
			InitialState: AssetInitialState{
				FixedCap: []Holder{
					{
						Amount:  startBalance,
						Address: addr0Str,
					},
					{
						Amount:  startBalance,
						Address: addr1Str,
					},
					{
						Amount:  startBalance,
						Address: addr2Str,
					},
				},
			},
		},
		"asset2": {
			Name:   "myVarCapAsset",
			Symbol: "MVCA",
			InitialState: AssetInitialState{
				VariableCap: []Owners{
					{
						Threshold: 1,
						Minters: []string{
							addr0Str,
							addr1Str,
						},
					},
					{
						Threshold: 2,
						Minters: []string{
							addr0Str,
							addr1Str,
							addr2Str,
						},
					},
				},
			},
		},
		"asset3": {
			Name: "myOtherVarCapAsset",
			InitialState: AssetInitialState{
				VariableCap: []Owners{
					{
						Threshold: 1,
						Minters: []string{
							addr0Str,
						},
					},
				},
			},
		},
		"asset4": {
			Name: "myFixedCapAsset",
			InitialState: AssetInitialState{
				FixedCap: []Holder{
					{
						Amount:  startBalance,
						Address: addr0Str,
					},
					{
						Amount:  startBalance,
						Address: addr1Str,
					},
				},
			},
		},
	}
}

func makeCustomAssetGenesisData(tb testing.TB) map[string]AssetDefinition {
	require := require.New(tb)

	addr0Str, err := address.FormatBech32(constants.UnitTestHRP, addrs[0].Bytes())
	require.NoError(err)

	addr1Str, err := address.FormatBech32(constants.UnitTestHRP, addrs[1].Bytes())
	require.NoError(err)

	addr2Str, err := address.FormatBech32(constants.UnitTestHRP, addrs[2].Bytes())
	require.NoError(err)

	return map[string]AssetDefinition{
		"asset1": {
			Name:   feeAssetName,
			Symbol: "TST",
			InitialState: AssetInitialState{
				FixedCap: []Holder{
					{
						Amount:  startBalance,
						Address: addr0Str,
					},
					{
						Amount:  startBalance,
						Address: addr1Str,
					},
					{
						Amount:  startBalance,
						Address: addr2Str,
					},
				},
			},
		},
		"asset2": {
			Name:   otherAssetName,
			Symbol: "OTH",
			InitialState: AssetInitialState{
				FixedCap: []Holder{
					{
						Amount:  startBalance,
						Address: addr0Str,
					},
					{
						Amount:  startBalance,
						Address: addr1Str,
					},
					{
						Amount:  startBalance,
						Address: addr2Str,
					},
				},
			},
		},
	}
}

// issueAndAccept expects the context lock not to be held
func issueAndAccept(
	require *require.Assertions,
	vm *VM,
	tx *txs.Tx,
) {
	txID, err := vm.issueTxFromRPC(tx)
	require.NoError(err)
	require.Equal(tx.ID(), txID)

	buildAndAccept(require, vm, txID)
}

// buildAndAccept expects the context lock not to be held
func buildAndAccept(
	require *require.Assertions,
	vm *VM,
	txID ids.ID,
) {
<<<<<<< HEAD:vms/avm/environment_test.go
	msg, err := vm.WaitForEvent(context.Background())
	require.NoError(err)
	require.Equal(common.PendingTxs, msg)
=======
	// Wait for the VM to signal that there are pending transactions
	msg, err := vm.WaitForEvent(context.Background())
	require.NoError(err)
	require.Equal(core.PendingTxs, msg)
>>>>>>> origin/regenesis-runtime-replay:vms/xvm/environment_test.go

	// Note: In tests, we don't need to lock since we're running synchronously

	blkIntf, err := vm.BuildBlock(context.Background())
	require.NoError(err)
	require.IsType(&executor.Block{}, blkIntf)

	blk := blkIntf.(*executor.Block)
	txs := blk.Txs()
	require.Len(txs, 1)

	issuedTx := txs[0]
	require.Equal(txID, issuedTx.ID())
	require.NoError(blk.Verify(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), blk.ID()))
	require.NoError(blk.Accept(context.Background()))
}
