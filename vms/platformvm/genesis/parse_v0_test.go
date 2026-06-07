// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/node/vms/platformvm/txs"
)

// TestParseAcceptsV0CachedGenesis is the regression guard for the
// v1.28.0 testnet-canary failure:
//
//	unable to load genesis file: resolve X-Chain asset ID from cached
//	genesis: parse platform genesis: unknown codec version
//
// Pre-fix, genesis.Codec aliased block.GenesisCodec which registers
// ONLY the v1 tx slot map. A v0-prefixed cached-genesis blob (carried
// over from v1.23.x bootstraps) errored at the very first byte with
// codec.ErrUnknownVersion.
//
// Post-fix, genesis.Codec aliases txs.GenesisCodec which registers
// BOTH v0 and v1 tx slot maps. The 2-byte wire prefix selects the slot
// map; the outer Genesis struct decodes inline regardless.
func TestParseAcceptsV0CachedGenesis(t *testing.T) {
	require := require.New(t)

	gen := createTestGenesis(t)

	// Build a v0-prefixed blob by Marshaling the same Genesis struct
	// at CodecVersionV0. The outer struct has no slot ID; embedded
	// *txs.Tx fields serialize their v0 slot IDs because the Marshal
	// path resolves slot IDs against the v0 slot map (registered on
	// txs.GenesisCodec under CodecVersionV0).
	v0Bytes, err := Codec.Marshal(txs.CodecVersionV0, gen)
	require.NoError(err, "v0 marshal must succeed — proves the v0 slot map is registered")
	require.GreaterOrEqual(len(v0Bytes), 2)

	prefix := binary.BigEndian.Uint16(v0Bytes[:2])
	require.Equal(txs.CodecVersionV0, prefix, "marshaled bytes must carry the v0 wire prefix")

	// The bug: Parse used to fail here with codec.ErrUnknownVersion.
	parsed, err := Parse(v0Bytes)
	require.NoError(err, "v0-prefixed genesis must round-trip through the multi-version dispatcher")
	require.NotNil(parsed)

	require.Equal(gen.Timestamp, parsed.Timestamp)
	require.Equal(gen.InitialSupply, parsed.InitialSupply)
	require.Equal(gen.Message, parsed.Message)
	require.Len(parsed.UTXOs, len(gen.UTXOs))
	require.Len(parsed.Validators, len(gen.Validators))
	require.Len(parsed.Chains, len(gen.Chains))

	// Embedded validator txs must have been re-initialized at v0 so
	// their (re-marshaled) signed bytes — and therefore their TxIDs —
	// match what a v1.23.x producer would have committed at genesis.
	require.NotEmpty(parsed.Validators[0].Bytes())
	require.NotEqual([32]byte{}, parsed.Validators[0].TxID)
}

// TestParseAcceptsV1Genesis is the no-regression guard for the
// canonical write path: every blob produced by Genesis.Bytes() in this
// binary is v1-prefixed and must still parse byte-for-byte.
func TestParseAcceptsV1Genesis(t *testing.T) {
	require := require.New(t)

	gen := createTestGenesis(t)
	v1Bytes, err := gen.Bytes()
	require.NoError(err)
	require.GreaterOrEqual(len(v1Bytes), 2)

	prefix := binary.LittleEndian.Uint16(v1Bytes[:2])
	require.Equal(txs.CodecVersionV1, prefix, "Genesis.Bytes must emit the v1 wire prefix (LE per LP-023)")

	parsed, err := Parse(v1Bytes)
	require.NoError(err)
	require.Equal(gen.Timestamp, parsed.Timestamp)
	require.Equal(gen.InitialSupply, parsed.InitialSupply)
	require.Equal(gen.Message, parsed.Message)
}

// TestParseV0RoundtripIsBytePreserving documents the byte-preserving
// guarantee in genesis.go's Parse doc: v0 -> Parse -> re-marshal-at-v0
// must produce the exact original bytes. This is what keeps the
// originally-committed genesis hash stable across the v0->v1 codec
// migration — the BlockID/genesis-hash a downstream block header
// chains back to is hash(v0 genesis bytes), and that hash MUST NOT
// rotate when this node reads v0 bytes off disk.
func TestParseV0RoundtripIsBytePreserving(t *testing.T) {
	require := require.New(t)

	gen := createTestGenesis(t)
	v0Bytes, err := Codec.Marshal(txs.CodecVersionV0, gen)
	require.NoError(err)

	parsed, err := Parse(v0Bytes)
	require.NoError(err)

	// Re-marshal at the SAME version. Linearcodec is deterministic,
	// so the output must be byte-equal to the input.
	roundtripBytes, err := Codec.Marshal(txs.CodecVersionV0, parsed)
	require.NoError(err)
	require.True(bytes.Equal(v0Bytes, roundtripBytes),
		"v0 -> Parse -> re-marshal must be byte-preserving (len in=%d, out=%d)",
		len(v0Bytes), len(roundtripBytes))
}

// TestParseRejectsUnknownVersion locks in that we did not accidentally
// open the door to silent acceptance of bogus prefixes. Only v0 and v1
// are registered on Codec; anything else must surface as an error.
func TestParseRejectsUnknownVersion(t *testing.T) {
	require := require.New(t)

	// Build a syntactically-valid v1 blob, then mutate the prefix.
	gen := createTestGenesis(t)
	v1Bytes, err := gen.Bytes()
	require.NoError(err)
	require.GreaterOrEqual(len(v1Bytes), 2)

	bogus := make([]byte, len(v1Bytes))
	copy(bogus, v1Bytes)
	binary.BigEndian.PutUint16(bogus[:2], 0xFFFE)

	_, err = Parse(bogus)
	require.Error(err, "Parse must reject codec versions outside {v0, v1}")
}
