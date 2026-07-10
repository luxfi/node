// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/constants"
	"github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
	"github.com/luxfi/vm/types"

	"github.com/luxfi/node/vms/pcodecs"
)

// The P-Chain runs ONE codec: ZAP-native (little-endian) at CodecVersion.
// These tests are the determinism contract that RED verifies before the
// Nova re-genesis: serialization determines tx IDs, block IDs, and state
// roots, so the encoding MUST be canonical (same Go value -> same bytes
// on every node, every run) and non-malleable (no two distinct wire
// encodings decode to the same value).

// determinismFixture is a named UnsignedTx used across the determinism
// assertions. Every fixture is SIGNATURE-FREE (no BLS) so its bytes are
// identical whether or not CGO/BLST is linked.
type determinismFixture struct {
	name string
	tx   UnsignedTx
}

func determinismFixtures() []determinismFixture {
	var (
		assetID = ids.ID{
			0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
			0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28,
			0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38,
			0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
		}
		utxoTxID = ids.ID{
			0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
			0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
			0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
			0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88,
		}
		addr = ids.ShortID{
			0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
			0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb,
			0x44, 0x55, 0x66, 0x77,
		}
		baseTx = lux.BaseTx{
			NetworkID:    constants.MainnetID,
			BlockchainID: constants.PlatformChainID,
			Ins: []*lux.TransferableInput{
				{
					UTXOID: lux.UTXOID{TxID: utxoTxID, OutputIndex: 1},
					Asset:  lux.Asset{ID: assetID},
					In: &secp256k1fx.TransferInput{
						Amt:   constants.Lux,
						Input: secp256k1fx.Input{SigIndices: []uint32{0, 2}},
					},
				},
			},
			Outs: []*lux.TransferableOutput{
				{
					Asset: lux.Asset{ID: assetID},
					Out: &secp256k1fx.TransferOutput{
						Amt: constants.MilliLux,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addr},
						},
					},
				},
			},
			Memo: types.JSONByteSlice("determinism"),
		}
	)

	return []determinismFixture{
		// Scalar-only: exercises the interface type-id prefix + a bare
		// uint64 body.
		{"AdvanceTimeTx", &AdvanceTimeTx{Time: 0x0102030405060708}},
		// Nested interfaces (TransferableInput.In / TransferableOutput.Out),
		// slices, byte-slice memo, sorted addr set — the canonical-bytes
		// machinery that state roots ride on.
		{"BaseTx", &BaseTx{BaseTx: baseTx}},
	}
}

// TestCodecVersionIsSole pins the single-codec invariant: CodecVersion is
// the one and only registered version, Marshal stamps it as a uint16
// little-endian wire prefix, and Unmarshal reports it back.
func TestCodecVersionIsSole(t *testing.T) {
	require := require.New(t)

	require.Equal(uint16(1), CodecVersion, "sole P-Chain codec version is 1 (ZAP-native)")

	var unsigned UnsignedTx = &AdvanceTimeTx{Time: 42}
	b, err := Codec.Marshal(CodecVersion, &unsigned)
	require.NoError(err)
	require.GreaterOrEqual(len(b), 2)
	require.Equal(CodecVersion, binary.LittleEndian.Uint16(b[:2]),
		"wire prefix MUST be CodecVersion, little-endian")

	var out UnsignedTx
	gotVersion, err := Codec.Unmarshal(b, &out)
	require.NoError(err)
	require.Equal(CodecVersion, gotVersion)
}

// TestGoldenAdvanceTimeTx pins the exact wire bytes AND the derived TxID
// of a fixed fixture. A change to the ZAP wire layout (byte order, type-id
// width, slot position, version prefix) breaks this test — which is the
// point: those bytes are the chain commitment.
//
// Layout, interface-marshaled at CodecVersion:
//
//	01 00                     version 1, uint16 LE
//	13 00 00 00               interface type-id 19 (AdvanceTimeTx), uint32 LE
//	08 07 06 05 04 03 02 01   Time uint64 LE (0x0102030405060708)
func TestGoldenAdvanceTimeTx(t *testing.T) {
	require := require.New(t)

	golden := []byte{
		0x01, 0x00,
		0x13, 0x00, 0x00, 0x00,
		0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01,
	}
	// TxID = SHA-256 of the signed wire bytes. Pinned so a silent
	// re-hashing or re-encoding is caught.
	goldenTxID := hash.ComputeHash256Array(golden)

	var unsigned UnsignedTx = &AdvanceTimeTx{Time: 0x0102030405060708}
	got, err := Codec.Marshal(CodecVersion, &unsigned)
	require.NoError(err)
	require.Equal(golden, got, "AdvanceTimeTx golden wire bytes drifted")
	require.Equal(goldenTxID, hash.ComputeHash256Array(got),
		"TxID = hash(wire) is not stable")
}

// TestMarshalIsDeterministic asserts Marshal is a pure function of its
// input: the same Go value marshals to byte-identical output on every
// call. A single differing byte across the fleet forks the chain.
func TestMarshalIsDeterministic(t *testing.T) {
	for _, f := range determinismFixtures() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			require := require.New(t)
			var unsigned UnsignedTx = f.tx
			first, err := Codec.Marshal(CodecVersion, &unsigned)
			require.NoError(err)
			for i := 0; i < 256; i++ {
				again, err := Codec.Marshal(CodecVersion, &unsigned)
				require.NoError(err)
				require.Equal(first, again, "Marshal is not deterministic across calls")
			}
		})
	}
}

// TestRoundTripByteStability is the canonical-form invariant: for every
// fixture, Marshal -> Unmarshal -> re-Marshal reproduces byte-identical
// wire. This is what guarantees a decoded-then-re-encoded tx (the block
// re-marshal path in initialize) keeps its TxID, and that state records
// re-serialize to the same bytes -> same state root.
func TestRoundTripByteStability(t *testing.T) {
	for _, f := range determinismFixtures() {
		f := f
		t.Run(f.name, func(t *testing.T) {
			require := require.New(t)

			var unsigned UnsignedTx = f.tx
			wire, err := Codec.Marshal(CodecVersion, &unsigned)
			require.NoError(err)

			var decoded UnsignedTx
			version, err := Codec.Unmarshal(wire, &decoded)
			require.NoError(err)
			require.Equal(CodecVersion, version)

			reWire, err := Codec.Marshal(CodecVersion, &decoded)
			require.NoError(err)
			require.Equal(wire, reWire,
				"decode->re-encode is not byte-stable; the encoding is non-canonical")
		})
	}
}

// TestTrailingBytesRejected is a malleability guard: appending any bytes
// to a valid wire MUST be rejected, not silently ignored. Otherwise two
// distinct byte strings would decode to the same value with different
// hashes.
func TestTrailingBytesRejected(t *testing.T) {
	require := require.New(t)

	var unsigned UnsignedTx = &AdvanceTimeTx{Time: 7}
	wire, err := Codec.Marshal(CodecVersion, &unsigned)
	require.NoError(err)

	malleable := make([]byte, len(wire)+1)
	copy(malleable, wire)
	malleable[len(wire)] = 0x00 // even a zero byte must be rejected

	var out UnsignedTx
	_, err = Codec.Unmarshal(malleable, &out)
	require.ErrorIs(err, pcodecs.ErrExtraSpace,
		"trailing bytes MUST be rejected (canonical-form / non-malleability)")
}

// TestTruncatedRejected asserts a wire missing its final byte is rejected
// rather than decoding a truncated value.
func TestTruncatedRejected(t *testing.T) {
	require := require.New(t)

	var unsigned UnsignedTx = &AdvanceTimeTx{Time: 7}
	wire, err := Codec.Marshal(CodecVersion, &unsigned)
	require.NoError(err)

	var out UnsignedTx
	_, err = Codec.Unmarshal(wire[:len(wire)-1], &out)
	require.Error(err, "a truncated wire MUST NOT decode")
}

// TestUnknownVersionRejected asserts that a wire whose 2-byte prefix is
// not CodecVersion is rejected. There is exactly one registered version;
// anything else is ErrUnknownVersion (no legacy fallback).
func TestUnknownVersionRejected(t *testing.T) {
	require := require.New(t)

	var unsigned UnsignedTx = &AdvanceTimeTx{Time: 7}
	wire, err := Codec.Marshal(CodecVersion, &unsigned)
	require.NoError(err)

	// Rewrite the prefix to a version that is not registered.
	forged := make([]byte, len(wire))
	copy(forged, wire)
	binary.LittleEndian.PutUint16(forged[:2], CodecVersion+1)

	var out UnsignedTx
	_, err = Codec.Unmarshal(forged, &out)
	require.ErrorIs(err, pcodecs.ErrUnknownVersion,
		"a prefix other than CodecVersion MUST be rejected")
}
