// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"bytes"
	"testing"

	"github.com/luxfi/address"
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
