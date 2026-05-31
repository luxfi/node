// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"

	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/node/vms/platformvm/block"
)

// TestMultiVersionUnmarshalAcceptsBothVersions documents that the
// defensive helper is a transparent pass-through for the canonical
// codec — neither v0 nor v1 reads should observe any behavioral
// difference from raw codec.Manager.Unmarshal once the codec has both
// versions registered.
func TestMultiVersionUnmarshalAcceptsBothVersions(t *testing.T) {
	require := require.New(t)

	original := gas.State{Capacity: 100, Excess: 200}

	for _, version := range []uint16{block.CodecVersionV0, block.CodecVersionV1} {
		bytesAtVersion, err := block.GenesisCodec.Marshal(version, original)
		require.NoError(err)

		var decoded gas.State
		gotVersion, err := multiVersionUnmarshal(block.GenesisCodec, bytesAtVersion, &decoded)
		require.NoError(err)
		require.Equal(version, gotVersion)
		require.Equal(original, decoded)
	}
}

// TestMultiVersionUnmarshalSurfacesSingleVersionCodec is the test for
// the soft warning path. We build a single-version codec.Manager (only
// v1 registered) and call multiVersionUnmarshal on it. The Unmarshal
// itself MUST still succeed for v1 bytes (the helper is a soft guard,
// not a hard one), but the post-condition probe MUST observe the
// missing v0 slot.
//
// We can't directly intercept log.Warn (it's a global logger), so we
// instead exercise the underlying probe deterministically: marshaling
// the empty `probe` struct at v0 on a single-version codec must return
// codec.ErrUnknownVersion. This is the exact condition that triggers
// the warning emission.
func TestMultiVersionUnmarshalSurfacesSingleVersionCodec(t *testing.T) {
	require := require.New(t)

	// Build a single-version codec by hand — only v1 registered.
	singleC := linearcodec.NewDefault()
	singleVersion := codec.NewDefaultManager()
	require.NoError(singleVersion.RegisterCodec(block.CodecVersionV1, singleC))

	// Confirm v1 Unmarshal still succeeds (helper is non-blocking).
	v1Bytes, err := singleVersion.Marshal(block.CodecVersionV1, gas.State{Capacity: 1, Excess: 2})
	require.NoError(err)

	var decoded gas.State
	_, err = multiVersionUnmarshal(singleVersion, v1Bytes, &decoded)
	require.NoError(err, "multiVersionUnmarshal must not block successful v1 reads on a single-version codec")
	require.Equal(uint16(2), uint16(decoded.Excess))

	// Confirm the underlying detection mechanism would fire: v0
	// Marshal on this codec returns ErrUnknownVersion, which is what
	// the post-condition check picks up.
	_, err = singleVersion.Marshal(block.CodecVersionV0, probe{})
	require.ErrorIs(err, codec.ErrUnknownVersion,
		"single-version codec must surface ErrUnknownVersion on the v0 probe — this is the warning trigger")
}

// TestMultiVersionUnmarshalSurfacesSingleVersionCodec_Idempotent
// documents that the warning probe fires AT MOST once per codec
// pointer, so a long-running canary boot that hits dozens of state
// reads through the same single-version codec produces a single log
// line, not a flood.
func TestMultiVersionUnmarshalSurfacesSingleVersionCodec_Idempotent(t *testing.T) {
	require := require.New(t)

	singleC := linearcodec.NewDefault()
	singleVersion := codec.NewDefaultManager()
	require.NoError(singleVersion.RegisterCodec(block.CodecVersionV1, singleC))

	v1Bytes, err := singleVersion.Marshal(block.CodecVersionV1, gas.State{Capacity: 5, Excess: 6})
	require.NoError(err)

	// First call: warning probe runs (sync.Map records the codec).
	var d1 gas.State
	_, err = multiVersionUnmarshal(singleVersion, v1Bytes, &d1)
	require.NoError(err)

	// Confirm the sync.Map remembers this codec — the LoadOrStore
	// returns observed=true on subsequent calls.
	_, observedAgain := multiVersionProbeOnce.LoadOrStore(singleVersion, struct{}{})
	require.True(observedAgain, "probe must record the codec on first observation")

	// Second call: warning probe SKIPS the marshal probe entirely.
	// Unmarshal still succeeds.
	var d2 gas.State
	_, err = multiVersionUnmarshal(singleVersion, v1Bytes, &d2)
	require.NoError(err)
}

// TestMultiVersionUnmarshalRejectsUnknownVersionUpstream confirms the
// helper does NOT swallow codec.ErrUnknownVersion — if the input bytes
// carry a prefix that the codec does not have registered (e.g. a future
// codec v2 prefix on a v0+v1 codec), the original error surfaces so the
// caller can decide whether to treat it as a corruption or a soft skip.
func TestMultiVersionUnmarshalRejectsUnknownVersionUpstream(t *testing.T) {
	require := require.New(t)

	bogus := make([]byte, 18)
	binary.BigEndian.PutUint16(bogus[:2], math.MaxUint16)

	var sink gas.State
	_, err := multiVersionUnmarshal(block.GenesisCodec, bogus, &sink)
	require.ErrorIs(err, codec.ErrUnknownVersion)
}

// TestBlockGenesisCodecPassesMultiVersionProbe pins the post-v1.28.2
// invariant: the canonical state-side codec MUST register both v0 and
// v1. If a future refactor accidentally drops a version slot, this test
// trips immediately and the canary doesn't have to.
func TestBlockGenesisCodecPassesMultiVersionProbe(t *testing.T) {
	require := require.New(t)

	for _, version := range []uint16{block.CodecVersionV0, block.CodecVersionV1} {
		_, err := block.GenesisCodec.Marshal(version, probe{})
		require.NoErrorf(err, "block.GenesisCodec must register version %d", version)
	}
}
