// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package xvm

import (
	"fmt"
	"github.com/luxfi/node/upgrade"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/vm"

	"github.com/luxfi/node/vms/xvm/txs"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

func TestSetsAndGets(t *testing.T) {
	require := require.New(t)

	// secp256k1fx is now included by default, so we only need the custom Fx
	env := setup(t, &envConfig{
		fork: upgrade.Default,
		additionalFxs: []interface{}{
			&vm.Fx{
				ID: ids.GenerateTestID(),
				Fx: &FxTest{
					InitializeF: func(vmIntf interface{}) error {
						// The VM passed here is actually a txs.fxVM which implements secp256k1fx.VM.
						// Post-ZAP, fx Initialize is a no-op (no codec registration chain).
						if _, ok := vmIntf.(secp256k1fx.VM); !ok {
							return fmt.Errorf("unexpected VM type: %T", vmIntf)
						}
						return nil
					},
				},
			},
		},
	})
	defer env.testLock.Unlock()

	utxo := &lux.UTXO{
		UTXOID: lux.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: lux.Asset{ID: ids.Empty},
		Out:   &lux.TestState{},
	}
	utxoID := utxo.InputID()

	tx := &txs.Tx{Unsigned: &txs.BaseTx{BaseTx: lux.BaseTx{
		NetworkID:    constants.UnitTestID,
		BlockchainID: env.consensusRuntime.XChainID,
		Ins: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: 0,
			},
			Asset: lux.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt: 20 * constants.KiloLux,
				Input: secp256k1fx.Input{
					SigIndices: []uint32{
						0,
					},
				},
			},
		}},
	}}}
	require.NoError(tx.SignSECP256K1Fx(env.vm.parser.Codec(), [][]*secp256k1.PrivateKey{{keys[0]}}))

	txID := tx.ID()

	env.vm.state.AddUTXO(utxo)
	env.vm.state.AddTx(tx)

	resultUTXO, err := env.vm.state.GetUTXO(utxoID)
	require.NoError(err)
	resultTx, err := env.vm.state.GetTx(txID)
	require.NoError(err)

	require.Equal(uint32(1), resultUTXO.OutputIndex)
	require.Equal(tx.ID(), resultTx.ID())
}

func TestFundingNoAddresses(t *testing.T) {
	// secp256k1fx is now included by default, so we only need the custom Fx
	env := setup(t, &envConfig{
		fork: upgrade.Default,
		additionalFxs: []interface{}{
			&vm.Fx{
				ID: ids.GenerateTestID(),
				Fx: &FxTest{
					InitializeF: func(vmIntf interface{}) error {
						// The VM passed here is actually a txs.fxVM which implements secp256k1fx.VM.
						// Post-ZAP, fx Initialize is a no-op (no codec registration chain).
						if _, ok := vmIntf.(secp256k1fx.VM); !ok {
							return fmt.Errorf("unexpected VM type: %T", vmIntf)
						}
						return nil
					},
				},
			},
		},
	})
	defer env.testLock.Unlock()

	utxo := &lux.UTXO{
		UTXOID: lux.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: lux.Asset{ID: ids.Empty},
		Out:   &lux.TestState{},
	}

	env.vm.state.AddUTXO(utxo)
	env.vm.state.DeleteUTXO(utxo.InputID())
}

func TestFundingAddresses(t *testing.T) {
	require := require.New(t)

	// secp256k1fx is now included by default, so we only need the custom Fx
	env := setup(t, &envConfig{
		fork: upgrade.Default,
		additionalFxs: []interface{}{
			&vm.Fx{
				ID: ids.GenerateTestID(),
				Fx: &FxTest{
					InitializeF: func(vmIntf interface{}) error {
						// Post-ZAP, fx Initialize is a no-op (no codec registration chain).
						_ = vmIntf.(secp256k1fx.VM)
						return nil
					},
				},
			},
		},
	})
	defer env.testLock.Unlock()

	addr := ids.ShortID{0x01}
	utxo := &lux.UTXO{
		UTXOID: lux.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		Asset: lux.Asset{ID: ids.Empty},
		// Use a real wire-serializable fx output (utxo v0.3.7 requires the
		// UTXO.Out to be a registered fx primitive for ZAP marshaling; the
		// lux.TestAddressable mock is not wire-serializable).
		Out: &secp256k1fx.TransferOutput{
			Amt: 1,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		},
	}

	env.vm.state.AddUTXO(utxo)
	require.NoError(env.vm.state.Commit())

	utxos, err := env.vm.state.UTXOIDs(addr.Bytes(), ids.Empty, math.MaxInt32)
	require.NoError(err)
	require.Len(utxos, 1)
	require.Equal(utxo.InputID(), utxos[0])

	env.vm.state.DeleteUTXO(utxo.InputID())
	require.NoError(env.vm.state.Commit())

	utxos, err = env.vm.state.UTXOIDs(addr.Bytes(), ids.Empty, math.MaxInt32)
	require.NoError(err)
	require.Empty(utxos)
}
