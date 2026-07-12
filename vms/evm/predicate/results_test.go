// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"testing"

	"github.com/luxfi/geth/common"
	"github.com/stretchr/testify/require"

	"github.com/luxfi/math/set"
)

// Valid result parsing/round-tripping is tested by [TestBlockResultsBytes]
func TestParseBlockResultsInvalid(t *testing.T) {
	tests := []struct {
		name string
		b    []byte
	}{
		{
			name: "nil",
			b:    nil,
		},
		{
			name: "truncated_header",
			b:    []byte{0x00, 0x01, 0x02},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseBlockResults(test.b)
			require.Error(t, err)
		})
	}
}

func TestBlockResultsGet(t *testing.T) {
	txHash := common.Hash{1}
	address := common.Address{2}
	tests := []struct {
		name    string
		results BlockResults
		want    set.Bits
	}{
		{
			name:    "nil",
			results: nil,
			want:    set.NewBits(),
		},
		{
			name:    "empty",
			results: make(BlockResults),
			want:    set.NewBits(),
		},
		{
			name: "missing_tx",
			results: BlockResults{
				{3}: {
					address: set.NewBits(1, 2, 3),
				},
			},
			want: set.NewBits(),
		},
		{
			name: "missing_address",
			results: BlockResults{
				txHash: {
					{3}: set.NewBits(1, 2, 3),
				},
			},
			want: set.NewBits(),
		},
		{
			name: "found",
			results: BlockResults{
				txHash: {
					address: set.NewBits(1, 2, 3),
				},
			},
			want: set.NewBits(1, 2, 3),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := test.results.Get(txHash, address)
			require.Equal(t, test.want, got)
		})
	}
}

func TestBlockResultsSet(t *testing.T) {
	txHash := common.Hash{1}
	tests := []struct {
		name  string
		start BlockResults
		toSet PrecompileResults
		want  BlockResults
	}{
		{
			name:  "nil_delete",
			start: nil,
			toSet: nil,
			want:  nil,
		},
		{
			name:  "nil_allocate",
			start: nil,
			toSet: PrecompileResults{
				{3}: set.NewBits(1, 2, 3),
			},
			want: BlockResults{
				txHash: PrecompileResults{
					{3}: set.NewBits(1, 2, 3),
				},
			},
		},
		{
			name:  "empty_allocate",
			start: make(BlockResults),
			toSet: PrecompileResults{
				{3}: set.NewBits(1, 2, 3),
			},
			want: BlockResults{
				txHash: PrecompileResults{
					{3}: set.NewBits(1, 2, 3),
				},
			},
		},
		{
			name: "overwrite_tx",
			start: BlockResults{
				txHash: PrecompileResults{
					{3}: set.NewBits(1, 2, 3),
				},
			},
			toSet: PrecompileResults{
				{3}: set.NewBits(1),
			},
			want: BlockResults{
				txHash: PrecompileResults{
					{3}: set.NewBits(1),
				},
			},
		},
		{
			name: "delete_tx",
			start: BlockResults{
				txHash: PrecompileResults{
					{3}: set.NewBits(1, 2, 3),
				},
			},
			toSet: PrecompileResults{},
			want:  BlockResults{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.start.Set(txHash, test.toSet)
			require.Equal(t, test.want, test.start)
		})
	}
}

// TestBlockResultsBytes checks that the native ZAP encoding round-trips and is
// deterministic (byte-identical across marshals of the same value — required
// because the results are committed during block building/verification).
func TestBlockResultsBytes(t *testing.T) {
	tests := []struct {
		name  string
		input BlockResults
	}{
		{
			name:  "nil",
			input: nil,
		},
		{
			name:  "empty",
			input: make(BlockResults),
		},
		{
			name: "single_tx_single_result",
			input: BlockResults{
				{1}: {
					{2}: set.NewBits(1, 2, 3),
				},
			},
		},
		{
			name: "single_tx_multiple_results",
			input: BlockResults{
				{1}: {
					{2}: set.NewBits(1, 2, 3),
					{3}: set.NewBits(0, 1, 2, 3),
				},
			},
		},
		{
			name: "multiple_txs_single_result",
			input: BlockResults{
				{1}: {
					{2}: set.NewBits(1, 2, 3),
				},
				{2}: {
					{3}: set.NewBits(3, 2, 1),
				},
			},
		},
		{
			name: "multiple_txs_multiple_results",
			input: BlockResults{
				{1}: {
					{2}: set.NewBits(1, 2, 3),
					{3}: set.NewBits(3, 2, 1),
				},
				{2}: {
					{2}: set.NewBits(1, 2, 3),
					{3}: set.NewBits(3, 2, 1),
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			got, err := test.input.Bytes()
			require.NoError(err)

			// Deterministic: a second marshal of the same value is byte-identical.
			got2, err := test.input.Bytes()
			require.NoError(err)
			require.Equal(got, got2)

			input := test.input
			if input == nil {
				// nil and empty should be considered equivalent
				input = make(BlockResults)
			}

			parsed, err := ParseBlockResults(got)
			require.NoError(err)
			require.Equal(input, parsed)
		})
	}
}
