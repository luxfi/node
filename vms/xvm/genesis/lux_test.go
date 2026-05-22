// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"bytes"
	"testing"

	"github.com/luxfi/address"
	"github.com/luxfi/ids"
	"github.com/stretchr/testify/require"
)

// addr returns a valid bech32 string for the given 20-byte payload
// under the "local" HRP. Test-only helper.
func addr(t *testing.T, b [20]byte) string {
	t.Helper()
	s, err := address.FormatBech32("local", b[:])
	require.NoError(t, err)
	return s
}

// TestBuildBytes_Deterministic verifies that BuildBytes is a pure
// function of its inputs: identical inputs produce byte-identical
// output (no map iteration order leakage, no time-based fields).
// This is the property the genesis hash check depends on.
func TestBuildBytes_Deterministic(t *testing.T) {
	asset := AssetDescriptor{Name: "Lux", Symbol: "LUX", Denomination: 9}
	// Bech32 strings for HRP "local"; the addresses themselves are
	// arbitrary fixed bytes — what matters is that the codec accepts
	// them and the output is stable across calls.
	holders := []Holder{
		{Amount: 1_000_000, Address: addr(t, [20]byte{1, 2, 3})},
		{Amount: 2_000_000, Address: addr(t, [20]byte{4, 5, 6})},
	}
	memo := []byte("deadbeef")

	b1, err := BuildBytes(1, asset, holders, memo)
	require.NoError(t, err)
	require.NotEmpty(t, b1)

	b2, err := BuildBytes(1, asset, holders, memo)
	require.NoError(t, err)

	require.True(t, bytes.Equal(b1, b2), "BuildBytes must be deterministic across calls")
}

// TestBuildBytes_NetworkScoped verifies that the networkID flows
// into the genesis bytes — same asset on a different network must
// produce a different blob (this is what keeps mainnet and
// testnet/devnet genesis hashes distinct).
func TestBuildBytes_NetworkScoped(t *testing.T) {
	asset := AssetDescriptor{Name: "Lux", Symbol: "LUX", Denomination: 9}
	holders := []Holder{
		{Amount: 1_000_000, Address: addr(t, [20]byte{1, 2, 3})},
	}

	mainnet, err := BuildBytes(1, asset, holders, nil)
	require.NoError(t, err)

	testnet, err := BuildBytes(2, asset, holders, nil)
	require.NoError(t, err)

	require.False(t, bytes.Equal(mainnet, testnet), "different networkID must produce different bytes")
}

// TestBuildBytes_EmptyHolders verifies the no-holders path. An asset
// with no initial holders is valid (the genesis just has no FixedCap
// outputs); BuildBytes must not panic.
func TestBuildBytes_EmptyHolders(t *testing.T) {
	asset := AssetDescriptor{Name: "Lux", Symbol: "LUX", Denomination: 9}

	b, err := BuildBytes(1, asset, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, b)
}

// TestBuildBytes_BadAddress verifies that a malformed holder address
// surfaces an error rather than silently producing garbage bytes.
// (BuildBytes delegates to xvm.NewGenesis which calls address.ParseBech32.)
func TestBuildBytes_BadAddress(t *testing.T) {
	asset := AssetDescriptor{Name: "Lux", Symbol: "LUX", Denomination: 9}
	holders := []Holder{{Amount: 1, Address: "not-a-bech32-address"}}

	_, err := BuildBytes(1, asset, holders, nil)
	require.Error(t, err)
}

// TestAssetIDFromBytes_Stable verifies that AssetIDFromBytes returns
// the same ID for the same genesis blob and that two different
// genesis blobs (different networkID — same asset descriptor) produce
// different IDs. Both properties matter:
//
//   - same blob → same ID: every node must agree on the asset ID
//     after parsing genesis.
//   - different blob → different ID: sovereign L1s sharing a primary-
//     network ID get distinct X-Chain assets, which is the whole
//     point of deriving the ID from genesis content rather than the
//     network-id-keyed constant.
func TestAssetIDFromBytes_Stable(t *testing.T) {
	asset := AssetDescriptor{Name: "Lux", Symbol: "LUX", Denomination: 9}
	holders := []Holder{
		{Amount: 1_000_000, Address: addr(t, [20]byte{1, 2, 3})},
	}

	bytes1, err := BuildBytes(1, asset, holders, nil)
	require.NoError(t, err)

	id1, err := AssetIDFromBytes(bytes1)
	require.NoError(t, err)
	require.NotEqual(t, ids.Empty, id1)

	id1Again, err := AssetIDFromBytes(bytes1)
	require.NoError(t, err)
	require.Equal(t, id1, id1Again, "AssetIDFromBytes must be a pure function of input")

	// Different networkID → different genesis blob → different asset ID.
	bytes2, err := BuildBytes(2, asset, holders, nil)
	require.NoError(t, err)
	id2, err := AssetIDFromBytes(bytes2)
	require.NoError(t, err)
	require.NotEqual(t, id1, id2, "different networkID must produce different X-Chain asset ID")
}

// TestAssetIDFromBytes_HolderSensitivity asserts that two genesis
// blobs identical except for their initial holders also produce
// different asset IDs. The holder set is part of the CreateAssetTx
// (via InitialState), so the runtime asset ID must reflect it.
//
// This is the property that decouples sovereign-L1 asset IDs even
// when two networks share networkID + asset descriptor: their
// validator set is different, the holder set is different, so the
// genesis-derived ID is different.
func TestAssetIDFromBytes_HolderSensitivity(t *testing.T) {
	asset := AssetDescriptor{Name: "Lux", Symbol: "LUX", Denomination: 9}

	a, err := BuildBytes(1, asset, []Holder{
		{Amount: 1_000_000, Address: addr(t, [20]byte{1, 2, 3})},
	}, nil)
	require.NoError(t, err)
	idA, err := AssetIDFromBytes(a)
	require.NoError(t, err)

	b, err := BuildBytes(1, asset, []Holder{
		{Amount: 1_000_000, Address: addr(t, [20]byte{9, 9, 9})},
	}, nil)
	require.NoError(t, err)
	idB, err := AssetIDFromBytes(b)
	require.NoError(t, err)

	require.NotEqual(t, idA, idB, "different holder set must produce different X-Chain asset ID")
}

// TestAssetIDFromBytes_Malformed verifies that AssetIDFromBytes
// surfaces an error for garbage input rather than silently returning
// ids.Empty (which would then bypass the genesis-derived fix and
// reintroduce the UTXOAssetIDFor(networkID) fallback path).
func TestAssetIDFromBytes_Malformed(t *testing.T) {
	_, err := AssetIDFromBytes([]byte{0xff, 0xfe, 0xfd})
	require.Error(t, err)

	_, err = AssetIDFromBytes(nil)
	require.Error(t, err)
}
