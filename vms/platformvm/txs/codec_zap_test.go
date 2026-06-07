// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCodecVersionForTimestamp_StrictBoundary asserts the timestamp
// selector pins V2 (ZAP-native) from genesis per LP-023.
// ZAPCodecActivationTimestamp == 0, so every ts >= 0 selects V2.
func TestCodecVersionForTimestamp_StrictBoundary(t *testing.T) {
	require := require.New(t)

	// EXACTLY at activation (ts=0): V2.
	require.Equal(CodecVersionV2, CodecVersionForTimestamp(ZAPCodecActivationTimestamp),
		"ts == activation must select zapcodec (V2)")

	// Far after activation: still V2.
	require.Equal(CodecVersionV2, CodecVersionForTimestamp(ZAPCodecActivationTimestamp+1_000_000),
		"ts > activation must select zapcodec (V2)")

	// Genesis (ts=0): V2 — LP-023 makes ZAP-native mandatory from genesis.
	require.Equal(CodecVersionV2, CodecVersionForTimestamp(0),
		"ts == 0 must select zapcodec (V2) — LP-023 hard cut")
}

// TestCodecForTimestamp_ManagerIsStable asserts the manager handle is
// the same across timestamps — only the version returned by
// CodecVersionForTimestamp varies. Callers MUST pass that version into
// Codec.Marshal / Codec.Unmarshal.
func TestCodecForTimestamp_ManagerIsStable(t *testing.T) {
	require := require.New(t)
	pre := CodecForTimestamp(0)
	post := CodecForTimestamp(ZAPCodecActivationTimestamp + 1)
	// Two interface values from the same package-level variable; must
	// be identical.
	require.Equal(Codec, pre)
	require.Equal(Codec, post)
	require.Equal(pre, post)
}

// TestPreActivationRoundTripV1 round-trips a tx through V1 (kept as a
// registered codec slot even though it is no longer the write version
// per LP-023 — activation is at genesis). Both V1 and V2 use the same
// ZAP-native LE wire encoding in this binary; the version byte is the
// only on-wire distinguisher.
func TestPreActivationRoundTripV1(t *testing.T) {
	require := require.New(t)

	tx := &AdvanceTimeTx{Time: 1700000000}

	b, err := Codec.Marshal(CodecVersionV1, tx)
	require.NoError(err)

	// Wire prefix is the codec version (LE per LP-023).
	require.Equal(uint16(CodecVersionV1), binary.LittleEndian.Uint16(b[:2]),
		"V1 wire prefix MUST be LE-encoded ZAP-native (LP-023)")

	out := &AdvanceTimeTx{}
	gotVersion, err := Codec.Unmarshal(b, out)
	require.NoError(err)
	require.Equal(CodecVersionV1, gotVersion)
	require.Equal(tx.Time, out.Time)
}

// TestPostActivationRoundTripV2 round-trips a tx through V2 — the
// canonical post-LP-023 write version. Wire prefix is LE per ZAP.
func TestPostActivationRoundTripV2(t *testing.T) {
	require := require.New(t)

	tx := &AdvanceTimeTx{Time: ZAPCodecActivationTimestamp}
	v := CodecVersionForTimestamp(tx.Time)
	require.Equal(CodecVersionV2, v)

	b, err := Codec.Marshal(v, tx)
	require.NoError(err)
	require.Equal(uint16(CodecVersionV2), binary.LittleEndian.Uint16(b[:2]),
		"post-activation wire MUST be V2-prefixed (LE per LP-023)")

	out := &AdvanceTimeTx{}
	gotVersion, err := Codec.Unmarshal(b, out)
	require.NoError(err)
	require.Equal(CodecVersionV2, gotVersion)
	require.Equal(tx.Time, out.Time)
}

// TestCrossVersionWireIsDistinct asserts that V1 and V2 bytes for the
// same logical tx differ in the prefix byte only — both encode the
// payload via ZAP-native LE with the same slot map per LP-023.
func TestCrossVersionWireIsDistinct(t *testing.T) {
	require := require.New(t)

	tx := &AdvanceTimeTx{Time: ZAPCodecActivationTimestamp}

	v1Bytes, err := Codec.Marshal(CodecVersionV1, tx)
	require.NoError(err)
	v2Bytes, err := Codec.Marshal(CodecVersionV2, tx)
	require.NoError(err)

	// Same length — same slot map, same wire encoding.
	require.Equal(len(v1Bytes), len(v2Bytes),
		"V1 and V2 wire MUST have identical length for the same logical tx")

	// Prefix differs (V1 vs V2 codec version).
	require.NotEqual(v1Bytes[:2], v2Bytes[:2],
		"V1 and V2 wire prefix MUST differ")

	// Payload (post-prefix) is identical — both are ZAP-native LE on the
	// same slot map. LP-023 made V1 and V2 wire-format-equivalent.
	require.Equal(v1Bytes[2:], v2Bytes[2:],
		"V1 and V2 wire payload MUST be byte-identical (both ZAP-native LE)")
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
// wire has the codec version followed by the zapcodec little-endian
// payload. Both prefix and payload are LE per ZAP — LP-023 makes ZAP
// mandatory from genesis.
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

	// Wire layout for AdvanceTimeTx under V2:
	//   bytes 0-1: 0x0002 (LE, codec version)
	//   bytes 2-9: Time uint64 in LE — LSB first
	require.Equal(byte(CodecVersionV2), b[0], "V2 prefix LE: byte 0 is LSB of version")
	require.Equal(byte(0x00), b[1])
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

// TestV1WireIsZapNative is the mirror assertion for V1: LP-023 makes
// ZAP mandatory from genesis, so V1 and V2 share the LE wire format.
// The version byte at offset 0 is the only on-wire distinguisher.
func TestV1WireIsZapNative(t *testing.T) {
	require := require.New(t)

	tx := &AdvanceTimeTx{Time: 0x0102030405060708}

	b, err := Codec.Marshal(CodecVersionV1, tx)
	require.NoError(err)
	require.Equal(byte(CodecVersionV1), b[0], "V1 prefix LE: byte 0 is LSB of version")
	require.Equal(byte(0x00), b[1])
	require.Equal(byte(0x08), b[2], "V1 LSB-first: byte 2 must be LSB of Time (ZAP-native LE)")
	require.Equal(byte(0x01), b[9], "V1 LSB-first: byte 9 must be MSB of Time (ZAP-native LE)")
}

// TestActivationConstantUnchanged is the watchdog test. The constant
// MUST be 0 (LP-023: ZAP-native mandatory from genesis).
func TestActivationConstantUnchanged(t *testing.T) {
	require.Equal(t, uint64(0), ZAPCodecActivationTimestamp,
		"activation constant changed without consensus coordination — "+
			"see comments on ZAPCodecActivationTimestamp for the rationale (LP-023: genesis)")
}
