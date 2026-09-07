// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/secp256k1"
	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

var (
	chainID = ids.ID{5, 4, 3, 2, 1}
	assetID = ids.ID{1, 2, 3}
	keys    = secp256k1.TestKeys()
)

// fxBytes re-encodes a decoded fx primitive to its wire envelope. Comparing
// fxBytes(original) to fxBytes(decoded) is a real decode-fidelity check: it
// exercises the reconstructed struct fields, not the cached input bytes.
func fxBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := childBytes(v)
	require.NoError(t, err)
	return b
}

func sampleBaseTx() *BaseTx {
	return &BaseTx{BaseTx: lux.BaseTx{
		NetworkID:    constants.UnitTestID,
		BlockchainID: chainID,
		Outs: []*lux.TransferableOutput{{
			Asset: lux.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		}},
		Ins: []*lux.TransferableInput{{
			UTXOID: lux.UTXOID{
				TxID: ids.ID{
					0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8,
					0xf7, 0xf6, 0xf5, 0xf4, 0xf3, 0xf2, 0xf1, 0xf0,
					0xef, 0xee, 0xed, 0xec, 0xeb, 0xea, 0xe9, 0xe8,
					0xe7, 0xe6, 0xe5, 0xe4, 0xe3, 0xe2, 0xe1, 0xe0,
				},
				OutputIndex: 1,
			},
			Asset: lux.Asset{ID: assetID},
			In: &secp256k1fx.TransferInput{
				Amt:   54321,
				Input: secp256k1fx.Input{SigIndices: []uint32{2}},
			},
		}},
		Memo: []byte{0x00, 0x01, 0x02, 0x03},
	}}
}

// TestBaseTxRoundTrip proves the native-ZAP struct-is-wire path: build → Bytes
// → Parse reconstructs the same tx (byte-identical, same ID, same fields), and
// signing appends a real fx credential envelope that round-trips.
func TestBaseTxRoundTrip(t *testing.T) {
	require := require.New(t)

	utx := sampleBaseTx()
	tx := &Tx{Unsigned: utx}
	require.NoError(tx.Initialize())
	require.NotEqual(ids.Empty, tx.ID())

	parsed, err := Parse(tx.Bytes())
	require.NoError(err)
	require.Equal(tx.Bytes(), parsed.Bytes())
	require.Equal(tx.ID(), parsed.ID())

	got := parsed.Unsigned.(*BaseTx)
	require.Equal(utx.NetworkID, got.NetworkID)
	require.Equal(utx.BlockchainID, got.BlockchainID)
	require.Equal([]byte(utx.Memo), []byte(got.Memo))
	require.Len(got.Outs, 1)
	require.Len(got.Ins, 1)
	require.Equal(utx.Outs[0].AssetID(), got.Outs[0].AssetID())
	require.Equal(fxBytes(t, utx.Outs[0].Out), fxBytes(t, got.Outs[0].Out))
	require.Equal(fxBytes(t, utx.Ins[0].In), fxBytes(t, got.Ins[0].In))
	require.Equal(utx.Ins[0].UTXOID, got.Ins[0].UTXOID)

	require.NoError(tx.SignSECP256K1Fx([][]*secp256k1.PrivateKey{
		{keys[0], keys[0]},
	}))
	require.Len(tx.Creds, 1)

	signed, err := Parse(tx.Bytes())
	require.NoError(err)
	require.Equal(tx.ID(), signed.ID())
	require.Len(signed.Creds, 1)
	cred := signed.Creds[0].Credential.(*secp256k1fx.Credential)
	require.Len(cred.Sigs, 2)
}

func TestBaseTxNotState(t *testing.T) {
	require := require.New(t)

	intf := interface{}(&BaseTx{})
	_, ok := intf.(verify.State)
	require.False(ok, "should not be marked as state")
}
