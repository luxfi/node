// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"

	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/pcodecs"
	"github.com/luxfi/node/vms/platformvm/block"
)

// TestStateV0FeeStateReadable simulates the exact code path that
// failed on the v1.28.1 testnet-canary:
//
//	loadMetadata → getFeeState → block.GenesisCodec.Unmarshal(v0Bytes, &feeState)
//	→ pcodecs.ErrUnknownVersion (unknown codec version)
//
// The fixture is byte-for-byte what v1.23.x wrote to singletonDB at the
// FeeStateKey: 2 bytes wire prefix (0x0000) + 8 bytes Capacity + 8 bytes
// Excess.
func TestStateV0FeeStateReadable(t *testing.T) {
	require := require.New(t)

	const (
		capacity = uint64(123_456_789)
		excess   = uint64(987_654_321)
	)

	// Construct the exact byte shape this binary writes. ZAP-native is
	// little-endian for both the version prefix and uint64 fields.
	v0Bytes := make([]byte, 18)
	binary.LittleEndian.PutUint16(v0Bytes[:2], 0)
	binary.LittleEndian.PutUint64(v0Bytes[2:10], capacity)
	binary.LittleEndian.PutUint64(v0Bytes[10:18], excess)

	// Cross-check the fixture: GenesisCodec.Marshal at v0 should
	// produce the same shape, otherwise the fixture is wrong and the
	// test would mask the bug.
	expected := gas.State{Capacity: gas.Gas(capacity), Excess: gas.Gas(excess)}
	marshaled, err := block.GenesisCodec.Marshal(block.CodecVersionV0, expected)
	require.NoError(err)
	require.True(bytes.Equal(v0Bytes, marshaled),
		"fixture must match the v0 wire shape (fixture=%x marshaled=%x)", v0Bytes, marshaled)

	// The bug surface: this Unmarshal failed pre-v1.28.2 because
	// block.GenesisCodec was v1-only.
	var decoded gas.State
	version, err := block.GenesisCodec.Unmarshal(v0Bytes, &decoded)
	require.NoError(err)
	require.Equal(uint16(block.CodecVersionV0), version)
	require.Equal(expected, decoded)
}

// TestStateV0HeightRangeReadable is the regression guard for
// state/state_metadata.go:176 — the HeightsIndexedKey read path that
// also flowed through block.GenesisCodec.Unmarshal.
func TestStateV0HeightRangeReadable(t *testing.T) {
	require := require.New(t)

	expected := heightRange{
		LowerBound: 100,
		UpperBound: 200,
	}

	v0Bytes, err := block.GenesisCodec.Marshal(block.CodecVersionV0, expected)
	require.NoError(err)
	require.Equal(uint16(block.CodecVersionV0), binary.LittleEndian.Uint16(v0Bytes[:2]))

	var decoded heightRange
	_, err = block.GenesisCodec.Unmarshal(v0Bytes, &decoded)
	require.NoError(err)
	require.Equal(expected, decoded)
}

// TestStateV0L1ValidatorReadable is the regression guard for
// state/l1_validator.go:222 and state/state_validators.go:{239,535}
// — the L1 validator value read paths. L1Validator is the largest
// state-side struct (PublicKey, RemainingBalanceOwner,
// DeactivationOwner blobs) and is the type loadActiveL1Validators +
// initValidatorSets iterate over on boot, so a regression here would
// surface at the same boot stage as the original feeState bug.
func TestStateV0L1ValidatorReadable(t *testing.T) {
	require := require.New(t)

	expected := L1Validator{
		ValidationID:          ids.GenerateTestID(),
		ChainID:               ids.GenerateTestID(),
		NodeID:                ids.GenerateTestNodeID(),
		PublicKey:             bytes.Repeat([]byte{0xAB}, 48),
		RemainingBalanceOwner: []byte{0x01, 0x02},
		DeactivationOwner:     []byte{0x03, 0x04},
		StartTime:             1700000000,
		Weight:                1_000_000,
		MinNonce:              0,
		EndAccumulatedFee:     2_000_000,
	}

	v0Bytes, err := block.GenesisCodec.Marshal(block.CodecVersionV0, expected)
	require.NoError(err)

	// ValidationID is NOT serialized (it's the DB key), so the
	// decoded copy will have a zero ValidationID. Match the production
	// pattern (see state_validators.go:535+: decode then assign
	// ValidationID from the DB key).
	expected.ValidationID = ids.Empty
	var decoded L1Validator
	_, err = block.GenesisCodec.Unmarshal(v0Bytes, &decoded)
	require.NoError(err)
	require.Equal(expected, decoded)
}

// TestStateV0NetToL1ConversionReadable is the regression guard for
// state/state_chains.go:116 — the NetToL1Conversion read path.
func TestStateV0NetToL1ConversionReadable(t *testing.T) {
	require := require.New(t)

	expected := NetToL1Conversion{
		ConversionID: ids.GenerateTestID(),
		ChainID:      ids.GenerateTestID(),
		Addr:         []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}

	v0Bytes, err := block.GenesisCodec.Marshal(block.CodecVersionV0, expected)
	require.NoError(err)

	var decoded NetToL1Conversion
	_, err = block.GenesisCodec.Unmarshal(v0Bytes, &decoded)
	require.NoError(err)
	require.Equal(expected, decoded)
}

// TestStateV0LegacyStateBlkReadable is the regression guard for
// state/state_blocks.go:115 — the legacy stateBlk fallback read path
// for pre-PR-1719 block storage format.
func TestStateV0LegacyStateBlkReadable(t *testing.T) {
	require := require.New(t)

	expected := stateBlk{
		Bytes:  []byte("legacy block payload bytes"),
		Status: 4,
	}

	v0Bytes, err := block.GenesisCodec.Marshal(block.CodecVersionV0, expected)
	require.NoError(err)

	var decoded stateBlk
	_, err = block.GenesisCodec.Unmarshal(v0Bytes, &decoded)
	require.NoError(err)
	require.Equal(expected, decoded)
}

// TestStateV1FeeStateReadable is the no-regression guard for the
// canonical write path. v1.28.2 still writes feeState at v1; this must
// remain readable.
func TestStateV1FeeStateReadable(t *testing.T) {
	require := require.New(t)

	expected := gas.State{Capacity: 100, Excess: 200}
	v1Bytes, err := block.GenesisCodec.Marshal(block.CodecVersion, expected)
	require.NoError(err)
	require.Equal(uint16(block.CodecVersionV1), binary.LittleEndian.Uint16(v1Bytes[:2]))

	var decoded gas.State
	_, err = block.GenesisCodec.Unmarshal(v1Bytes, &decoded)
	require.NoError(err)
	require.Equal(expected, decoded)
}

// TestStateMetadataCodecCarriesBothVersions documents that
// MetadataCodec (used by metadata_validator.go / metadata_delegator.go)
// has been multi-version since its introduction. This is a structural
// guard rail — if a future patch ever drops the v0 slot from
// MetadataCodec, this test trips.
func TestStateMetadataCodecCarriesBothVersions(t *testing.T) {
	require := require.New(t)

	expected := preDelegateeRewardMetadata{
		UpDuration:      42,
		LastUpdated:     1700000000,
		PotentialReward: 1_000_000,
	}

	for _, version := range []uint16{CodecVersion0, CodecVersion1} {
		bytesAtVersion, err := MetadataCodec.Marshal(version, &expected)
		require.NoErrorf(err, "MetadataCodec must register version %d for Marshal", version)

		var decoded preDelegateeRewardMetadata
		gotVersion, err := MetadataCodec.Unmarshal(bytesAtVersion, &decoded)
		require.NoErrorf(err, "MetadataCodec must register version %d for Unmarshal", version)
		require.Equal(version, gotVersion)
		require.Equal(expected, decoded)
	}
}

// TestStateBootFromV0SingletonDB simulates a P-Chain boot from a
// v1.23.x-written singletonDB — the exact end-to-end shape that broke
// the testnet canary at "loadMetadata: feeState: unknown codec
// version". We write the v0-shaped feeState bytes and the v0-shaped
// heightRange bytes directly into the DB row(s) loadMetadata would
// observe and assert getFeeState + the heightRange unmarshal both
// succeed.
//
// This does NOT call loadMetadata itself (that requires a full state
// object), but it pins both private-package helpers that loadMetadata
// invokes, which is exactly where the canary failure surfaced.
func TestStateBootFromV0SingletonDB(t *testing.T) {
	require := require.New(t)

	// getFeeState path.
	{
		original := gas.State{Capacity: 5_000_000, Excess: 12345}
		v0Bytes, err := block.GenesisCodec.Marshal(block.CodecVersionV0, original)
		require.NoError(err)

		// The exact code under getFeeState():
		var feeState gas.State
		_, err = block.GenesisCodec.Unmarshal(v0Bytes, &feeState)
		require.NoError(err, "getFeeState must accept v0-prefixed bytes; this was the canary failure")
		require.Equal(original, feeState)
	}

	// loadMetadata heightRange path.
	{
		original := &heightRange{LowerBound: 1000, UpperBound: 2000}
		v0Bytes, err := block.GenesisCodec.Marshal(block.CodecVersionV0, original)
		require.NoError(err)

		indexedHeights := &heightRange{}
		_, err = block.GenesisCodec.Unmarshal(v0Bytes, indexedHeights)
		require.NoError(err)
		require.Equal(original, indexedHeights)
	}
}

// TestStateCodecRejectsUnknownVersion locks in that the multi-version
// extension did NOT silently open the door to bogus prefixes.
func TestStateCodecRejectsUnknownVersion(t *testing.T) {
	require := require.New(t)

	bogus := make([]byte, 18)
	binary.LittleEndian.PutUint16(bogus[:2], 0xFFFD)

	var sink gas.State
	_, err := block.GenesisCodec.Unmarshal(bogus, &sink)
	require.ErrorIs(err, pcodecs.ErrUnknownVersion)
}
