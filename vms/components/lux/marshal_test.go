// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package lux

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/utils"
	"github.com/luxfi/utxo/secp256k1fx"
)

func testTransferOutput() *secp256k1fx.TransferOutput {
	// OutputOwners.Verify() requires sorted+unique addresses — the wire
	// encoding preserves order faithfully, so seed canonical (sorted) data.
	addrs := []ids.ShortID{ids.GenerateTestShortID(), ids.GenerateTestShortID()}
	utils.Sort(addrs)
	return &secp256k1fx.TransferOutput{
		Amt: 12345,
		OutputOwners: secp256k1fx.OutputOwners{
			Locktime:  67,
			Threshold: 1,
			Addrs:     addrs,
		},
	}
}

func testTransferInput() *secp256k1fx.TransferInput {
	return &secp256k1fx.TransferInput{
		Amt:   99,
		Input: secp256k1fx.Input{SigIndices: []uint32{0, 2, 5}},
	}
}

func TestAssetRoundTrip(t *testing.T) {
	require := require.New(t)
	a := &Asset{ID: ids.GenerateTestID()}

	b, err := a.Marshal()
	require.NoError(err)

	got := &Asset{}
	require.NoError(got.Unmarshal(b))
	require.Equal(a.ID, got.ID)
}

func TestUTXOIDRoundTrip(t *testing.T) {
	require := require.New(t)
	u := &UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 7}

	b, err := u.Marshal()
	require.NoError(err)

	got := &UTXOID{}
	require.NoError(got.Unmarshal(b))
	require.Equal(u.TxID, got.TxID)
	require.Equal(u.OutputIndex, got.OutputIndex)
}

func TestTransferableOutputRoundTrip(t *testing.T) {
	require := require.New(t)
	out := &TransferableOutput{
		Asset: Asset{ID: ids.GenerateTestID()},
		Out:   testTransferOutput(),
	}

	b, err := out.Marshal()
	require.NoError(err)

	got := &TransferableOutput{}
	require.NoError(got.Unmarshal(b))
	require.Equal(out.Asset.ID, got.Asset.ID)
	require.Equal(out.Out, got.Out)
	require.NoError(got.Verify())

	// Wire bytes are stable across a re-marshal of the decoded value.
	b2, err := got.Marshal()
	require.NoError(err)
	require.Equal(b, b2)
}

func TestTransferableInputRoundTrip(t *testing.T) {
	require := require.New(t)
	in := &TransferableInput{
		UTXOID: UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 3},
		Asset:  Asset{ID: ids.GenerateTestID()},
		In:     testTransferInput(),
	}

	b, err := in.Marshal()
	require.NoError(err)

	got := &TransferableInput{}
	require.NoError(got.Unmarshal(b))
	require.Equal(in.UTXOID.TxID, got.UTXOID.TxID)
	require.Equal(in.UTXOID.OutputIndex, got.UTXOID.OutputIndex)
	require.Equal(in.Asset.ID, got.Asset.ID)
	require.Equal(in.In, got.In)
	require.NoError(got.Verify())
}

func TestUTXORoundTrip(t *testing.T) {
	require := require.New(t)
	utxo := &UTXO{
		UTXOID: UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 4},
		Asset:  Asset{ID: ids.GenerateTestID()},
		Out:    testTransferOutput(),
	}

	b, err := utxo.Marshal()
	require.NoError(err)

	got := &UTXO{}
	require.NoError(got.Unmarshal(b))
	require.Equal(utxo.UTXOID.TxID, got.UTXOID.TxID)
	require.Equal(utxo.UTXOID.OutputIndex, got.UTXOID.OutputIndex)
	require.Equal(utxo.Asset.ID, got.Asset.ID)
	require.Equal(utxo.Out, got.Out)
	require.NoError(got.Verify())

	// InputID (TxID.Prefix(index)) is preserved end-to-end.
	require.Equal(utxo.InputID(), got.InputID())

	b2, err := got.Marshal()
	require.NoError(err)
	require.Equal(b, b2)
}

func TestBaseTxRoundTrip(t *testing.T) {
	require := require.New(t)
	tx := &BaseTx{
		NetworkID:    96369,
		BlockchainID: ids.GenerateTestID(),
		Outs: []*TransferableOutput{
			{Asset: Asset{ID: ids.GenerateTestID()}, Out: testTransferOutput()},
		},
		Ins: []*TransferableInput{
			{
				UTXOID: UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 1},
				Asset:  Asset{ID: ids.GenerateTestID()},
				In:     testTransferInput(),
			},
		},
		Memo: []byte("round-trip"),
	}

	b, err := tx.Marshal()
	require.NoError(err)

	got := &BaseTx{}
	require.NoError(got.Unmarshal(b))
	require.Equal(tx.NetworkID, got.NetworkID)
	require.Equal(tx.BlockchainID, got.BlockchainID)
	require.Len(got.Outs, 1)
	require.Len(got.Ins, 1)
	require.Equal(tx.Outs[0].Asset.ID, got.Outs[0].Asset.ID)
	require.Equal(tx.Outs[0].Out, got.Outs[0].Out)
	require.Equal(tx.Ins[0].In, got.Ins[0].In)
	require.Equal(tx.Memo, got.Memo)
}

func TestMetadataRoundTrip(t *testing.T) {
	require := require.New(t)
	md := &Metadata{}
	md.Initialize([]byte("unsigned-bytes"), []byte("signed-bytes"))

	b, err := md.Marshal()
	require.NoError(err)

	got := &Metadata{}
	require.NoError(got.Unmarshal(b))
	require.Equal(md.Bytes(), got.Bytes())
	require.Equal(md.SignedBytes(), got.SignedBytes())
	require.Equal(md.ID(), got.ID())
	require.NoError(got.Verify())
}

// TestOutputOwnersRoundTrip exercises the standalone OutputOwners path
// (MarshalOwner / UnmarshalOwner) shared with off-tx owner identity keys.
func TestOutputOwnersRoundTrip(t *testing.T) {
	require := require.New(t)
	owners := &secp256k1fx.OutputOwners{
		Locktime:  5,
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	b, err := MarshalOwner(owners)
	require.NoError(err)

	got, err := UnmarshalOwner(b)
	require.NoError(err)
	require.Equal(owners.Locktime, got.Locktime)
	require.Equal(owners.Threshold, got.Threshold)
	require.Equal(owners.Addrs, got.Addrs)
}
