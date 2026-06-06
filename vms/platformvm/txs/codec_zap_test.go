// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCodecVersionForTimestamp_StrictBoundary asserts the timestamp
// selector is bit-exact at the activation boundary. ts ==
// ZAPCodecActivationTimestamp - 1 is V1; ts ==
// ZAPCodecActivationTimestamp is V2. No fuzz, no fallback.
//
// Note (LP-023 phase-7): both V1 and V2 are now backed by zapcodec
// (LE on the wire); the version-selector still distinguishes them so
// downstream tooling that pins on the prefix uint16 keeps working.
func TestCodecVersionForTimestamp_StrictBoundary(t *testing.T) {
	require := require.New(t)

	// One second BEFORE activation: must be V1.
	require.Equal(CodecVersionV1, CodecVersionForTimestamp(ZAPCodecActivationTimestamp-1),
		"ts < activation must select V1")

	// EXACTLY at activation: must be V2.
	require.Equal(CodecVersionV2, CodecVersionForTimestamp(ZAPCodecActivationTimestamp),
		"ts == activation must select V2")

	// Far after activation: still V2.
	require.Equal(CodecVersionV2, CodecVersionForTimestamp(ZAPCodecActivationTimestamp+1_000_000),
		"ts > activation must select V2")

	// Genesis (ts=0): V1, the historical write path.
	require.Equal(CodecVersionV1, CodecVersionForTimestamp(0),
		"ts == 0 must select V1")
}

// TestCodecForTimestamp_ManagerIsStable asserts the manager handle is
// the same across timestamps — only the version returned by
// CodecVersionForTimestamp varies. Callers MUST pass that version into
// Codec.Marshal / Codec.Unmarshal.
func TestCodecForTimestamp_ManagerIsStable(t *testing.T) {
	require := require.New(t)
	pre := CodecForTimestamp(ZAPCodecActivationTimestamp - 1)
	post := CodecForTimestamp(ZAPCodecActivationTimestamp + 1)
	// Two interface values from the same package-level variable; must
	// be identical.
	require.Equal(Codec, pre)
	require.Equal(Codec, post)
	require.Equal(pre, post)
}

// TestPreActivationRoundTripV1 asserts a tx with ts < activation
// round-trips through V1: marshal at V1 → unmarshal as V1 → identical
// Go value. The wire prefix MUST be V1.
//
// The version prefix is uint16 LE per the multi-manager wire layout
// (proto/zap_codec/multi.go): [uint16 LE codec version][inner body].
func TestPreActivationRoundTripV1(t *testing.T) {
	require := require.New(t)

	tx := &AdvanceTimeTx{Time: ZAPCodecActivationTimestamp - 1}
	v := CodecVersionForTimestamp(tx.Time)
	require.Equal(CodecVersionV1, v)

	b, err := Codec.Marshal(v, tx)
	require.NoError(err)

	// Wire prefix is the codec version (2 bytes LE per multi-manager).
	require.Equal(uint16(CodecVersionV1), binary.LittleEndian.Uint16(b[:2]),
		"pre-activation wire MUST be V1-prefixed")

	out := &AdvanceTimeTx{}
	gotVersion, err := Codec.Unmarshal(b, out)
	require.NoError(err)
	require.Equal(CodecVersionV1, gotVersion)
	require.Equal(tx.Time, out.Time)
}

// TestPostActivationRoundTripV2 asserts a tx with ts >= activation
// round-trips through V2: marshal at V2 → unmarshal as V2 → identical
// Go value. Wire prefix MUST be V2.
func TestPostActivationRoundTripV2(t *testing.T) {
	require := require.New(t)

	tx := &AdvanceTimeTx{Time: ZAPCodecActivationTimestamp}
	v := CodecVersionForTimestamp(tx.Time)
	require.Equal(CodecVersionV2, v)

	b, err := Codec.Marshal(v, tx)
	require.NoError(err)
	require.Equal(uint16(CodecVersionV2), binary.LittleEndian.Uint16(b[:2]),
		"post-activation wire MUST be V2-prefixed")

	out := &AdvanceTimeTx{}
	gotVersion, err := Codec.Unmarshal(b, out)
	require.NoError(err)
	require.Equal(CodecVersionV2, gotVersion)
	require.Equal(tx.Time, out.Time)
}

// TestCrossVersionWirePrefixDistinct asserts that V1 and V2 bytes for
// the same logical tx differ in their version prefix.
//
// Per LP-023 phase-7, V1 and V2 use the same zapcodec wire encoding
// (both LE) — they only differ in the 2-byte version prefix. The
// payload bytes are byte-identical for the same logical tx; only the
// prefix selects which slot map the decoder will use. This is the
// inverse of the pre-rip world where V1 was BE and V2 was LE.
func TestCrossVersionWirePrefixDistinct(t *testing.T) {
	require := require.New(t)

	tx := &AdvanceTimeTx{Time: ZAPCodecActivationTimestamp}

	v1Bytes, err := Codec.Marshal(CodecVersionV1, tx)
	require.NoError(err)
	v2Bytes, err := Codec.Marshal(CodecVersionV2, tx)
	require.NoError(err)

	// Same length — V1 and V2 share the zapcodec wire encoding.
	require.Equal(len(v1Bytes), len(v2Bytes),
		"V1 and V2 wire MUST have identical length for the same logical tx")

	// Prefix differs (V1 vs V2 codec version).
	require.NotEqual(v1Bytes[:2], v2Bytes[:2],
		"V1 and V2 wire prefix MUST differ")

	// Payload (post-prefix) is identical: both V1 and V2 are backed
	// by zapcodec (LE) with the same slot map. The only on-wire
	// difference is the 2-byte version prefix.
	require.Equal(v1Bytes[2:], v2Bytes[2:],
		"V1 and V2 wire payload MUST be byte-equal — both are zapcodec-backed (LE)")
}

// TestCodecAllowsRead asserts the read-acceptance gate. V0, V1, V2 are
// recognised; anything else is not.
func TestCodecAllowsRead(t *testing.T) {
	require := require.New(t)
	require.True(CodecAllowsRead(CodecVersionV0))
	require.True(CodecAllowsRead(CodecVersionV1))
	require.True(CodecAllowsRead(CodecVersionV2))
	require.False(CodecAllowsRead(3))
	require.False(CodecAllowsRead(0xFFFF))
}

// TestCodecRequiresLegacy asserts the legacy-version classifier.
func TestCodecRequiresLegacy(t *testing.T) {
	require := require.New(t)
	require.True(CodecRequiresLegacy(CodecVersionV0))
	require.True(CodecRequiresLegacy(CodecVersionV1))
	require.False(CodecRequiresLegacy(CodecVersionV2))
}

// TestV2WireIsZapNative asserts the canonical detection: a V2-prefixed
// wire has the codec version 0x0002 (LE per multi-manager) followed by
// the zapcodec little-endian payload. The activation contract is that
// V2 == zapcodec; if anyone changes the underlying impl, this test
// surfaces the regression.
//
// We assert it structurally by checking that the marshaled byte at
// offset 2 (first payload byte after the version prefix) is the LSB
// of the Time field, not the MSB. AdvanceTimeTx packs Time as a
// uint64 directly; LE means LSB-first.
func TestV2WireIsZapNative(t *testing.T) {
	require := require.New(t)

	// Pick a Time with distinct LSB/MSB.
	tx := &AdvanceTimeTx{Time: 0x0102030405060708}

	b, err := Codec.Marshal(CodecVersionV2, tx)
	require.NoError(err)

	// Wire layout for AdvanceTimeTx under V2 (LP-023 ZAP-native):
	//   bytes 0-1: 0x0002 (LE, codec version) -> 0x02 0x00
	//   bytes 2-9: Time uint64 in LE — LSB first
	require.Equal(byte(CodecVersionV2), b[0], "V2 version prefix is LE: byte 0 = LSB of version (0x02)")
	require.Equal(byte(0x00), b[1], "V2 version prefix is LE: byte 1 = MSB of version (0x00)")
	require.Equal(byte(0x08), b[2], "V2 LSB-first: byte 2 must be LSB of Time")
	require.Equal(byte(0x07), b[3])
	require.Equal(byte(0x06), b[4])
	require.Equal(byte(0x05), b[5])
	require.Equal(byte(0x04), b[6])
	require.Equal(byte(0x03), b[7])
	require.Equal(byte(0x02), b[8])
	require.Equal(byte(0x01), b[9], "V2 LSB-first: byte 9 must be MSB of Time")

	// Cross-check via LE uint64.
	require.Equal(uint64(0x0102030405060708), binary.LittleEndian.Uint64(b[2:]),
		"V2 payload MUST be LE-decodable as the original Time value")
}

// TestV1WireIsLittleEndian asserts V1 is on the zapcodec (LE) wire too.
//
// Pre-LP-023, V1 was linearcodec (BE). Phase-7 of the codec rip moved
// the V1 backend to zapcodec, so V1 wire bytes are now LE. The version
// prefix is still uint16 LE (matches V0/V2). The slot map is the only
// thing that distinguishes V1 from V2 — the wire encoding is identical
// for any payload that doesn't cross a slot-dispatched type.
func TestV1WireIsLittleEndian(t *testing.T) {
	require := require.New(t)

	tx := &AdvanceTimeTx{Time: 0x0102030405060708}

	b, err := Codec.Marshal(CodecVersionV1, tx)
	require.NoError(err)
	// V1 version prefix is also LE.
	require.Equal(byte(CodecVersionV1), b[0], "V1 version prefix is LE: byte 0 = LSB of version (0x01)")
	require.Equal(byte(0x00), b[1], "V1 version prefix is LE: byte 1 = MSB of version (0x00)")
	require.Equal(byte(0x08), b[2], "V1 LSB-first: byte 2 must be LSB of Time")
	require.Equal(byte(0x01), b[9], "V1 LSB-first: byte 9 must be MSB of Time")
}

// TestActivationConstantUnchanged is the watchdog test. Anyone who
// modifies the activation timestamp out of a consensus-coordinated
// PR will trip this gate. The constant MUST be 1782864000 (2026-07-01
// 00:00:00 UTC) — see comments on ZAPCodecActivationTimestamp.
func TestActivationConstantUnchanged(t *testing.T) {
	require.Equal(t, uint64(1782864000), ZAPCodecActivationTimestamp,
		"activation constant changed without consensus coordination — "+
			"see comments on ZAPCodecActivationTimestamp for the rationale")
}
