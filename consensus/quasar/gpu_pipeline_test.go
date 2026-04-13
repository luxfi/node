// Copyright (C) 2019-2025, Lux Industries Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package quasar

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// makeRandomBytes returns n random bytes.
func makeRandomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// makeBLSWork creates BLSWork with n entries using correct BLS sizes.
func makeBLSWork(n int) *BLSWork {
	w := &BLSWork{
		Messages:   make([][]byte, n),
		Signatures: make([][]byte, n),
		PubKeys:    make([][]byte, n),
	}
	for i := 0; i < n; i++ {
		w.Messages[i] = makeRandomBytes(32)
		w.Signatures[i] = makeRandomBytes(96) // BLS G2 point
		w.PubKeys[i] = makeRandomBytes(48)    // BLS G1 point
	}
	return w
}

// makeRingtailWork creates RingtailWork with n entries.
func makeRingtailWork(n int) *RingtailWork {
	w := &RingtailWork{
		Messages:   make([][]byte, n),
		Signatures: make([][]byte, n),
		PubKeys:    make([][]byte, n),
	}
	for i := 0; i < n; i++ {
		w.Messages[i] = makeRandomBytes(48)
		w.Signatures[i] = makeRandomBytes(512)
		w.PubKeys[i] = makeRandomBytes(256)
	}
	return w
}

// makeZKWork creates ZKWork with m entries.
func makeZKWork(m int) *ZKWork {
	w := &ZKWork{
		Scalars: make([][]byte, m),
		Bases:   make([][]byte, m),
	}
	for i := 0; i < m; i++ {
		w.Scalars[i] = makeRandomBytes(32)
		w.Bases[i] = makeRandomBytes(64)
	}
	return w
}

// makeMLDSAWork creates MLDSAWork with n entries using correct Dilithium3 sizes.
func makeMLDSAWork(n int) *MLDSAWork {
	w := &MLDSAWork{
		Messages:   make([][]byte, n),
		Signatures: make([][]byte, n),
		PubKeys:    make([][]byte, n),
	}
	for i := 0; i < n; i++ {
		w.Messages[i] = makeRandomBytes(64)
		w.Signatures[i] = makeRandomBytes(3293) // Dilithium3 signature
		w.PubKeys[i] = makeRandomBytes(1952)    // Dilithium3 public key
	}
	return w
}

func TestGPUPipeline_AllFourTypes(t *testing.T) {
	pipeline := NewGPUVerifyPipeline()

	work := &BlockVerifyWork{
		BLS:      makeBLSWork(5),
		Ringtail: makeRingtailWork(3),
		ZK:       makeZKWork(2),
		MLDSA:    makeMLDSAWork(10),
	}

	result, err := pipeline.VerifyBlock(work)
	require.NoError(t, err)
	require.NotNil(t, result)

	// BLS results
	require.Len(t, result.BLSValid, 5, "should have 5 BLS results")
	for i, v := range result.BLSValid {
		require.True(t, v, "BLS[%d] should be valid", i)
	}

	// Ringtail results
	require.Len(t, result.RingtailValid, 3, "should have 3 Ringtail results")
	for i, v := range result.RingtailValid {
		require.True(t, v, "Ringtail[%d] should be valid", i)
	}

	// ZK result
	require.True(t, result.ZKValid, "ZK batch should be valid")

	// ML-DSA results
	require.Len(t, result.MLDSAValid, 10, "should have 10 ML-DSA results")
	for i, v := range result.MLDSAValid {
		require.True(t, v, "MLDSA[%d] should be valid", i)
	}

	// Timing: all durations should be non-negative
	require.GreaterOrEqual(t, result.TotalTime.Nanoseconds(), int64(0))
	require.GreaterOrEqual(t, result.BLSTime.Nanoseconds(), int64(0))
	require.GreaterOrEqual(t, result.RingtailTime.Nanoseconds(), int64(0))
	require.GreaterOrEqual(t, result.ZKTime.Nanoseconds(), int64(0))
	require.GreaterOrEqual(t, result.MLDSATime.Nanoseconds(), int64(0))

	// Stats should reflect the verification
	stats := pipeline.Stats()
	require.Equal(t, uint64(1), stats.GPUVerifies+stats.CPUVerifies,
		"exactly one verify should have been recorded")
}

func TestGPUPipeline_CPUFallback(t *testing.T) {
	// Without CGO/GPU, accel.Available() returns false.
	// Pipeline must fall back to CPU verification.
	pipeline := NewGPUVerifyPipeline()

	work := &BlockVerifyWork{
		BLS:   makeBLSWork(3),
		MLDSA: makeMLDSAWork(4),
	}

	result, err := pipeline.VerifyBlock(work)
	require.NoError(t, err)
	require.NotNil(t, result)

	// CPU fallback must produce valid results for well-formed inputs
	require.Len(t, result.BLSValid, 3)
	for i, v := range result.BLSValid {
		require.True(t, v, "CPU BLS[%d] should be valid", i)
	}

	require.Len(t, result.MLDSAValid, 4)
	for i, v := range result.MLDSAValid {
		require.True(t, v, "CPU MLDSA[%d] should be valid", i)
	}

	// GPU should not have been used (no CGO in test env)
	require.False(t, result.GPUUsed, "should use CPU fallback")

	stats := pipeline.Stats()
	require.Equal(t, uint64(1), stats.CPUVerifies)
}

func TestGPUPipeline_EmptyBatches(t *testing.T) {
	pipeline := NewGPUVerifyPipeline()

	tests := []struct {
		name string
		work *BlockVerifyWork
	}{
		{
			name: "nil work",
			work: nil,
		},
		{
			name: "all nil batches",
			work: &BlockVerifyWork{},
		},
		{
			name: "empty BLS only",
			work: &BlockVerifyWork{
				BLS: &BLSWork{},
			},
		},
		{
			name: "BLS filled, rest nil",
			work: &BlockVerifyWork{
				BLS: makeBLSWork(2),
			},
		},
		{
			name: "ZK only",
			work: &BlockVerifyWork{
				ZK: makeZKWork(1),
			},
		},
		{
			name: "MLDSA only",
			work: &BlockVerifyWork{
				MLDSA: makeMLDSAWork(1),
			},
		},
		{
			name: "Ringtail only",
			work: &BlockVerifyWork{
				Ringtail: makeRingtailWork(1),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := pipeline.VerifyBlock(tt.work)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

func TestGPUPipeline_ValidationErrors(t *testing.T) {
	pipeline := NewGPUVerifyPipeline()

	tests := []struct {
		name    string
		work    *BlockVerifyWork
		wantErr error
	}{
		{
			name: "BLS size mismatch",
			work: &BlockVerifyWork{
				BLS: &BLSWork{
					Messages:   [][]byte{{1}},
					Signatures: [][]byte{{1}, {2}}, // 2 != 1
					PubKeys:    [][]byte{{1}},
				},
			},
			wantErr: ErrBLSSizeMismatch,
		},
		{
			name: "Ringtail size mismatch",
			work: &BlockVerifyWork{
				Ringtail: &RingtailWork{
					Messages:   [][]byte{{1}, {2}},
					Signatures: [][]byte{{1}}, // 1 != 2
					PubKeys:    [][]byte{{1}, {2}},
				},
			},
			wantErr: ErrRingtailSizeMismatch,
		},
		{
			name: "ZK size mismatch",
			work: &BlockVerifyWork{
				ZK: &ZKWork{
					Scalars: [][]byte{{1}, {2}},
					Bases:   [][]byte{{1}}, // 1 != 2
				},
			},
			wantErr: ErrZKSizeMismatch,
		},
		{
			name: "MLDSA size mismatch",
			work: &BlockVerifyWork{
				MLDSA: &MLDSAWork{
					Messages:   [][]byte{{1}},
					Signatures: [][]byte{{1}},
					PubKeys:    [][]byte{{1}, {2}}, // 2 != 1
				},
			},
			wantErr: ErrMLDSASizeMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pipeline.VerifyBlock(tt.work)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func BenchmarkGPUPipeline(b *testing.B) {
	pipeline := NewGPUVerifyPipeline()

	work := &BlockVerifyWork{
		BLS:      makeBLSWork(100),
		Ringtail: makeRingtailWork(50),
		ZK:       makeZKWork(10),
		MLDSA:    makeMLDSAWork(200),
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = pipeline.VerifyBlock(work)
	}
}
