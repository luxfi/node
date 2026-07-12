// Copyright (C) 2019-2026, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/luxfi/ids"
	"github.com/luxfi/node/vms/components/verify"
	lux "github.com/luxfi/utxo"
	"github.com/luxfi/utxo/secp256k1fx"
)

var errTest = errors.New("non-nil error")

func TestInitialStateRoundTrip(t *testing.T) {
	require := require.New(t)

	is := &InitialState{
		FxIndex: 0,
		Outs: []verify.State{
			&secp256k1fx.TransferOutput{
				Amt: 12345,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  54321,
					Threshold: 1,
					Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
				},
			},
		},
	}

	b, err := is.Bytes()
	require.NoError(err)

	got, err := parseInitialState(b)
	require.NoError(err)
	require.EqualValues(0, got.FxIndex)
	require.Len(got.Outs, 1)
	require.Equal(fxBytes(t, is.Outs[0]), fxBytes(t, got.Outs[0]))
}

func TestInitialStateVerifyNil(t *testing.T) {
	require := require.New(t)
	numFxs := 1

	is := (*InitialState)(nil)
	err := is.Verify(numFxs)
	require.ErrorIs(err, ErrNilInitialState)
}

func TestInitialStateVerifyUnknownFxID(t *testing.T) {
	require := require.New(t)
	numFxs := 1

	is := InitialState{
		FxIndex: 1,
	}
	err := is.Verify(numFxs)
	require.ErrorIs(err, ErrUnknownFx)
}

func TestInitialStateVerifyNilOutput(t *testing.T) {
	require := require.New(t)
	numFxs := 1

	is := InitialState{
		FxIndex: 0,
		Outs:    []verify.State{nil},
	}
	err := is.Verify(numFxs)
	require.ErrorIs(err, ErrNilFxOutput)
}

func TestInitialStateVerifyInvalidOutput(t *testing.T) {
	require := require.New(t)
	numFxs := 1

	is := InitialState{
		FxIndex: 0,
		Outs:    []verify.State{&lux.TestState{Err: errTest}},
	}
	err := is.Verify(numFxs)
	require.ErrorIs(err, errTest)
}

func TestInitialStateVerifyUnsortedOutputs(t *testing.T) {
	require := require.New(t)
	numFxs := 1

	outA := &secp256k1fx.TransferOutput{
		Amt:          1,
		OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{keys[0].PublicKey().Address()}},
	}
	outB := &secp256k1fx.TransferOutput{
		Amt:          2,
		OutputOwners: secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{keys[0].PublicKey().Address()}},
	}
	is := InitialState{FxIndex: 0, Outs: []verify.State{outA, outB}}
	// Force an unsorted order regardless of the canonical wire byte ordering.
	if isSortedState(is.Outs) {
		is.Outs[0], is.Outs[1] = is.Outs[1], is.Outs[0]
	}
	require.ErrorIs(is.Verify(numFxs), ErrOutputsNotSorted)
	is.Sort()
	require.NoError(is.Verify(numFxs))
}

func TestInitialStateCompare(t *testing.T) {
	tests := []struct {
		a        *InitialState
		b        *InitialState
		expected int
	}{
		{
			a:        &InitialState{},
			b:        &InitialState{},
			expected: 0,
		},
		{
			a: &InitialState{
				FxIndex: 1,
			},
			b:        &InitialState{},
			expected: 1,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%d_%d_%d", test.a.FxIndex, test.b.FxIndex, test.expected), func(t *testing.T) {
			require := require.New(t)

			require.Equal(test.expected, test.a.Compare(test.b))
			require.Equal(-test.expected, test.b.Compare(test.a))
		})
	}
}
