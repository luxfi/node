// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TODO: Update these tests for the new bloom filter API
/*
func TestNewErrors(t *testing.T) {
	tests := []struct {
		numHashes  int
		numEntries int
		err        error
	}{
		{
			numHashes:  0,
			numEntries: 1,
			err:        errTooFewHashes,
		},
		{
			numHashes:  17,
			numEntries: 1,
			err:        errTooManyHashes,
		},
		{
			numHashes:  8,
			numEntries: 0,
			err:        errTooFewEntries,
		},
	}
	for _, test := range tests {
		t.Run(test.err.Error(), func(t *testing.T) {
			_, err := New(1000, 0.01, 1<<20) // maxN=1000, p=0.01, maxSize=1MB
			require.ErrorIs(t, err, test.err)
		})
	}
}
*/

// TODO: Rewrite test for new bloom filter API
func TestNormalUsage(t *testing.T) {
	require := require.New(t)

	// Basic test for new API
	filter, err := New(1000, 0.01, 1<<20) // maxN=1000, p=0.01, maxSize=1MB
	require.NoError(err)

	// Add some values
	testData := [][]byte{
		[]byte("test1"),
		[]byte("test2"),
		[]byte("test3"),
	}
	
	filter.Add(testData...)
	
	// Check they exist
	for _, data := range testData {
		require.True(filter.Check(data))
	}
	
	// Check non-existent value
	require.False(filter.Check([]byte("nonexistent")))
}

/*
func BenchmarkAdd(b *testing.B) {
	f, err := New(8, 16*units.KiB)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Add(1)
	}
}

func BenchmarkMarshal(b *testing.B) {
	f, err := New(OptimalParameters(10_000, .01))
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Marshal()
	}
}
*/
