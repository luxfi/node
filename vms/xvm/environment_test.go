// Copyright (C) 2020-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/database/memdb"
	"github.com/luxfi/database/prefixdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/chains/atomic"
	"github.com/luxfi/node/consensus"
	"github.com/luxfi/node/consensus/consensustest"
	"github.com/luxfi/node/consensus/engine/core"
	"github.com/luxfi/node/consensus/engine/enginetest"
	"github.com/luxfi/node/upgrade/upgradetest"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/crypto/secp256k1"
	"github.com/luxfi/node/utils/formatting/address"
	"github.com/luxfi/node/vms/components/lux"
	"github.com/luxfi/node/vms/nftfx"
	"github.com/luxfi/node/vms/secp256k1fx"
	"github.com/luxfi/node/vms/xvm/block/executor"
	"github.com/luxfi/node/vms/xvm/config"
	"github.com/luxfi/node/vms/xvm/fxs"
	"github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/node/vms/xvm/txs/txstest"
)

const (
	testTxFee    uint64 = 1000
	startBalance uint64 = 50000

	feeAssetName   = "TEST"
	otherAssetName = "OTHER"
)

var (
	assetID = ids.ID{1, 2, 3}

	keys  = secp256k1.TestKeys()[:3] // TODO: Remove [:3]
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

	ctx := consensustest.Context(tb, consensustest.XChainID)

	baseDB := memdb.New()
	m := atomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	ctx.SharedMemory = m.NewSharedMemory(ctx.ChainID)

	// NB: this lock is intentionally left locked when this function returns.
	// The caller of this function is responsible for unlocking.
	ctx.Lock.Lock()

	vmStaticConfig := config.Config{
		Upgrades:         upgradetest.GetConfig(c.fork),
		TxFee:            testTxFee,
		CreateAssetTxFee: testTxFee,
	}
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

	require.NoError(vm.Initialize(
		context.Background(),
		ctx,
		prefixdb.New([]byte{1}, baseDB),
		genesisBytes,
		configBytes,
		nil,
		append(
			[]*core.Fx{
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
	))

	stopVertexID := ids.GenerateTestID()

	env := &environment{
		genesisBytes: genesisBytes,
		genesisTx:    getCreateTxFromGenesisTest(tb, genesisBytes, assetName),
		sharedMemory: m,
		vm:           vm,
		txBuilder:    txstest.New(vm.parser.Codec(), vm.ctx, &vm.Config, vm.feeAssetID, vm.state),
	}

	require.NoError(vm.SetState(context.Background(), consensus.Bootstrapping))
	if c.notLinearized {
		return env
	}

	require.NoError(vm.Linearize(context.Background(), stopVertexID))
	if c.notBootstrapped {
		return env
	}

	require.NoError(vm.SetState(context.Background(), consensus.NormalOp))

	tb.Cleanup(func() {
		env.vm.ctx.Lock.Lock()
		defer env.vm.ctx.Lock.Unlock()

		require.NoError(env.vm.Shutdown(context.Background()))
	})

	return env
}

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
	msg, err := vm.WaitForEvent(context.Background())
	require.NoError(err)
	require.Equal(core.PendingTxs, msg)

	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()

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
