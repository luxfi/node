// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/codec"

	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/txs"
)

// TestGenesisCodecAcceptsV0FeeState is the regression guard for the
// v1.28.1 testnet-canary failure:
//
//	P-Chain state corrupt after init — database must be wiped
//	error="loadMetadata: feeState: unknown codec version"
//
// Pre-fix, block.GenesisCodec registered ONLY the v1 slot map. A
// testnet bootstrapped at v1.23.x had written feeState bytes with the
// v0 prefix (0x0000); v1.28.0+ called block.GenesisCodec.Unmarshal on
// those bytes, codec.Manager looked up version 0 in its map, missed,
// and returned codec.ErrUnknownVersion — surfacing as the
// "unknown codec version" error at loadMetadata: feeState.
//
// Post-fix, block.GenesisCodec is multi-version (v0 read-only + v1
// read+write). The 2-byte wire prefix selects the slot map; the
// gas.State struct is byte-equal across versions (no slot dispatch
// inside it), so v0 bytes decode into the same Go value.
func TestGenesisCodecAcceptsV0FeeState(t *testing.T) {
	require := require.New(t)

	original := gas.State{
		Capacity: gas.Gas(1_000_000),
		Excess:   gas.Gas(424242),
	}

	v0Bytes, err := GenesisCodec.Marshal(CodecVersionV0, original)
	require.NoError(err, "v0 marshal must succeed — proves the v0 slot map is registered on block.GenesisCodec")
	require.GreaterOrEqual(len(v0Bytes), 2)
	require.Equal(uint16(CodecVersionV0), binary.BigEndian.Uint16(v0Bytes[:2]),
		"marshaled bytes must carry the v0 wire prefix")

	// The bug: this Unmarshal used to fail with codec.ErrUnknownVersion.
	var decoded gas.State
	version, err := GenesisCodec.Unmarshal(v0Bytes, &decoded)
	require.NoError(err, "v0-prefixed feeState must decode via the multi-version GenesisCodec")
	require.Equal(uint16(CodecVersionV0), version)
	require.Equal(original, decoded)
}

// TestGenesisCodecAcceptsV1FeeState is the no-regression guard for the
// canonical write path: every feeState written by this binary carries
// the v1 wire prefix and must continue to round-trip.
func TestGenesisCodecAcceptsV1FeeState(t *testing.T) {
	require := require.New(t)

	original := gas.State{
		Capacity: gas.Gas(2_500_000),
		Excess:   gas.Gas(7777),
	}

	v1Bytes, err := GenesisCodec.Marshal(CodecVersion, original)
	require.NoError(err)
	require.Equal(uint16(CodecVersionV1), binary.BigEndian.Uint16(v1Bytes[:2]),
		"canonical write path must emit v1 prefix")

	var decoded gas.State
	version, err := GenesisCodec.Unmarshal(v1Bytes, &decoded)
	require.NoError(err)
	require.Equal(uint16(CodecVersionV1), version)
	require.Equal(original, decoded)
}

// TestGenesisCodecRejectsUnknownVersion locks in that the multi-version
// extension did NOT silently open the door to bogus prefixes. Only v0
// and v1 are registered on GenesisCodec; anything else must surface as
// codec.ErrUnknownVersion.
func TestGenesisCodecRejectsUnknownVersion(t *testing.T) {
	require := require.New(t)

	bogus := make([]byte, 16)
	binary.BigEndian.PutUint16(bogus[:2], 0xFFFE)

	var sink gas.State
	_, err := GenesisCodec.Unmarshal(bogus, &sink)
	require.ErrorIs(err, codec.ErrUnknownVersion)
}

// TestGenesisCodecV0RoundtripIsBytePreserving documents the byte-
// preserving guarantee for state values read at v0 and re-marshaled at
// v0 (e.g. for a migration/extraction tool). Linearcodec is
// deterministic — output must be byte-equal to input.
func TestGenesisCodecV0RoundtripIsBytePreserving(t *testing.T) {
	require := require.New(t)

	original := gas.State{
		Capacity: gas.Gas(123),
		Excess:   gas.Gas(456),
	}

	v0Bytes, err := GenesisCodec.Marshal(CodecVersionV0, original)
	require.NoError(err)

	var decoded gas.State
	_, err = GenesisCodec.Unmarshal(v0Bytes, &decoded)
	require.NoError(err)

	roundtripBytes, err := GenesisCodec.Marshal(CodecVersionV0, decoded)
	require.NoError(err)
	require.True(bytes.Equal(v0Bytes, roundtripBytes),
		"v0 marshal -> unmarshal -> re-marshal must be byte-preserving (len in=%d, out=%d)",
		len(v0Bytes), len(roundtripBytes))
}

// TestGenesisCodecCarriesBothVersions is a structural invariant test
// that prevents accidental regressions of the multi-version property.
// Future patches that add a new state-side type or refactor the codec
// init order will trip this if they drop either version slot.
func TestGenesisCodecCarriesBothVersions(t *testing.T) {
	require := require.New(t)

	// Marshal a value at each version. If either codec is missing,
	// codec.Manager.Marshal returns ErrUnknownVersion.
	for _, version := range []uint16{CodecVersionV0, CodecVersionV1} {
		_, err := GenesisCodec.Marshal(version, gas.State{Capacity: 1, Excess: 2})
		require.NoErrorf(err, "GenesisCodec must register version %d", version)
	}
}

// TestGenesisCodecV0RegisteredTypesMatchTxsV0 asserts the v0 type slot
// map registered on block.GenesisCodec is the same shape as the one on
// txs.GenesisCodec. They share the registerV0TxTypes layout so that
// state values containing fx.Owner / signer.* / secp256k1fx.* types
// decode by the same slot IDs whether they were written via
// txs.GenesisCodec.Marshal or block.GenesisCodec.Marshal at v0.
//
// We cannot directly compare slot-ID maps (linearcodec doesn't expose
// them), so we cross-check by marshaling an interface-carrying value
// at v0 through both codecs and asserting byte-equality.
func TestGenesisCodecV0RegisteredTypesMatchTxsV0(t *testing.T) {
	require := require.New(t)

	// Use a simple wrapper that exercises slot dispatch on a known v0
	// type. AdvanceTimeTx is registered at the same slot (20 in v1, 20
	// in v0) on both codecs.
	tx := &txs.AdvanceTimeTx{Time: 1234567890}

	blockBytes, err := GenesisCodec.Marshal(CodecVersionV0, &tx)
	require.NoError(err)

	txsBytes, err := txs.GenesisCodec.Marshal(CodecVersionV0, &tx)
	require.NoError(err)

	require.True(bytes.Equal(blockBytes, txsBytes),
		"block.GenesisCodec and txs.GenesisCodec must produce byte-equal v0 encodings (block=%d txs=%d)",
		len(blockBytes), len(txsBytes))
}
