// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	"github.com/luxfi/node/vms/platformvm/stakeable"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

// secp256k1SigLen is the canonical secp256k1 signature length (65 bytes),
// matching secp256k1.SignatureLen without importing the CGO package.
const secp256k1SigLen = 65

// sampleBaseTx builds a BaseTx exercising every hard component path the
// single-address stub could not represent: a 2-of-3 multisig output with an
// owner locktime, a stakeable.LockOut, and a stakeable.LockIn input, plus a
// memo.
func sampleBaseTx() (*BaseTx, ids.ShortID, ids.ShortID, ids.ShortID) {
	asset := ids.GenerateTestID()
	a0, a1, a2 := ids.GenerateTestShortID(), ids.GenerateTestShortID(), ids.GenerateTestShortID()

	bt := &BaseTx{BaseTx: lux.BaseTx{
		NetworkID:    96369,
		BlockchainID: ids.GenerateTestID(),
		Outs: []*lux.TransferableOutput{
			{
				Asset: lux.Asset{ID: asset},
				Out: &secp256k1fx.TransferOutput{
					Amt: 1_000_000,
					OutputOwners: secp256k1fx.OutputOwners{
						Locktime:  1_700_000_000,
						Threshold: 2,
						Addrs:     []ids.ShortID{a0, a1, a2},
					},
				},
			},
			{
				Asset: lux.Asset{ID: asset},
				Out: &stakeable.LockOut{
					Locktime: 1_766_708_400,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt: 42,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{a0},
						},
					},
				},
			},
		},
		Ins: []*lux.TransferableInput{
			{
				UTXOID: lux.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 1},
				Asset:  lux.Asset{ID: asset},
				In: &stakeable.LockIn{
					Locktime: 1_766_708_400,
					TransferableIn: &secp256k1fx.TransferInput{
						Amt:   1_000_042,
						Input: secp256k1fx.Input{SigIndices: []uint32{0, 1}},
					},
				},
			},
		},
		Memo: []byte("native-zap"),
	}}
	return bt, a0, a1, a2
}

// TestNative_BaseTx_FullSpending_RoundTrip proves the shared spending envelope:
// multisig + stakeable outputs, a stakeable input, and a memo all survive
// Marshal -> Unmarshal with exact field equality, through the native ZAP wire
// with zero reflection.
func TestNative_BaseTx_FullSpending_RoundTrip(t *testing.T) {
	require := require.New(t)

	bt, _, _, _ := sampleBaseTx()

	// Unsigned round trip.
	b, err := nativeCodec.Marshal(CodecVersion, bt)
	require.NoError(err)

	var u UnsignedTx
	_, err = nativeCodec.Unmarshal(b, &u)
	require.NoError(err)

	got, ok := u.(*BaseTx)
	require.True(ok)
	require.Equal(bt.NetworkID, got.NetworkID)
	require.Equal(bt.BlockchainID, got.BlockchainID)
	require.Equal(bt.Outs, got.Outs, "multisig + stakeable outputs must round-trip exactly")
	require.Equal(bt.Ins, got.Ins, "stakeable input must round-trip exactly")
	require.Equal([]byte(bt.Memo), []byte(got.Memo))
}

// TestNative_SignedBaseTx_WithCreds_RoundTrip proves the signed envelope:
// unsigned ‖ creds, with real secp256k1 credential signatures round-tripping.
func TestNative_SignedBaseTx_WithCreds_RoundTrip(t *testing.T) {
	require := require.New(t)

	bt, _, _, _ := sampleBaseTx()

	var sig0, sig1 [secp256k1SigLen]byte
	for i := range sig0 {
		sig0[i] = byte(i)
		sig1[i] = byte(255 - i)
	}
	creds := []verify.Verifiable{
		&secp256k1fx.Credential{Sigs: [][secp256k1SigLen]byte{sig0, sig1}},
	}

	signed := &Tx{Unsigned: bt, Creds: creds}
	b, err := nativeCodec.Marshal(CodecVersion, signed)
	require.NoError(err)

	out := &Tx{}
	_, err = nativeCodec.Unmarshal(b, out)
	require.NoError(err)

	gotBase, ok := out.Unsigned.(*BaseTx)
	require.True(ok)
	require.Equal(bt.Outs, gotBase.Outs)
	require.Equal(bt.Ins, gotBase.Ins)
	require.Equal(creds, out.Creds, "credentials must round-trip through the signed envelope")

	// Unsigned bytes are a genuine prefix of signed bytes (tx.go invariant).
	ub, err := nativeCodec.Marshal(CodecVersion, bt)
	require.NoError(err)
	require.Equal(ub, b[:len(ub)], "unsigned must be a byte-prefix of signed")
}
