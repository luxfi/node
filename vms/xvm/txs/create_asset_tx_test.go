// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// TestCreateAssetTxRoundTrip round-trips a create-asset tx carrying both a
// TransferOutput and a MintOutput in its initial state — proving the packed
// InitialState list and the fx-state envelope dispatch reconstruct byte-for-byte.
func TestCreateAssetTxRoundTrip(t *testing.T) {
	require := require.New(t)

	utx := &CreateAssetTx{
		BaseTx: *sampleBaseTx(),
		Name:   "Volatility Index",
		Symbol: "VIX",
		Denomination: 2,
		States: []*InitialState{
			{
				FxIndex: 0,
				Outs: []verify.State{
					&secp256k1fx.TransferOutput{
						Amt: 12345,
						OutputOwners: secp256k1fx.OutputOwners{
							Locktime:  54321,
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
					},
					&secp256k1fx.MintOutput{
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[1].PublicKey().Address()},
						},
					},
				},
			},
		},
	}

	tx := &Tx{Unsigned: utx}
	require.NoError(tx.Initialize())

	parsed, err := Parse(tx.Bytes())
	require.NoError(err)
	require.Equal(tx.Bytes(), parsed.Bytes())
	require.Equal(tx.ID(), parsed.ID())

	got := parsed.Unsigned.(*CreateAssetTx)
	require.Equal("Volatility Index", got.Name)
	require.Equal("VIX", got.Symbol)
	require.Equal(byte(2), got.Denomination)
	require.Equal(constants.UnitTestID, got.NetworkID)
	require.Len(got.States, 1)
	require.EqualValues(0, got.States[0].FxIndex)
	require.Len(got.States[0].Outs, 2)
	require.Equal(fxBytes(t, utx.States[0].Outs[0]), fxBytes(t, got.States[0].Outs[0]))
	require.Equal(fxBytes(t, utx.States[0].Outs[1]), fxBytes(t, got.States[0].Outs[1]))
}

// TestCreateAssetTxNoStatesRoundTrip covers the degenerate shape (no base
// outputs, memo only, single mint state) — a common wallet-built create tx.
func TestCreateAssetTxNoStatesRoundTrip(t *testing.T) {
	require := require.New(t)

	utx := &CreateAssetTx{
		BaseTx: BaseTx{BaseTx: lux.BaseTx{
			NetworkID:    constants.UnitTestID,
			BlockchainID: chainID,
			Memo:         []byte{0x00, 0x01, 0x02, 0x03},
		}},
		Name:         "name",
		Symbol:       "symb",
		Denomination: 0,
		States: []*InitialState{
			{
				FxIndex: 0,
				Outs: []verify.State{
					&secp256k1fx.MintOutput{
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
					},
				},
			},
		},
	}

	tx := &Tx{Unsigned: utx}
	require.NoError(tx.Initialize())

	parsed, err := Parse(tx.Bytes())
	require.NoError(err)
	require.Equal(tx.Bytes(), parsed.Bytes())

	got := parsed.Unsigned.(*CreateAssetTx)
	require.Equal("name", got.Name)
	require.Equal("symb", got.Symbol)
	require.Empty(got.Outs)
	require.Len(got.States, 1)
	require.Equal(fxBytes(t, utx.States[0].Outs[0]), fxBytes(t, got.States[0].Outs[0]))
}

func TestCreateAssetTxNotState(t *testing.T) {
	require := require.New(t)

	intf := interface{}(&CreateAssetTx{})
	_, ok := intf.(verify.State)
	require.False(ok, "should not be marked as state")
}
