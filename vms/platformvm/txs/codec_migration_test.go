// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	hash "github.com/luxfi/crypto/hash"
	"github.com/luxfi/ids"
)

// fixtureUnsigned returns a small, deterministic UnsignedTx suitable
// for cross-version round-trip tests. AdvanceTimeTx is chosen because
// its slot ID (19) is identical under v0 and v1 — so identical input
// fields produce structurally similar bytes under both layouts. The
// only delta is the leading 2-byte version prefix.
func fixtureUnsigned() *AdvanceTimeTx {
	return &AdvanceTimeTx{Time: 1_700_000_000}
}

// fixtureSignedTx wraps fixtureUnsigned in a *Tx with an empty creds
// list. Empty creds keep the bytes minimal and let us isolate the
// codec-version prefix as the source of TxID divergence.
func fixtureSignedTx(t *testing.T) *Tx {
	t.Helper()
	return &Tx{Unsigned: fixtureUnsigned()}
}

// TestCodecVersionV0V1Coexist asserts that the v0 and v1 codec entries
// are both registered on Codec and that each can round-trip the
// AdvanceTimeTx fixture (whose slot is stable across the migration).
func TestCodecVersionV0V1Coexist(t *testing.T) {
	require := require.New(t)
	tx := fixtureSignedTx(t)

	v0Bytes, err := Codec.Marshal(CodecVersionV0, tx)
	require.NoError(err, "v0 marshal")
	v1Bytes, err := Codec.Marshal(CodecVersionV1, tx)
	require.NoError(err, "v1 marshal")

	// First 2 bytes carry the version prefix.
	require.Equal(byte(0x00), v0Bytes[0])
	require.Equal(byte(0x00), v0Bytes[1])
	require.Equal(byte(0x00), v1Bytes[0])
	require.Equal(byte(0x01), v1Bytes[1])

	// The post-prefix payload must be identical for AdvanceTimeTx —
	// slot 19 is the same in both layouts. This is the structural
	// invariant we rely on for the Apricot-tx subset of v0 reads.
	require.True(bytes.Equal(v0Bytes[2:], v1Bytes[2:]),
		"AdvanceTimeTx body differs across versions; slot 19 is supposed to be stable")
}

// TestParseDispatchesByVersion exercises the version-aware
// Parse path with a trivial AdvanceTimeTx (slot 19 stable). The
// signature-of-correctness here is that BOTH v0 bytes AND v1 bytes
// parse cleanly via txs.Parse and produce txs whose TxID is stable
// under their own version.
func TestParseDispatchesByVersion(t *testing.T) {
	require := require.New(t)
	tx := fixtureSignedTx(t)

	v0Bytes, err := Codec.Marshal(CodecVersionV0, tx)
	require.NoError(err)
	v1Bytes, err := Codec.Marshal(CodecVersionV1, tx)
	require.NoError(err)

	v0Parsed, err := Parse(Codec, v0Bytes)
	require.NoError(err, "v0 parse")
	v1Parsed, err := Parse(Codec, v1Bytes)
	require.NoError(err, "v1 parse")

	// TxID stability: each parsed tx's TxID must equal the hash of its
	// OWN signedBytes — never re-marshaled at the other version. This
	// is the byte-preserving invariant.
	require.Equal(ids.ID(hash.ComputeHash256Array(v0Bytes)), v0Parsed.TxID,
		"v0 TxID = hash(v0Bytes) — byte-preserving")
	require.Equal(ids.ID(hash.ComputeHash256Array(v1Bytes)), v1Parsed.TxID,
		"v1 TxID = hash(v1Bytes) — byte-preserving")

	// v0 and v1 TxIDs MUST differ (the bytes differ in the version
	// prefix). This is the cross-version non-collision check.
	require.NotEqual(v0Parsed.TxID, v1Parsed.TxID,
		"v0 and v1 wire-distinct txs must produce distinct TxIDs")

	// Bytes are preserved verbatim.
	require.True(bytes.Equal(v0Bytes, v0Parsed.Bytes()),
		"v0 parsed bytes are the original input")
	require.True(bytes.Equal(v1Bytes, v1Parsed.Bytes()),
		"v1 parsed bytes are the original input")
}

// TestTxIDStabilityRoundTrip asserts that the byte-preserving Parse
// path is idempotent: re-marshal of the parsed tx under its OWN
// version produces byte-equal output, so the TxID does not rotate
// across Parse -> Marshal cycles. This is what guarantees that
// re-broadcasting a tx loaded from disk keeps its TxID stable.
func TestTxIDStabilityRoundTrip(t *testing.T) {
	require := require.New(t)
	tx := fixtureSignedTx(t)

	v0Bytes, err := Codec.Marshal(CodecVersionV0, tx)
	require.NoError(err)
	v0Parsed, err := Parse(Codec, v0Bytes)
	require.NoError(err)

	// Re-marshal v0Parsed at v0 — bytes must round-trip exactly.
	rebuilt, err := Codec.Marshal(CodecVersionV0, v0Parsed)
	require.NoError(err)
	require.True(bytes.Equal(v0Bytes, rebuilt),
		"v0 round-trip: Marshal(Parse(b)) == b")
	require.Equal(v0Parsed.TxID, ids.ID(hash.ComputeHash256Array(rebuilt)),
		"v0 TxID stable across Parse -> Marshal")

	// Same for v1.
	v1Bytes, err := Codec.Marshal(CodecVersionV1, tx)
	require.NoError(err)
	v1Parsed, err := Parse(Codec, v1Bytes)
	require.NoError(err)
	rebuilt, err = Codec.Marshal(CodecVersionV1, v1Parsed)
	require.NoError(err)
	require.True(bytes.Equal(v1Bytes, rebuilt),
		"v1 round-trip: Marshal(Parse(b)) == b")
	require.Equal(v1Parsed.TxID, ids.ID(hash.ComputeHash256Array(rebuilt)),
		"v1 TxID stable across Parse -> Marshal")
}

// TestCrossVersionRefuses asserts that bytes produced at one version
// CANNOT be decoded at the other version without going through the
// dispatcher. Specifically: v0 bytes' slot map differs from v1 for
// post-Apricot types; if we manually swap the version prefix from
// 0->1 (or 1->0) without re-encoding, the slot lookup will read the
// wrong type and either fail or produce a different TxID. This is
// the property the dispatcher protects against.
//
// For AdvanceTimeTx (slot 19 stable), the post-prefix bytes ARE the
// same, so a manual prefix swap would silently succeed but produce a
// different TxID (because the prefix bytes are part of the hash). We
// assert that mismatch here.
func TestCrossVersionRefuses(t *testing.T) {
	require := require.New(t)
	tx := fixtureSignedTx(t)
	v0Bytes, err := Codec.Marshal(CodecVersionV0, tx)
	require.NoError(err)

	// Build a Frankenbytes: v1 prefix + v0 payload.
	frank := make([]byte, len(v0Bytes))
	copy(frank, v0Bytes)
	frank[0], frank[1] = 0x00, 0x01

	// Parse must NOT yield a tx whose TxID equals the v0 tx's TxID —
	// the version prefix is part of hash input. AdvanceTimeTx is one
	// of the stable-slot types so the parse itself may succeed under
	// v1 dispatcher; the test point is that the TxID differs.
	frankParsed, err := Parse(Codec, frank)
	require.NoError(err, "AdvanceTimeTx is slot-stable; v1 parse should succeed structurally")
	v0Parsed, err := Parse(Codec, v0Bytes)
	require.NoError(err)
	require.NotEqual(v0Parsed.TxID, frankParsed.TxID,
		"manually-relabeled v0 bytes must NOT produce the v0 TxID")
}
