// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/gas"
	"github.com/luxfi/utxo/secp256k1fx"
)

func TestStatewire_OutputOwnersRoundTrip(t *testing.T) {
	cases := []*secp256k1fx.OutputOwners{
		{Threshold: 0, Locktime: 0, Addrs: nil},
		{Threshold: 1, Locktime: 42, Addrs: []ids.ShortID{{0x01}}},
		{
			Threshold: 2,
			Locktime:  0xDEADBEEF,
			Addrs: []ids.ShortID{
				{0x01, 0x02, 0x03},
				{0xFF, 0xEE, 0xDD, 0xCC},
				ids.GenerateTestShortID(),
			},
		},
	}
	for _, want := range cases {
		b, err := marshalOwner(want)
		require.NoError(t, err)

		got, err := parseOwner(b)
		require.NoError(t, err)

		oo, ok := got.(*secp256k1fx.OutputOwners)
		require.True(t, ok)
		require.Equal(t, want.Threshold, oo.Threshold)
		require.Equal(t, want.Locktime, oo.Locktime)
		require.Equal(t, want.Addrs, oo.Addrs)
	}
}

func TestStatewire_L1ValidatorRoundTrip(t *testing.T) {
	full := L1Validator{
		// ValidationID is the DB key — NOT serialized; parse leaves it zero.
		ValidationID:          ids.GenerateTestID(),
		ChainID:               ids.GenerateTestID(),
		NodeID:                ids.GenerateTestNodeID(),
		PublicKey:             []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE},
		RemainingBalanceOwner: []byte{0x11, 0x22, 0x33},
		DeactivationOwner:     []byte{0x44},
		StartTime:             1_700_000_000,
		Weight:                9_999,
		MinNonce:              7,
		EndAccumulatedFee:     123_456_789,
	}
	// A validator with empty opaque blobs (inactive/degenerate shapes).
	empty := L1Validator{
		ChainID:   ids.GenerateTestID(),
		NodeID:    ids.GenerateTestNodeID(),
		StartTime: 1,
		Weight:    0,
		MinNonce:  0,
	}

	for _, want := range []L1Validator{full, empty} {
		b, err := marshalL1Validator(want)
		require.NoError(t, err)

		var got L1Validator
		require.NoError(t, parseL1Validator(b, &got))

		// ValidationID is not on the wire; compare the serialized fields only.
		want.ValidationID = ids.Empty
		require.Equal(t, want, got)
	}
}

func TestStatewire_FeeStateRoundTrip(t *testing.T) {
	for _, want := range []gas.State{
		{},
		{Capacity: 1, Excess: 2},
		{Capacity: 0xFFFFFFFFFFFFFFFF, Excess: 0x0123456789ABCDEF},
	} {
		b, err := marshalFeeState(want)
		require.NoError(t, err)

		got, err := parseFeeState(b)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}

func TestStatewire_HeightRangeRoundTrip(t *testing.T) {
	want := &heightRange{LowerBound: 10, UpperBound: 987_654_321}
	b, err := marshalHeightRange(want)
	require.NoError(t, err)

	var got heightRange
	require.NoError(t, parseHeightRange(b, &got))
	require.Equal(t, *want, got)
}

func TestStatewire_ConversionRoundTrip(t *testing.T) {
	cases := []NetToL1Conversion{
		{ConversionID: ids.GenerateTestID(), ChainID: ids.GenerateTestID(), Addr: nil},
		{ConversionID: ids.GenerateTestID(), ChainID: ids.GenerateTestID(), Addr: []byte{0x01, 0x02, 0x03, 0x04}},
	}
	for _, want := range cases {
		b, err := marshalConversion(want)
		require.NoError(t, err)

		got, err := parseConversion(b)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}
