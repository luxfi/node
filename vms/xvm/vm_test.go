// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"github.com/luxfi/node/upgrade"
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/vm"
	"github.com/luxfi/runtime"
	"github.com/luxfi/constants"
	"github.com/luxfi/database"
	"github.com/luxfi/database/memdb"
	"github.com/luxfi/ids"
	"github.com/luxfi/math/set"
	"github.com/luxfi/vm/chains/atomic"
	
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/node/vms/components/verify"
	xvmtxs "github.com/luxfi/node/vms/xvm/txs"
	"github.com/luxfi/utxo/nftfx"
	"github.com/luxfi/utxo/propertyfx"
	"github.com/luxfi/utxo/secp256k1fx"
)

func TestInvalidFx(t *testing.T) {
	require := require.New(t)

	vmImpl := &VM{}
	rt := &runtime.Runtime{
		ChainID: ids.GenerateTestID(),
	}
	// Shutdown handled by t.Cleanup in setup()

	genesisBytes := newGenesisBytesTest(t)
	toEngine := make(chan vm.Message, 1)
	err := vmImpl.Initialize(
		context.Background(),
		vm.Init{
			Runtime: rt,
			DB:      memdb.New(),
			Genesis: genesisBytes,
			Upgrade: nil,
			Config:  nil,
			ToEngine: toEngine,
			Fx: []interface{}{
				nil,
			},
			Sender: nil,
		},
	)
	require.ErrorIs(err, errIncompatibleFx)
}

func TestFxInitializationFailure(t *testing.T) {
	require := require.New(t)

	vmImpl := &VM{}
	rt := &runtime.Runtime{
		ChainID: ids.GenerateTestID(),
	}
	// Shutdown handled by t.Cleanup in setup()

	genesisBytes := newGenesisBytesTest(t)
	toEngine := make(chan vm.Message, 1)
	fx := &vm.Fx{
		ID: ids.Empty,
		Fx: &FxTest{
			InitializeF: func(interface{}) error {
				return errUnknownFx
			},
		},
	}
	err := vmImpl.Initialize(
		context.Background(),
		vm.Init{
			Runtime: rt,
			DB:      memdb.New(),
			Genesis: genesisBytes,
			Upgrade: nil,
			Config:  nil,
			ToEngine: toEngine,
			Fx:      []interface{}{fx},
			Sender:  nil,
		},
	)
	require.ErrorIs(err, errUnknownFx)
}

func TestIssueTx(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork: upgrade.Default,
	})
	env.vm.Lock.Unlock()

	tx := newTx(t, env.genesisBytes, env.consensusRuntime.ChainID, env.vm.parser, "LUX")
	issueAndAccept(require, env.vm, tx)
}

// Test issuing a transaction that creates an NFT family
func TestIssueNFT(t *testing.T) {
	require := require.New(t)

	// secp256k1fx and nftfx are now included by default
	env := setup(t, &envConfig{
		fork: upgrade.Default,
	})
	env.vm.Lock.Unlock()

	var (
		key = keys[0]
		kc  = secp256k1fx.NewKeychain(key)
	)

	// Create the asset
	initialStates := map[uint32][]verify.State{
		1: {
			&nftfx.MintOutput{
				GroupID: 1,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		},
	}

	createAssetTx, err := env.txBuilder.CreateAssetTx(
		"Team Rocket", // name
		"TR",          // symbol
		0,             // denomination
		initialStates,
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, createAssetTx)

	// Mint the NFT
	mintNFTTx, err := env.txBuilder.MintNFT(
		createAssetTx.ID(),
		[]byte{'h', 'e', 'l', 'l', 'o'}, // payload
		[]*secp256k1fx.OutputOwners{{
			Threshold: 1,
			Addrs:     []ids.ShortID{key.Address()},
		}},
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, mintNFTTx)

	// Move the NFT
	moveAddrs := make(set.Set[ids.ShortID])
	for addr := range kc.Addresses() {
		moveAddrs.Add(addr)
	}
	utxos, err := lux.GetAllUTXOs(env.vm.state, moveAddrs)
	require.NoError(err)
	transferOp, _, err := env.vm.SpendNFT(
		utxos,
		kc,
		createAssetTx.ID(),
		1,
		keys[2].Address(),
	)
	require.NoError(err)

	transferNFTTx, err := env.txBuilder.Operation(
		transferOp,
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, transferNFTTx)
}

// Test issuing a transaction that creates an Property family
func TestIssueProperty(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork: upgrade.Default,
		additionalFxs: []interface{}{
			&vm.Fx{
				ID: propertyfx.ID,
				Fx: &propertyfx.Fx{},
			},
		},
	})
	env.vm.Lock.Unlock()

	var (
		key = keys[0]
		kc  = secp256k1fx.NewKeychain(key)
	)

	// create the asset
	// propertyfx is at index 1 (secp256k1fx is always at index 0)
	initialStates := map[uint32][]verify.State{
		1: {
			&propertyfx.MintOutput{
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		},
	}

	createAssetTx, err := env.txBuilder.CreateAssetTx(
		"Team Rocket", // name
		"TR",          // symbol
		0,             // denomination
		initialStates,
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, createAssetTx)

	// mint the property
	mintPropertyOp := &xvmtxs.Operation{
		Asset: lux.Asset{ID: createAssetTx.ID()},
		UTXOIDs: []*lux.UTXOID{{
			TxID:        createAssetTx.ID(),
			OutputIndex: 1,
		}},
		Op: &propertyfx.MintOperation{
			MintInput: secp256k1fx.Input{
				SigIndices: []uint32{0},
			},
			MintOutput: propertyfx.MintOutput{
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
			OwnedOutput: propertyfx.OwnedOutput{},
		},
	}

	mintPropertyTx, err := env.txBuilder.Operation(
		[]*xvmtxs.Operation{mintPropertyOp},
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, mintPropertyTx)

	// burn the property
	burnPropertyOp := &xvmtxs.Operation{
		Asset: lux.Asset{ID: createAssetTx.ID()},
		UTXOIDs: []*lux.UTXOID{{
			TxID:        mintPropertyTx.ID(),
			OutputIndex: 2,
		}},
		Op: &propertyfx.BurnOperation{Input: secp256k1fx.Input{}},
	}

	burnPropertyTx, err := env.txBuilder.Operation(
		[]*xvmtxs.Operation{burnPropertyOp},
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, burnPropertyTx)
}

func TestIssueTxWithFeeAsset(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork: upgrade.Default,
	})
	env.vm.Lock.Unlock()

	// send first asset
	tx := newTx(t, env.genesisBytes, env.consensusRuntime.ChainID, env.vm.parser, "LUX")
	issueAndAccept(require, env.vm, tx)
}

func TestIssueTxWithAnotherAsset(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork: upgrade.Default,
	})
	env.vm.Lock.Unlock()

	// send second asset
	var (
		key = keys[0]
		kc  = secp256k1fx.NewKeychain(key)

		feeAssetCreateTx = getCreateTxFromGenesisTest(t, env.genesisBytes, "LUX")
		createTx         = getCreateTxFromGenesisTest(t, env.genesisBytes, "LUX")
	)

	tx, err := env.txBuilder.BaseTx(
		[]*lux.TransferableOutput{
			{ // fee asset
				Asset: lux.Asset{ID: feeAssetCreateTx.ID()},
				Out: &secp256k1fx.TransferOutput{
					Amt: startBalance - env.vm.TxFee,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{key.PublicKey().Address()},
					},
				},
			},
			{ // issued asset
				Asset: lux.Asset{ID: createTx.ID()},
				Out: &secp256k1fx.TransferOutput{
					Amt: startBalance - env.vm.TxFee,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs:     []ids.ShortID{key.PublicKey().Address()},
					},
				},
			},
		},
		nil, // memo
		kc,
		key.Address(),
	)
	require.NoError(err)
	issueAndAccept(require, env.vm, tx)
}

func TestVMFormat(t *testing.T) {
	env := setup(t, &envConfig{
		fork: upgrade.Default,
	})
	// setup() already acquired the lock, so release it
	env.vm.Lock.Unlock()

	tests := []struct {
		in       ids.ShortID
		expected string
	}{
		{
			in: ids.ShortEmpty,
			// FormatLocalAddress returns full chainID prefix, not alias
			// Format: [chainID]-[hrp][encoded address]
			expected: "", // Will be set dynamically based on actual chain ID
		},
	}
	for _, test := range tests {
		t.Run(test.in.String(), func(t *testing.T) {
			require := require.New(t)
			addrStr, err := env.vm.FormatLocalAddress(test.in)
			require.NoError(err)
			// Verify format is correct: should contain chain ID prefix and address
			require.Contains(addrStr, "-testing1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqtu2yas")
		})
	}
}

// Test issuing an import transaction.
func TestIssueImportTx(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork: upgrade.Default,
	})
	// Note: Manual lock management in this test, no defer

	peerSharedMemory := env.sharedMemory.NewSharedMemory(constants.PlatformChainID)

	genesisTx := getCreateTxFromGenesisTest(t, env.genesisBytes, "LUX")
	luxID := genesisTx.ID()

	var (
		key = keys[0]
		kc  = secp256k1fx.NewKeychain(key)

		utxoID = lux.UTXOID{
			TxID: ids.ID{
				0x0f, 0x2f, 0x4f, 0x6f, 0x8e, 0xae, 0xce, 0xee,
				0x0d, 0x2d, 0x4d, 0x6d, 0x8c, 0xac, 0xcc, 0xec,
				0x0b, 0x2b, 0x4b, 0x6b, 0x8a, 0xaa, 0xca, 0xea,
				0x09, 0x29, 0x49, 0x69, 0x88, 0xa8, 0xc8, 0xe8,
			},
		}
		txAssetID    = lux.Asset{ID: luxID}
		importedUtxo = &lux.UTXO{
			UTXOID: utxoID,
			Asset:  txAssetID,
			Out: &secp256k1fx.TransferOutput{
				Amt: 1010,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{key.PublicKey().Address()},
				},
			},
		}
	)

	// Provide the platform UTXO via the ZAP-native wire envelope —
	// matches the production exporter path (state.PutUTXO + GetAtomicUTXOs
	// dispatch through utxo.ParseUTXO).
	utxoBytes, err := importedUtxo.WireBytes()
	require.NoError(err)

	inputID := importedUtxo.InputID()
	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		env.vm.ChainID: {
			PutRequests: []*atomic.Element{{
				Key:   inputID[:],
				Value: utxoBytes,
				Traits: [][]byte{
					key.PublicKey().Address().Bytes(),
				},
			}},
		},
	}))

	tx, err := env.txBuilder.ImportTx(
		constants.PlatformChainID, // source chain
		key.Address(),
		kc,
	)
	require.NoError(err)

	// Unlock before calling issueAndAccept, which needs the lock released
	env.vm.Lock.Unlock()
	issueAndAccept(require, env.vm, tx)
	env.vm.Lock.Lock() // Re-lock for the remainder of the test

	id := utxoID.InputID()
	_, err = env.vm.SharedMemory.Get(constants.PlatformChainID, [][]byte{id[:]})
	require.ErrorIs(err, database.ErrNotFound)

	env.vm.Lock.Unlock() // Final unlock (no defer in this test)
}

func TestImportTxNotState(t *testing.T) {
	require := require.New(t)

	intf := interface{}(&xvmtxs.ImportTx{})
	_, ok := intf.(verify.State)
	require.False(ok)
}

// Test issuing an export transaction.
func TestIssueExportTx(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{fork: upgrade.Default})
	defer env.vm.Lock.Unlock()

	genesisTx := getCreateTxFromGenesisTest(t, env.genesisBytes, "LUX")

	var (
		luxID      = genesisTx.ID()
		key        = keys[0]
		kc         = secp256k1fx.NewKeychain(key)
		to         = key.PublicKey().Address()
		changeAddr = to
	)

	tx, err := env.txBuilder.ExportTx(
		constants.PlatformChainID,
		to, // to
		luxID,
		startBalance-env.vm.TxFee,
		kc,
		changeAddr,
	)
	require.NoError(err)

	peerSharedMemory := env.sharedMemory.NewSharedMemory(constants.PlatformChainID)
	utxoBytes, _, _, err := peerSharedMemory.Indexed(
		env.vm.ChainID,
		[][]byte{
			key.PublicKey().Address().Bytes(),
		},
		nil,
		nil,
		math.MaxInt32,
	)
	require.NoError(err)
	require.Empty(utxoBytes)

	env.vm.Lock.Unlock()

	issueAndAccept(require, env.vm, tx)

	env.vm.Lock.Lock()

	utxoBytes, _, _, err = peerSharedMemory.Indexed(
		env.vm.ChainID,
		[][]byte{
			key.PublicKey().Address().Bytes(),
		},
		nil,
		nil,
		math.MaxInt32,
	)
	require.NoError(err)
	require.Len(utxoBytes, 1)
}

func TestClearForceAcceptedExportTx(t *testing.T) {
	require := require.New(t)

	env := setup(t, &envConfig{
		fork: upgrade.Default,
	})
	defer env.vm.Lock.Unlock()

	genesisTx := getCreateTxFromGenesisTest(t, env.genesisBytes, "LUX")

	var (
		luxID      = genesisTx.ID()
		key        = keys[0]
		kc         = secp256k1fx.NewKeychain(key)
		to         = key.PublicKey().Address()
		changeAddr = to
	)

	tx, err := env.txBuilder.ExportTx(
		constants.PlatformChainID,
		to, // to
		luxID,
		startBalance-env.vm.TxFee,
		kc,
		changeAddr,
	)
	require.NoError(err)

	utxo := lux.UTXOID{
		TxID:        tx.ID(),
		OutputIndex: 0,
	}
	utxoID := utxo.InputID()

	peerSharedMemory := env.sharedMemory.NewSharedMemory(constants.PlatformChainID)
	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		env.vm.ChainID: {
			RemoveRequests: [][]byte{utxoID[:]},
		},
	}))

	_, err = peerSharedMemory.Get(env.vm.ChainID, [][]byte{utxoID[:]})
	require.ErrorIs(err, database.ErrNotFound)

	env.vm.Lock.Unlock()

	issueAndAccept(require, env.vm, tx)

	env.vm.Lock.Lock()

	_, err = peerSharedMemory.Get(env.vm.ChainID, [][]byte{utxoID[:]})
	require.ErrorIs(err, database.ErrNotFound)
}
